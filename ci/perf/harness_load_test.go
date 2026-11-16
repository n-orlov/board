//go:build perf

package perf

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// api talks to the server as the harness actor with the minted key.
type api struct {
	base, key string
	client    *http.Client
	nonce     string
	seq       atomic.Int64
}

func newAPI(base, key string) *api {
	tr := &http.Transport{MaxIdleConnsPerHost: 256}
	return &api{base: base, key: key, client: &http.Client{Transport: tr, Timeout: 10 * time.Second},
		nonce: fmt.Sprintf("%d", time.Now().UnixNano())}
}

func (a *api) request(ctx context.Context, method, path, body string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.key)
	req.Header.Set("Board-Actor", ActorHandle)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", fmt.Sprintf("perf-%s-%d", a.nonce, a.seq.Add(1)))
	}
	return req, nil
}

// do sends one request, reads the whole answer and returns how long that took.
// Any status outside 2xx is an error.
func (a *api) do(ctx context.Context, method, path, body string) (time.Duration, error) {
	req, err := a.request(ctx, method, path, body)
	if err != nil {
		return 0, err
	}
	begin := time.Now()
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return 0, err
	}
	took := time.Since(begin)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return took, fmt.Errorf("%s %s answered %d", method, path, resp.StatusCode)
	}
	return took, nil
}

// latencies sends warm requests that are not counted, then n counted ones one
// after the other, and returns each one's time. The first failure ends it.
func latencies(n int, send func(i int) (time.Duration, error)) ([]time.Duration, error) {
	const warm = 50
	out := make([]time.Duration, 0, n)
	for i := -warm; i < n; i++ {
		d, err := send(max(i, 0))
		if err != nil {
			return nil, err
		}
		if i >= 0 {
			out = append(out, d)
		}
	}
	return out, nil
}

// loadResult is what the sustained load did.
type loadResult struct {
	Sent, Errors int
	Latencies    []time.Duration
	Elapsed      time.Duration
}

// loadRequest is the i-th request of the mix: every fifth is a write (a note), the
// rest read /now, /attention, the notes or the events in turn.
func loadRequest(i int) (method, path, body string) {
	if i%5 == 4 {
		return http.MethodPost, "/api/v1/notes", fmt.Sprintf(`{"summary":"load note %d"}`, i)
	}
	reads := []string{"/api/v1/now", "/api/v1/attention", "/api/v1/notes?limit=20", "/api/v1/events?limit=20"}
	return http.MethodGet, reads[i%5%len(reads)], ""
}

// runLoad sends rate*seconds requests, the i-th at i/rate seconds from the start,
// each in its own goroutine (at most 256 at once; a request that finds them all
// busy is dropped and counts as an error). It returns when every request ended.
func runLoad(ctx context.Context, a *api, rate, seconds int) loadResult {
	total := rate * seconds
	interval := time.Second / time.Duration(rate)
	var mu sync.Mutex
	res := loadResult{Sent: total}
	slots := make(chan struct{}, 256)
	var wg sync.WaitGroup
	begin := time.Now()
	for i := 0; i < total; i++ {
		time.Sleep(time.Until(begin.Add(time.Duration(i) * interval)))
		select {
		case slots <- struct{}{}:
		default:
			mu.Lock()
			res.Errors++
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-slots }()
			method, path, body := loadRequest(i)
			d, err := a.do(ctx, method, path, body)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				res.Errors++
				return
			}
			res.Latencies = append(res.Latencies, d)
		}(i)
	}
	wg.Wait()
	res.Elapsed = time.Since(begin)
	return res
}

// sseResult is how the SSE clients fared.
type sseResult struct {
	Failed int
	Max    time.Duration
}

// sseWait is how long a client has to receive the published event (R14).
const sseWait = 5 * time.Second

// sseOutcome is the end of one client: when it read the published event, or that
// it never did (it could not connect, or its stream ended first).
type sseOutcome struct {
	at time.Time
	ok bool
}

// runSSE opens clients streams, publishes one note once they have connected and
// returns how many did not see it within sseWait and the slowest that did. A
// client that cannot connect counts as one that did not see it.
func runSSE(ctx context.Context, a *api, clients int) (sseResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	marker := fmt.Sprintf("sse-probe-%s", a.nonce)
	connected := make(chan struct{}, clients)
	got := make(chan sseOutcome, clients)
	var wg sync.WaitGroup
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got <- sseClient(ctx, a, marker, connected)
		}()
	}
	defer wg.Wait()
	defer cancel()
	for i := 0; i < clients; i++ {
		<-connected
	}
	time.Sleep(500 * time.Millisecond)
	published := time.Now()
	if _, err := a.do(ctx, http.MethodPost, "/api/v1/notes", fmt.Sprintf(`{"summary":%q}`, marker)); err != nil {
		return sseResult{}, err
	}
	return collectSSE(got, clients, published), nil
}

// collectSSE reads each client's outcome until all have reported or sseWait plus a
// second of grace is over, then judges each against sseWait.
func collectSSE(got <-chan sseOutcome, clients int, published time.Time) sseResult {
	res := sseResult{}
	timeout := time.After(sseWait + time.Second)
	for seen := 0; seen < clients; seen++ {
		select {
		case o := <-got:
			res.judge(o, published)
		case <-timeout:
			res.Failed += clients - seen
			res.Max = max(res.Max, sseWait+time.Second)
			return res
		}
	}
	return res
}

func (r *sseResult) judge(o sseOutcome, published time.Time) {
	d := o.at.Sub(published)
	switch {
	case !o.ok || d > sseWait:
		r.Failed++
	default:
		r.Max = max(r.Max, d)
	}
}

// sseClient opens the stream and says on connected when it is open or has failed
// to open (exactly once), then returns the time it read a data line holding
// marker, or that it did not.
func sseClient(ctx context.Context, a *api, marker string, connected chan<- struct{}) sseOutcome {
	// A stream has no timeout of its own: the context ends it.
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	req, err := a.request(ctx, http.MethodGet, "/api/v1/stream", "")
	if err != nil {
		connected <- struct{}{}
		return sseOutcome{}
	}
	resp, err := client.Do(req)
	connected <- struct{}{}
	if err != nil {
		return sseOutcome{}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return sseOutcome{}
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "data:") && strings.Contains(sc.Text(), marker) {
			return sseOutcome{at: time.Now(), ok: true}
		}
	}
	return sseOutcome{}
}

//go:build perf

package perf

// The measuring half of the harness (SPEC 14, R14): it builds the real vh binary,
// seeds a SQLite store, starts `vh serve` on it and measures the eight gates of
// gates.go, then writes perf.json and junit-perf.xml. It is behind the perf build
// tag because it takes minutes:
//
//	ci/perf.sh [dir]
//
// VH_PERF_EVENTS and the other VH_PERF_ settings of Config size a smoke run on a
// reduced seed, VH_PERF_OUT moves the results (default ci-results at the
// repository root) and VH_PERF_VH names a prebuilt binary. A gate that is breached
// or was not measured fails the test.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/store"
)

// run is one harness run: where it works, what it has measured so far.
type run struct {
	t       *testing.T
	cfg     Config
	bin     string
	work    string
	metrics map[string]float64
}

// record stores a measurement in milliseconds under name.
func (r *run) record(name string, d time.Duration) { r.metrics[name] = Millis(d) }

// percentile records the p-th percentile of samples under name.
func (r *run) percentile(name string, samples []time.Duration, p float64) {
	d, err := Percentile(samples, p)
	if err != nil {
		r.t.Errorf("%s: %v", name, err)
		return
	}
	r.record(name, d)
}

func TestPerf(t *testing.T) {
	cfg, err := ConfigFromEnv(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	r := &run{t: t, cfg: cfg, work: t.TempDir(), metrics: map[string]float64{}}
	started := time.Now()
	defer func() { r.finish(started) }()
	if r.bin, err = buildVH(r.work); err != nil {
		t.Fatal(err)
	}
	t.Logf("config %+v (reduced: %v)", cfg, cfg.Reduced())
	r.coldStartEmpty()
	db, key, ok := r.seed()
	if !ok {
		return
	}
	srv, ok := r.coldStartSeeded(db)
	if !ok {
		return
	}
	defer srv.stop()
	r.idle(srv)
	a := newAPI(srv.base, key)
	r.reads(a)
	r.renew(a)
	r.load(a)
	r.sse(a)
	r.cli(srv, key)
}

// finish writes perf.json and the JUnit file and fails the test on any gate that
// did not pass.
func (r *run) finish(started time.Time) {
	t := r.t
	report := NewReport(r.cfg, r.metrics, started, time.Now())
	out := os.Getenv("VH_PERF_OUT")
	if out == "" {
		out = filepath.Join(repoRoot, "ci-results")
	}
	if err := report.WriteJSON(filepath.Join(out, "perf.json")); err != nil {
		t.Errorf("perf.json: %v", err)
	}
	if err := report.WriteJUnit(filepath.Join(out, "junit-perf.xml")); err != nil {
		t.Errorf("junit-perf.xml: %v", err)
	}
	for _, c := range report.Checks {
		t.Logf("%-16s %-4v %s: %s", c.Target, c.Pass, c.Name, c.Detail)
	}
	if !report.Passed {
		t.Errorf("a perf gate was breached or not measured (reduced run: %v)", report.Reduced)
	}
}

// coldStartEmpty times vh serve on a database file that does not exist yet: it
// migrates it, then answers /healthz.
func (r *run) coldStartEmpty() {
	srv, took, err := startServer(r.bin, filepath.Join(r.work, "empty", "board.db"), r.work)
	if err != nil {
		r.t.Errorf("cold start on an empty store: %v", err)
		return
	}
	srv.stop()
	r.record(MetricColdEmptyMillis, took)
}

// seed makes the seeded store, migrated, and mints a key in it.
func (r *run) seed() (db, key string, ok bool) {
	ctx := context.Background()
	db = filepath.Join(r.work, "seeded", "board.db")
	st, err := store.Open("sqlite://" + db)
	if err == nil {
		err = st.Migrate(ctx)
	}
	if err != nil {
		r.t.Errorf("preparing the seeded store: %v", err)
		return "", "", false
	}
	begin := time.Now()
	plan := SeedPlan{Events: r.cfg.Events, Notes: r.cfg.Notes, Leases: r.cfg.Leases, Now: time.Now()}
	if err := Seed(ctx, st.Writer(), plan); err != nil {
		_ = st.Close()
		r.t.Errorf("seeding: %v", err)
		return "", "", false
	}
	if err := st.Close(); err != nil {
		r.t.Errorf("closing the seeded store: %v", err)
		return "", "", false
	}
	r.t.Logf("seeded %d events, %d notes, %d leases in %v", plan.Events, plan.Notes, plan.Leases, time.Since(begin).Round(time.Second))
	if key, err = mintKey(ctx, r.bin, db, r.work); err != nil {
		r.t.Errorf("%v", err)
		return "", "", false
	}
	return db, key, true
}

func (r *run) coldStartSeeded(db string) (*server, bool) {
	srv, took, err := startServer(r.bin, db, r.work)
	if err != nil {
		r.t.Errorf("cold start on the seeded store: %v", err)
		return nil, false
	}
	r.record(MetricColdSeededMillis, took)
	return srv, true
}

// idle leaves the freshly started server alone for the idle wait and reads its RSS.
func (r *run) idle(srv *server) {
	time.Sleep(time.Duration(r.cfg.IdleSeconds) * time.Second)
	rss, err := srv.rssBytes()
	if err != nil {
		r.t.Errorf("idle RSS: %v", err)
		return
	}
	r.metrics[MetricIdleRSSBytes] = rss
}

// get returns a latency sender for GET path.
func get(a *api, path string) func(int) (time.Duration, error) {
	return func(int) (time.Duration, error) { return a.do(context.Background(), "GET", path, "") }
}

// reads measures GET /api/v1/now and /api/v1/attention.
func (r *run) reads(a *api) {
	for name, path := range map[string]string{MetricNowP99Millis: "/api/v1/now", MetricAttentionP99: "/api/v1/attention"} {
		samples, err := latencies(r.cfg.Requests, get(a, path))
		if err != nil {
			r.t.Errorf("%s: %v", path, err)
			continue
		}
		r.percentile(name, samples, 99)
	}
}

// renew measures POST /api/v1/leases/L<n>/renew, over the seeded leases in turn.
func (r *run) renew(a *api) {
	send := func(i int) (time.Duration, error) {
		path := fmt.Sprintf("/api/v1/leases/L%d/renew", i%r.cfg.Leases+1)
		return a.do(context.Background(), "POST", path, `{}`)
	}
	samples, err := latencies(r.cfg.Requests, send)
	if err != nil {
		r.t.Errorf("lease renew: %v", err)
		return
	}
	r.percentile(MetricRenewP99Millis, samples, 99)
}

// load runs the sustained mixed load and records its errors.
func (r *run) load(a *api) {
	res := runLoad(context.Background(), a, r.cfg.LoadRate, r.cfg.LoadSeconds)
	r.metrics[MetricLoadErrors] = float64(res.Errors)
	r.metrics["load_requests"] = float64(res.Sent)
	r.metrics["load_achieved_rps"] = float64(len(res.Latencies)) / res.Elapsed.Seconds()
	if len(res.Latencies) > 0 {
		d, _ := Percentile(res.Latencies, 99)
		r.record("load_p99_ms", d)
	}
}

// sse connects the SSE clients and publishes an event to them.
func (r *run) sse(a *api) {
	res, err := runSSE(context.Background(), a, r.cfg.SSEClients)
	if err != nil {
		r.t.Errorf("SSE: %v", err)
		return
	}
	r.metrics[MetricSSEFailed] = float64(res.Failed)
	r.record(MetricSSEMaxMillis, res.Max)
}

// cli times `vh version` and `vh now` as a person at a shell would run them.
func (r *run) cli(srv *server, key string) {
	env := vhEnv(r.work, "VH_URL="+srv.base, "VH_API_KEY="+key, "VH_ACTOR="+ActorHandle)
	for name, c := range map[string]struct {
		p    float64
		args []string
	}{MetricCLIVersionP95: {95, []string{"version"}}, MetricCLINowP95: {95, []string{"now"}}} {
		samples, err := timeCLI(r.bin, env, r.cfg.CLIRuns, c.args...)
		if err != nil {
			r.t.Errorf("%v", err)
			continue
		}
		r.percentile(name, samples, c.p)
	}
}

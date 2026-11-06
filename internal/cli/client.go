package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ExitAuth is the exit code of a refused or missing key (SPEC §8.1).
const ExitAuth = 5

// requestTimeout bounds one request to the server.
const requestTimeout = 30 * time.Second

// Host facts the fallback identity reads. Tests replace them.
var (
	hostnameFn = os.Hostname
	ppidFn     = os.Getppid
	getwdFn    = os.Getwd
)

// identity is who the CLI acts as: a handle and the way back to it.
type identity struct {
	Actor   string
	WayBack string
}

// resolveIdentity reads BOARD_ACTOR and BOARD_WAY_BACK. With no BOARD_ACTOR it
// falls back to agent:<hostname>:<ppid> (SPEC §9), which never reads a
// tool-specific variable.
func resolveIdentity(env Env) identity {
	id := identity{Actor: env("BOARD_ACTOR"), WayBack: env("BOARD_WAY_BACK")}
	if id.Actor != "" {
		return id
	}
	host, err := hostnameFn()
	if err != nil || host == "" {
		host = "unknown"
	}
	ppid := ppidFn()
	id.Actor = "agent:" + host + ":" + strconv.Itoa(ppid)
	if id.WayBack == "" {
		cwd, err := getwdFn()
		if err != nil {
			cwd = "unknown"
		}
		id.WayBack = fmt.Sprintf("pid %d on %s, cwd %s", ppid, host, cwd)
	}
	return id
}

// apiError is an answer the server gave that is not a success: a problem
// document, or a bare status.
type apiError struct {
	Status int
	Detail string
	Next   string
	// ExpiredAt is the `expired_at` of the 410 of an expired lease, when sent.
	ExpiredAt string
	// Exit, when not zero, is the exit code this error ends the command with
	// instead of the one its status maps to.
	Exit int
}

func (e *apiError) Error() string {
	if e.Next == "" {
		return e.Detail
	}
	return e.Detail + " → " + e.Next
}

// exitCode maps the status to the exit codes of SPEC §8.1.
func (e *apiError) exitCode() int {
	if e.Exit != 0 {
		return e.Exit
	}
	switch e.Status {
	case http.StatusUnauthorized:
		return ExitAuth
	case http.StatusNotFound:
		return ExitNotFound
	default:
		return ExitError
	}
}

// authNext is what an agent does about a key the server refused (SPEC §8.1).
const authNext = "check BOARD_API_KEY (board admin keys list shows which keys are active)"

// withNext replaces the suggested next action of an apiError with the given
// status by a command the CLI knows to be the right one: the server's own
// `next` names an HTTP call, the CLI names the command. Any other error stays.
func withNext(err error, status int, next string) error {
	var ae *apiError
	if errors.As(err, &ae) && ae.Status == status {
		return &apiError{Status: ae.Status, Detail: ae.Detail, Next: next}
	}
	return err
}

// problem is the part of an RFC 9457 document the CLI prints.
type problem struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Next   string `json:"next"`
	// ExpiredAt is carried by the 410 of a lease that ran out.
	ExpiredAt string `json:"expired_at"`
}

// newAPIError reads the body of a non-2xx answer.
func newAPIError(status int, body []byte) *apiError {
	var p problem
	_ = json.Unmarshal(body, &p)
	detail := p.Detail
	if detail == "" {
		detail = p.Title
	}
	if detail == "" {
		detail = "server answered " + strconv.Itoa(status)
	}
	return &apiError{Status: status, Detail: detail, Next: p.Next, ExpiredAt: p.ExpiredAt}
}

// client calls the board API as one environment and one actor.
type client struct {
	base string
	key  string
	id   identity
	http *http.Client
	// onBehalf, when not empty, is sent as Board-On-Behalf-Of: the actor a
	// write is made for.
	onBehalf string
}

// newClient checks the environment: BOARD_API_KEY and BOARD_URL. The key comes
// from the environment only.
func newClient(env Env) (*client, error) {
	key := env("BOARD_API_KEY")
	if key == "" {
		return nil, &apiError{Status: http.StatusUnauthorized, Detail: "no BOARD_API_KEY", Next: "export BOARD_API_KEY (this environment's key)"}
	}
	base := strings.TrimRight(env("BOARD_URL"), "/")
	if base == "" {
		return nil, errors.New("no BOARD_URL → export BOARD_URL (the address of the board server)")
	}
	return &client{base: base, key: key, id: resolveIdentity(env), http: &http.Client{Timeout: requestTimeout}}, nil
}

// do sends one request and decodes a 2xx JSON answer into out (when not nil).
func (c *client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("bad BOARD_URL %q → export BOARD_URL (the address of the board server)", c.base)
	}
	c.headers(req, in != nil)
	return c.finish(req, out)
}

func (c *client) headers(req *http.Request, hasBody bool) {
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Board-Actor", c.id.Actor)
	if c.id.WayBack != "" {
		req.Header.Set("Board-Actor-Way-Back", c.id.WayBack)
	}
	if c.onBehalf != "" {
		req.Header.Set("Board-On-Behalf-Of", c.onBehalf)
	}
	req.Header.Set("Accept", "application/json")
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
}

func (c *client) finish(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("server unreachable at %s → check BOARD_URL and that board serve is running", c.base)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("reading the server's answer: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return withNext(newAPIError(resp.StatusCode, raw), http.StatusUnauthorized, authNext)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("the server's answer is not the expected JSON: %w", err)
	}
	return nil
}

// report prints err as one stderr line and returns its exit code.
func report(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintln(stderr, err.Error())
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.exitCode()
	}
	return ExitError
}

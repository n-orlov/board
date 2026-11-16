//go:build perf

package perf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// repoRoot is the checkout: go test runs in ci/perf.
const repoRoot = "../.."

// freeAddr is a loopback address nothing listens on right now.
func freeAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().String(), nil
}

// vhEnv is the whole environment of a vh process: nothing of the caller's VH_ or
// BOARD_ settings leaks in.
func vhEnv(home string, extra ...string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}
	return append(env, extra...)
}

// buildVH builds the checkout's vh binary into dir, or returns VH_PERF_VH when
// that names a prebuilt one.
func buildVH(dir string) (string, error) {
	if bin := os.Getenv("VH_PERF_VH"); bin != "" {
		return filepath.Abs(bin)
	}
	bin := filepath.Join(dir, "vh")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/vh")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build ./cmd/vh: %w\n%s", err, out)
	}
	return bin, nil
}

// server is a running `vh serve`.
type server struct {
	cmd  *exec.Cmd
	base string
	log  *bytes.Buffer
	done chan error
}

// startServer starts `vh serve` on the database at dbPath and returns it with the
// time from the exec until GET /healthz first answered 200.
func startServer(bin, dbPath, home string) (*server, time.Duration, error) {
	addr, err := freeAddr()
	if err != nil {
		return nil, 0, err
	}
	s := &server{base: "http://" + addr, log: &bytes.Buffer{}, done: make(chan error, 1)}
	s.cmd = exec.Command(bin, "serve")
	s.cmd.Env = vhEnv(home, "VH_DB_URL=sqlite://"+dbPath, "VH_LISTEN="+addr)
	s.cmd.Stdout, s.cmd.Stderr = s.log, s.log
	begin := time.Now()
	if err := s.cmd.Start(); err != nil {
		return nil, 0, err
	}
	go func() { s.done <- s.cmd.Wait() }()
	if err := s.waitHealthy(30 * time.Second); err != nil {
		s.stop()
		return nil, 0, err
	}
	return s, time.Since(begin), nil
}

// waitHealthy polls GET /healthz every 5 ms until it answers 200.
func (s *server) waitHealthy(limit time.Duration) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.After(limit)
	for {
		select {
		case err := <-s.done:
			s.done <- err
			return fmt.Errorf("vh serve exited before it answered /healthz: %w\n%s", err, s.log.String())
		case <-deadline:
			return fmt.Errorf("vh serve did not answer /healthz within %v\n%s", limit, s.log.String())
		default:
		}
		if resp, err := client.Get(s.base + "/healthz"); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// rssBytes is the resident set of the server's process, from /proc.
func (s *server) rssBytes() (float64, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(s.cmd.Process.Pid) + "/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			kb, err := strconv.ParseFloat(strings.Fields(rest)[0], 64)
			return kb * 1024, err
		}
	}
	return 0, errors.New("no VmRSS in /proc status")
}

// stop ends the server with SIGTERM and waits for it, killing it only when it
// does not leave within 10 s. It signals only the process this harness started.
func (s *server) stop() {
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case err := <-s.done:
		s.done <- err
	case <-time.After(10 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.done
	}
}

// mintKey makes an API key in the database at dbPath with `vh admin keys create`.
func mintKey(ctx context.Context, bin, dbPath, home string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "admin", "keys", "create", "perf")
	cmd.Env = vhEnv(home, "VH_DB_URL=sqlite://"+dbPath)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("vh admin keys create: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 4 {
		return "", fmt.Errorf("unexpected vh admin keys create output %q", out)
	}
	return fields[len(fields)-1], nil
}

// timeCLI runs the binary runs times (after two runs that are not counted) and
// returns the wall time of each, from exec to exit. A run that exits non-zero
// fails the measurement.
func timeCLI(bin string, env []string, runs int, args ...string) ([]time.Duration, error) {
	out := make([]time.Duration, 0, runs)
	for i := -2; i < runs; i++ {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		begin := time.Now()
		raw, err := cmd.CombinedOutput()
		took := time.Since(begin)
		if err != nil || len(raw) == 0 {
			return nil, fmt.Errorf("vh %s run %d: %w, output %q", strings.Join(args, " "), i, err, raw)
		}
		if i >= 0 {
			out = append(out, took)
		}
	}
	return out, nil
}

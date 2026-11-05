package main

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestRunRefusesToStartWithoutASecretOrAnEmail(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		env  map[string]string
	}{
		"no secret": {[]string{"-email", "a@example.test"}, nil},
		"no email":  {nil, map[string]string{"FAKE_OIDC_CLIENT_SECRET": "x"}},
		"bad flag":  {[]string{"-nope"}, map[string]string{"FAKE_OIDC_CLIENT_SECRET": "x"}},
	} {
		var out bytes.Buffer
		if err := run(context.Background(), tc.args, env(tc.env), &out); err == nil {
			t.Errorf("%s: run succeeded", name)
		}
		if out.Len() != 0 {
			t.Errorf("%s: printed %q before refusing", name, out.String())
		}
	}
}

func TestExitCodeIsOneAndNamesTheProblemWhenRunRefuses(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := exitCode(context.Background(), nil, env(nil), &out, &errOut); code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "FAKE_OIDC_CLIENT_SECRET") {
		t.Fatalf("stderr %q does not name the missing secret", errOut.String())
	}
}

func TestExitCodeIsZeroWhenTheContextEndsTheProvider(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	args := []string{"-email", "a@example.test"}
	if code := exitCode(ctx, args, env(map[string]string{"FAKE_OIDC_CLIENT_SECRET": "x"}), &out, &errOut); code != 0 {
		t.Fatalf("exit code %d (%s), want 0", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "http://") {
		t.Fatalf("stdout %q is not the issuer URL", out.String())
	}
}

func TestRunServesTheProviderUntilItsContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &lockedBuffer{ready: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-client-id", "board", "-email", "a@example.test"}, env(map[string]string{"FAKE_OIDC_CLIENT_SECRET": "x"}), out)
	}()
	select {
	case <-out.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("run printed no URL")
	}
	issuer := strings.TrimSpace(out.String())
	if status, err := get(issuer + "/.well-known/openid-configuration"); err != nil || status != http.StatusOK {
		t.Fatalf("discovery answered %d, %v", status, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := get(issuer + "/jwks"); err == nil {
		t.Fatal("the provider still answers after run returned")
	}
}

// get fetches url and returns the status.
func get(url string) (int, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// lockedBuffer signals when the first line is written.
type lockedBuffer struct {
	bytes.Buffer
	ready chan struct{}
	once  bool
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	n, err := b.Buffer.Write(p)
	if !b.once {
		b.once = true
		close(b.ready)
	}
	return n, err
}

package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	upd "github.com/n-orlov/village-hall/internal/update"
)

const (
	updOld = "old vh\n"
	updNew = "new vh\n"
)

// releaseStub answers the release API and the downloads of example/board for a
// single release, immutable unless immutable says otherwise.
func releaseStub(t *testing.T, tag, immutable string) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	_ = tw.WriteHeader(&tar.Header{Name: "vh", Mode: 0o755, Size: int64(len(updNew)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(updNew))
	_ = tw.Close()
	_ = zw.Close()
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	asset := upd.AssetName("linux", "amd64")
	if immutable == "bad" {
		sum = [32]byte{}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/example/board/releases/tags/") && !strings.HasSuffix(r.URL.Path, "/"+tag):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/repos/example/board/releases/"):
			imm := `"immutable":true,`
			if immutable == "false" {
				imm = `"immutable":false,`
			}
			_, _ = w.Write([]byte(`{` + imm + `"tag_name":"` + tag + `"}`))
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n"))
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// updateEnv installs an old binary in a temporary directory, makes the running
// binary that one at version current, and returns the environment that sends
// vh update to srv.
func updateEnv(t *testing.T, srv *httptest.Server, current string, extra map[string]string) (Env, string) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "vh")
	if err := os.WriteFile(exe, []byte(updOld), 0o755); err != nil {
		t.Fatal(err)
	}
	oldV, oldE := currentVersion, executable
	currentVersion = func() string { return current }
	executable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { currentVersion, executable = oldV, oldE })
	vars := map[string]string{"VH_REPO": "example/board", "VH_API_URL": srv.URL, "VH_DOWNLOAD_URL": srv.URL}
	for k, v := range extra {
		vars[k] = v
	}
	return func(name string) string { return vars[name] }, exe
}

func binaryIs(t *testing.T, exe, want string) {
	t.Helper()
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != want {
		t.Fatalf("binary is %q (%v), want %q", got, err, want)
	}
}

func TestUpdateReplacesTheBinaryAndPrintsTheMove(t *testing.T) {
	env, exe := updateEnv(t, releaseStub(t, "v1.1.0", "true"), "v1.0.0", nil)
	code, out, errs := runEnv(env, "update")
	resolved, _ := filepath.EvalSymlinks(exe)
	if want := "updated: v1.0.0 → v1.1.0 (" + resolved + ")\n"; code != ExitOK || out != want || errs != "" {
		t.Fatalf("exit %d, stdout %q (want %q), stderr %q", code, out, want, errs)
	}
	binaryIs(t, exe, updNew)
}

func TestUpdateCheckSaysWhetherAnUpdateExistsAndWritesNothing(t *testing.T) {
	for _, c := range []struct{ latest, want string }{
		{"v1.1.0", "update available: v1.0.0 → v1.1.0\n"},
		{"v1.0.0", "up to date (v1.0.0)\n"},
		{"v0.9.0", "up to date (v1.0.0)\n"},
	} {
		env, exe := updateEnv(t, releaseStub(t, c.latest, "true"), "v1.0.0", nil)
		code, out, errs := runEnv(env, "update", "--check")
		if code != ExitOK || out != c.want || errs != "" {
			t.Errorf("latest %s: exit %d, stdout %q, stderr %q, want %q", c.latest, code, out, errs, c.want)
		}
		binaryIs(t, exe, updOld)
		if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
			t.Errorf("latest %s: --check left %d files in the directory", c.latest, len(entries))
		}
	}
}

func TestUpdateWithTheRunningReleaseSaysUpToDateAndChangesNothing(t *testing.T) {
	env, exe := updateEnv(t, releaseStub(t, "v1.0.0", "true"), "v1.0.0", nil)
	code, out, _ := runEnv(env, "update")
	if code != ExitOK || out != "up to date (v1.0.0)\n" {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	binaryIs(t, exe, updOld)
}

func TestUpdateVersionPinsARelease(t *testing.T) {
	env, exe := updateEnv(t, releaseStub(t, "v1.1.0", "true"), "v1.0.0", nil)
	code, out, errs := runEnv(env, "update", "--version", "v1.1.0")
	if code != ExitOK || !strings.HasPrefix(out, "updated: v1.0.0 → v1.1.0 (") || errs != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errs)
	}
	binaryIs(t, exe, updNew)
}

func TestUpdateCheckWithAPinnedVersionComparesThatRelease(t *testing.T) {
	env, exe := updateEnv(t, releaseStub(t, "v1.1.0", "true"), "v1.0.0", nil)
	code, out, _ := runEnv(env, "update", "--check", "--version=v1.1.0")
	if code != ExitOK || out != "update available: v1.0.0 → v1.1.0\n" {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	binaryIs(t, exe, updOld)
}

func TestUpdateRefusalsExitOneWithAnArrowAndLeaveTheBinary(t *testing.T) {
	for _, c := range []struct {
		name, current, immutable string
		extra                    map[string]string
		args                     []string
		want                     string
	}{
		{"dev build", "devel+0123456789ab", "true", nil, nil, "dev build"},
		{"dev build with --check", "devel", "true", nil, []string{"--check"}, "dev build"},
		{"not immutable", "v1.0.0", "false", nil, nil, "not an immutable release"},
		{"opt-out value 1 keeps the check", "v1.0.0", "false", map[string]string{"VH_REQUIRE_IMMUTABLE": "1"}, nil, "not an immutable release"},
		{"opt-out value no keeps the check", "v1.0.0", "false", map[string]string{"VH_REQUIRE_IMMUTABLE": "no"}, nil, "not an immutable release"},
		{"checksum mismatch", "v1.0.0", "bad", nil, nil, "checksum mismatch"},
		{"unknown version", "v1.0.0", "true", nil, []string{"--version", "v9.9.9"}, "v9.9.9"},
		{"bad base URL", "v1.0.0", "true", map[string]string{"VH_API_URL": "http://example.com"}, nil, "VH_API_URL"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := releaseStub(t, "v1.1.0", c.immutable)
			env, exe := updateEnv(t, srv, c.current, c.extra)
			code, out, errs := runEnv(env, append([]string{"update"}, c.args...)...)
			if code != ExitError || out != "" || !strings.Contains(errs, " → ") || !strings.Contains(errs, c.want) || strings.Count(errs, "\n") != 1 {
				t.Fatalf("exit %d, stdout %q, stderr %q, want exit 1, no stdout, one line with → and %q", code, out, errs, c.want)
			}
			binaryIs(t, exe, updOld)
		})
	}
}

func TestUpdateRefusesAPackageManagedInstall(t *testing.T) {
	env, _ := updateEnv(t, releaseStub(t, "v1.1.0", "true"), "v1.0.0", nil)
	executable = func() (string, error) { return "/usr/bin/vh", nil }
	code, out, errs := runEnv(env, "update")
	if code != ExitError || out != "" || !strings.Contains(errs, "package manager") || !strings.Contains(errs, " → ") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errs)
	}
}

func TestUpdateRefusesAnUnwritableDirectory(t *testing.T) {
	env, exe := updateEnv(t, releaseStub(t, "v1.1.0", "true"), "v1.0.0", nil)
	dir := filepath.Dir(exe)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	code, out, errs := runEnv(env, "update")
	if code != ExitError || out != "" || !strings.Contains(errs, "cannot write to the install directory") || !strings.Contains(errs, " → ") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errs)
	}
	binaryIs(t, exe, updOld)
}

func TestUpdateOptOutInstallsWithANoteOnStderr(t *testing.T) {
	env, exe := updateEnv(t, releaseStub(t, "v1.1.0", "false"), "v1.0.0", map[string]string{"VH_REQUIRE_IMMUTABLE": "0"})
	code, out, errs := runEnv(env, "update")
	if code != ExitOK || !strings.HasPrefix(out, "updated: ") || !strings.Contains(errs, "VH_REQUIRE_IMMUTABLE=0") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errs)
	}
	binaryIs(t, exe, updNew)
}

func TestUpdateImmutableReleaseNeedsNoNote(t *testing.T) {
	env, _ := updateEnv(t, releaseStub(t, "v1.1.0", "true"), "v1.0.0", map[string]string{"VH_REQUIRE_IMMUTABLE": "0"})
	if _, _, errs := runEnv(env, "update"); errs != "" {
		t.Fatalf("stderr %q: an immutable release was reported as not confirmed", errs)
	}
}

func TestUpdateUsageErrors(t *testing.T) {
	none := func(string) string { return "" }
	for _, args := range [][]string{
		{"update", "now"},
		{"update", "--no-such-flag"},
		{"update", "--check=yes"},
		{"update", "--version"},
		{"update", "--version", "v1.0.0", "--version", "v1.0.1"},
	} {
		code, out, errs := runEnv(none, args...)
		if code != ExitUsage || out != "" || errs != UpdateUsage+"\n" {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out, errs)
		}
	}
}

func TestUpdateOptionsReadTheSameVariablesAsInstallSh(t *testing.T) {
	o := updateOptions(func(string) string { return "" })
	if o.Repo != "n-orlov/board" || o.APIBase != "https://api.github.com" || o.DownloadBase != "https://github.com" || !o.RequireImmutable {
		t.Errorf("defaults = %+v", o)
	}
	set := map[string]string{"VH_REPO": "a/b", "VH_API_URL": "http://127.0.0.1:1", "VH_DOWNLOAD_URL": "http://127.0.0.1:2", "VH_REQUIRE_IMMUTABLE": "0"}
	o = updateOptions(func(n string) string { return set[n] })
	if o.Repo != "a/b" || o.APIBase != "http://127.0.0.1:1" || o.DownloadBase != "http://127.0.0.1:2" || o.RequireImmutable {
		t.Errorf("overrides = %+v", o)
	}
}

func TestUpdateIsInTheCommandTreeWithItsFlags(t *testing.T) {
	flags, ok := Commands()["update"]
	if !ok || strings.Join(flags, " ") != "--check --version" {
		t.Fatalf("Commands()[update] = %v, %v", flags, ok)
	}
}

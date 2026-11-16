package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	oldContent = "old vh binary\n"
	newContent = "new vh binary\n"
	repo       = "example/board"
)

// stub is a release server: the API under /repos/<repo>/releases/... and the
// downloads under /<repo>/releases/download/<tag>/.
type stub struct {
	t         *testing.T
	srv       *httptest.Server
	latest    string // tag_name of releases/latest
	immutable string // raw JSON for the `immutable` member, "" leaves it out
	known     map[string]bool
	archive   []byte
	sums      func(asset string, archive []byte) string
	apiStatus int // 0 = normal

	mu   sync.Mutex
	reqs []string
}

func newStub(t *testing.T, latest string) *stub {
	t.Helper()
	s := &stub{t: t, latest: latest, immutable: "true", known: map[string]bool{latest: true, "v1.2.3": true}}
	s.archive = tarball(t, "vh", newContent)
	s.sums = sumLine
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stub) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reqs...)
}

func (s *stub) release(tag string) string {
	imm := ""
	if s.immutable != "" {
		imm = `"immutable":` + s.immutable + `,`
	}
	return fmt.Sprintf(`{"url":"x","assets":[{"immutable":false}],%s"tag_name":%q}`, imm, tag)
}

func (s *stub) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.reqs = append(s.reqs, r.URL.Path)
	s.mu.Unlock()
	if r.Header.Get("Authorization") != "" {
		s.t.Errorf("a request carried an Authorization header: %s", r.URL.Path)
	}
	p := r.URL.Path
	switch {
	case s.apiStatus != 0 && strings.HasPrefix(p, "/repos/"):
		w.WriteHeader(s.apiStatus)
	case p == "/repos/"+repo+"/releases/latest":
		_, _ = w.Write([]byte(s.release(s.latest)))
	case strings.HasPrefix(p, "/repos/"+repo+"/releases/tags/"):
		tag := strings.TrimPrefix(p, "/repos/"+repo+"/releases/tags/")
		if !s.known[tag] {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(s.release(tag)))
	case strings.HasSuffix(p, "/"+checksumsName):
		_, _ = w.Write([]byte(s.sums("vh_linux_amd64.tar.gz", s.archive)))
	case strings.HasSuffix(p, "/vh_linux_amd64.tar.gz"):
		_, _ = w.Write(s.archive)
	default:
		http.NotFound(w, r)
	}
}

func tarball(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, f := range []struct{ name, body string }{{"README", "docs"}, {name, content}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o600, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(f.body))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// install makes a directory with an old binary in it and options for it.
func (s *stub) install(t *testing.T) (Options, string) {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "vh")
	if err := os.WriteFile(exe, []byte(oldContent), 0o755); err != nil {
		t.Fatal(err)
	}
	return Options{
		Current: "v1.0.0", Exe: exe, Repo: repo, APIBase: s.srv.URL, DownloadBase: s.srv.URL,
		RequireImmutable: true, OS: "linux", Arch: "amd64",
	}, exe
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantRefusal(t *testing.T, err error, detail string) {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("error %v is not a *Refusal", err)
	}
	if !strings.Contains(r.Detail, detail) {
		t.Errorf("refusal %q does not say %q", r.Detail, detail)
	}
	if r.Next == "" || !strings.Contains(err.Error(), " → "+r.Next) {
		t.Errorf("error %q has no → line for %q", err.Error(), r.Next)
	}
}

func TestRunReplacesTheBinaryThroughARenameAtTheSamePath(t *testing.T) {
	s := newStub(t, "v1.1.0")
	o, exe := s.install(t)
	before, _ := os.Stat(exe)
	out, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(exe)
	if !out.Updated || out.Path != resolved || out.Tag != "v1.1.0" || out.Current != "v1.0.0" || out.Immutable != "true" {
		t.Errorf("outcome = %+v", out)
	}
	if got := read(t, exe); got != newContent {
		t.Errorf("content = %q, want the new binary", got)
	}
	after, _ := os.Stat(exe)
	if after.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", after.Mode().Perm())
	}
	if os.SameFile(before, after) {
		t.Error("the file at the old path is the same inode: it was written in place, not renamed")
	}
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 1 {
		t.Errorf("the directory holds %d entries after the update, want only vh", len(entries))
	}
}

func TestRunFollowsASymlinkAndReplacesWhatItPointsAt(t *testing.T) {
	s := newStub(t, "v1.1.0")
	o, exe := s.install(t)
	link := filepath.Join(t.TempDir(), "vh-link")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	o.Exe = link
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if read(t, exe) != newContent {
		t.Error("the binary behind the link was not replaced")
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link itself was replaced: %v %v", fi, err)
	}
}

func TestRunWithTheCurrentReleaseWritesNothing(t *testing.T) {
	for _, latest := range []string{"v1.0.0", "v0.9.0"} {
		s := newStub(t, latest)
		o, exe := s.install(t)
		out, err := Run(context.Background(), o)
		if err != nil || out.Updated || out.Newer {
			t.Fatalf("latest %s: outcome %+v, err %v", latest, out, err)
		}
		if read(t, exe) != oldContent {
			t.Errorf("latest %s: the binary changed", latest)
		}
		for _, r := range s.requests() {
			if strings.Contains(r, "/download/") {
				t.Errorf("latest %s: downloaded %s", latest, r)
			}
		}
	}
}

func TestCheckReportsBothStatesAndWritesNothing(t *testing.T) {
	for _, c := range []struct {
		latest string
		newer  bool
	}{{"v1.2.0", true}, {"v1.0.0", false}, {"v0.9.9", false}, {"v1.0.1", true}, {"v2.0.0", true}, {"v1.0.0-rc.1", false}} {
		s := newStub(t, c.latest)
		o, exe := s.install(t)
		out, err := Check(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		if out.Newer != c.newer || out.Tag != c.latest || out.Current != "v1.0.0" || out.Updated {
			t.Errorf("latest %s: outcome %+v, want newer=%v", c.latest, out, c.newer)
		}
		if read(t, exe) != oldContent {
			t.Errorf("latest %s: the binary changed", c.latest)
		}
		if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
			t.Errorf("latest %s: --check left %d entries in the directory", c.latest, len(entries))
		}
		if got := s.requests(); len(got) != 1 || got[0] != "/repos/"+repo+"/releases/latest" {
			t.Errorf("latest %s: requests %v, want the one release lookup", c.latest, got)
		}
	}
}

func TestCheckStillWorksWhereTheBinaryCannotBeReplaced(t *testing.T) {
	s := newStub(t, "v1.1.0")
	o, _ := s.install(t)
	o.Exe = "/usr/bin/vh"
	out, err := Check(context.Background(), o)
	if err != nil || !out.Newer {
		t.Fatalf("outcome %+v, err %v", out, err)
	}
}

func TestAPinnedVersionInstallsThatReleaseEvenAsADowngrade(t *testing.T) {
	s := newStub(t, "v1.1.0")
	s.known["v0.5.0"] = true
	o, exe := s.install(t)
	o.Pinned = "v0.5.0"
	out, err := Run(context.Background(), o)
	if err != nil || !out.Updated || out.Tag != "v0.5.0" {
		t.Fatalf("outcome %+v, err %v", out, err)
	}
	if read(t, exe) != newContent {
		t.Error("the binary was not replaced")
	}
	var downloaded bool
	for _, r := range s.requests() {
		downloaded = downloaded || r == "/"+repo+"/releases/download/v0.5.0/vh_linux_amd64.tar.gz"
	}
	if !downloaded {
		t.Errorf("requests %v: no download of the v0.5.0 archive", s.requests())
	}
}

func TestAPinnedVersionThatIsTheRunningOneChangesNothing(t *testing.T) {
	s := newStub(t, "v1.1.0")
	s.known["v1.0.0"] = true
	o, exe := s.install(t)
	o.Pinned = "v1.0.0"
	out, err := Run(context.Background(), o)
	if err != nil || out.Updated || out.Newer || read(t, exe) != oldContent {
		t.Fatalf("outcome %+v, err %v", out, err)
	}
}

func TestEveryRefusalSaysWhyWithAnArrowAndLeavesTheBinaryUnchanged(t *testing.T) {
	type refusal struct {
		name   string
		detail string
		setup  func(t *testing.T, s *stub, o *Options)
		// network is whether the refusal may come after a request.
		network bool
	}
	cases := []refusal{
		{"dev build", "dev build", func(_ *testing.T, _ *stub, o *Options) { o.Current = "devel+0123456789ab" }, false},
		{"dirty build", "dev build", func(_ *testing.T, _ *stub, o *Options) { o.Current = "v1.0.0-dirty" }, false},
		{"no version at all", "dev build", func(_ *testing.T, _ *stub, o *Options) { o.Current = "devel" }, false},
		{"clean checkout build", "dev build", func(_ *testing.T, _ *stub, o *Options) { o.Current = "v0.0.0-20261116131445-643cc29bf60f" }, false},
		{"package-managed", "package manager owns", func(_ *testing.T, _ *stub, o *Options) { o.Exe = "/usr/bin/vh" }, false},
		{"under nix", "package manager owns", func(_ *testing.T, _ *stub, o *Options) { o.Exe = "/nix/store/abc-vh/bin/vh" }, false},
		{"symlink into a managed prefix", "package manager owns", func(t *testing.T, _ *stub, o *Options) {
			link := filepath.Join(t.TempDir(), "vh")
			if err := os.Symlink("/bin/sh", link); err != nil {
				t.Fatal(err)
			}
			o.Exe = link
		}, false},
		{"unwritable directory", "cannot write to the install directory", func(t *testing.T, _ *stub, o *Options) {
			dir := filepath.Dir(o.Exe)
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		}, false},
		{"not immutable", "not an immutable release", func(_ *testing.T, s *stub, _ *Options) { s.immutable = "false" }, true},
		{"immutable member missing", "could not be confirmed immutable", func(_ *testing.T, s *stub, _ *Options) { s.immutable = "" }, true},
		{"immutable is the string true", "could not be confirmed immutable", func(_ *testing.T, s *stub, _ *Options) { s.immutable = `"true"` }, true},
		{"release API down", "could not be confirmed immutable", func(_ *testing.T, s *stub, _ *Options) { s.apiStatus = http.StatusBadGateway }, true},
		{"checksum mismatch", "checksum mismatch", func(_ *testing.T, s *stub, _ *Options) {
			s.sums = func(asset string, _ []byte) string { return strings.Repeat("0", 64) + "  " + asset + "\n" }
		}, true},
		{"no checksum line", "no single SHA-256 line", func(_ *testing.T, s *stub, _ *Options) {
			s.sums = func(string, []byte) string { return strings.Repeat("0", 64) + "  other.tar.gz\n" }
		}, true},
		{"two checksum lines", "no single SHA-256 line", func(_ *testing.T, s *stub, _ *Options) {
			s.sums = func(asset string, a []byte) string { l := sumLine(asset, a); return l + l }
		}, true},
		{"short checksum", "no single SHA-256 line", func(_ *testing.T, s *stub, _ *Options) {
			s.sums = func(asset string, _ []byte) string { return "abcd  " + asset + "\n" }
		}, true},
		{"unknown version", "does not exist", func(_ *testing.T, _ *stub, o *Options) { o.Pinned = "v9.9.9" }, true},
		{"malformed version", "not a release tag", func(_ *testing.T, _ *stub, o *Options) { o.Pinned = "latest" }, false},
		{"archive without vh", "holds no vh binary", func(t *testing.T, s *stub, _ *Options) { s.archive = tarball(t, "other", "x") }, true},
		{"archive that is not gzip", "could not replace", func(_ *testing.T, s *stub, _ *Options) { s.archive = []byte("not gzip") }, true},
		{"loopback-only http", "VH_API_URL", func(_ *testing.T, _ *stub, o *Options) { o.APIBase = "http://example.com" }, false},
		{"bad download base", "VH_DOWNLOAD_URL", func(_ *testing.T, _ *stub, o *Options) { o.DownloadBase = "ftp://127.0.0.1" }, false},
		{"unsupported OS", "unsupported platform windows/amd64", func(_ *testing.T, _ *stub, o *Options) { o.OS = "windows" }, false},
		{"unsupported arch", "unsupported platform linux/386", func(_ *testing.T, _ *stub, o *Options) { o.Arch = "386" }, false},
		{"bad repo", "OWNER/NAME", func(_ *testing.T, _ *stub, o *Options) { o.Repo = "no-slash" }, false},
		{"download missing", "could not download", func(t *testing.T, _ *stub, o *Options) {
			gone := httptest.NewServer(http.NotFoundHandler())
			t.Cleanup(gone.Close)
			o.DownloadBase = gone.URL
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStub(t, "v1.1.0")
			o, exe := s.install(t)
			c.setup(t, s, &o)
			_, err := Run(context.Background(), o)
			wantRefusal(t, err, c.detail)
			if got := read(t, exe); got != oldContent {
				t.Errorf("the binary changed to %q", got)
			}
			if !c.network && len(s.requests()) != 0 {
				t.Errorf("a refusal that needs no network sent %v", s.requests())
			}
			if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
				t.Errorf("the directory holds %d entries, a temporary file was left", len(entries))
			}
		})
	}
}

func sumLine(asset string, archive []byte) string {
	sum := sha256.Sum256(archive)
	return hex.EncodeToString(sum[:]) + "  " + asset + "\n"
}

func TestCheckRefusesADevBuildAndABadBaseBeforeAnyRequest(t *testing.T) {
	s := newStub(t, "v1.1.0")
	o, _ := s.install(t)
	o.Current = "devel+0123456789ab"
	_, err := Check(context.Background(), o)
	wantRefusal(t, err, "dev build")
	o.Current, o.APIBase = "v1.0.0", "http://example.com"
	_, err = Check(context.Background(), o)
	wantRefusal(t, err, "VH_API_URL")
	if len(s.requests()) != 0 {
		t.Errorf("requests %v", s.requests())
	}
}

func TestTheFourPlatformsAreSupported(t *testing.T) {
	for _, p := range [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
		if !supported(p[0], p[1]) {
			t.Errorf("%s/%s is not supported", p[0], p[1])
		}
	}
	if supported("", "amd64") || supported("linux", "") {
		t.Error("an empty platform part is supported")
	}
}

func TestAReleaseTheAPIDoesNotKnowIsRefusedNamingTheRepository(t *testing.T) {
	s := newStub(t, "v1.1.0")
	s.apiStatus = http.StatusNotFound
	o, _ := s.install(t)
	_, err := Check(context.Background(), o)
	wantRefusal(t, err, "example/board has no release")
}

func TestCheckOfAnUnknownVersionIsRefused(t *testing.T) {
	s := newStub(t, "v1.1.0")
	o, _ := s.install(t)
	o.Pinned = "v7.7.7"
	_, err := Check(context.Background(), o)
	wantRefusal(t, err, "does not exist")
}

func TestOptingOutInstallsAReleaseThatIsNotConfirmedImmutable(t *testing.T) {
	for name, set := range map[string]func(*stub){
		"false":   func(s *stub) { s.immutable = "false" },
		"missing": func(s *stub) { s.immutable = "" },
	} {
		s := newStub(t, "v1.1.0")
		set(s)
		o, exe := s.install(t)
		o.RequireImmutable = false
		out, err := Run(context.Background(), o)
		if err != nil || !out.Updated || read(t, exe) != newContent {
			t.Fatalf("%s: outcome %+v, err %v", name, out, err)
		}
		if out.Immutable == "true" {
			t.Errorf("%s: the outcome claims the release is immutable", name)
		}
	}
}

func TestOptingOutWithThePinnedReleaseInstallsWhenTheAPIIsDown(t *testing.T) {
	s := newStub(t, "v1.1.0")
	s.apiStatus = http.StatusServiceUnavailable
	o, exe := s.install(t)
	o.RequireImmutable, o.Pinned = false, "v1.2.3"
	out, err := Run(context.Background(), o)
	if err != nil || !out.Updated || out.Immutable != "unknown" || read(t, exe) != newContent {
		t.Fatalf("outcome %+v, err %v", out, err)
	}
}

func TestOptingOutCannotInstallTheLatestWhenTheAPIIsDown(t *testing.T) {
	s := newStub(t, "v1.1.0")
	s.apiStatus = http.StatusServiceUnavailable
	o, exe := s.install(t)
	o.RequireImmutable = false
	_, err := Run(context.Background(), o)
	wantRefusal(t, err, "latest release of example/board is unknown")
	if read(t, exe) != oldContent {
		t.Error("the binary changed")
	}
}

func TestTheAPINamingANonTagIsRefused(t *testing.T) {
	s := newStub(t, "../../evil")
	o, exe := s.install(t)
	_, err := Run(context.Background(), o)
	wantRefusal(t, err, "no usable release tag")
	if read(t, exe) != oldContent {
		t.Error("the binary changed")
	}
}

func TestAPinnedReleaseKeepsTheRequestedTagWhateverTheAPISays(t *testing.T) {
	s := newStub(t, "v1.1.0")
	o, _ := s.install(t)
	o.Pinned = "v1.2.3"
	out, err := Check(context.Background(), o)
	if err != nil || out.Tag != "v1.2.3" || !out.Newer {
		t.Fatalf("outcome %+v, err %v", out, err)
	}
}

func TestARedirectToPlainHTTPIsRefused(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.1.0","immutable":true}`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL+r.URL.Path)
		w.WriteHeader(http.StatusFound)
	}))
	defer redirect.Close()
	s := newStub(t, "v1.1.0")
	o, exe := s.install(t)
	o.APIBase = redirect.URL
	_, err := Run(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "not https") {
		t.Fatalf("err = %v, want the plain-http redirect refused", err)
	}
	if read(t, exe) != oldContent {
		t.Error("the binary changed")
	}
}

func TestAnOversizeAPIAnswerIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte(" "), maxAPIBody+10))
	}))
	defer srv.Close()
	s := newStub(t, "v1.1.0")
	o, _ := s.install(t)
	o.APIBase = srv.URL
	if _, err := Check(context.Background(), o); err == nil {
		t.Fatal("an answer past the limit was accepted")
	}
}

func TestACancelledContextStopsTheRun(t *testing.T) {
	s := newStub(t, "v1.1.0")
	o, exe := s.install(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, o); err == nil {
		t.Fatal("a cancelled run succeeded")
	}
	if read(t, exe) != oldContent {
		t.Error("the binary changed")
	}
}

func TestAnArchiveEntryNamedDotSlashVhIsTheBinary(t *testing.T) {
	s := newStub(t, "v1.1.0")
	s.archive = tarball(t, "./vh", newContent)
	o, exe := s.install(t)
	if _, err := Run(context.Background(), o); err != nil || read(t, exe) != newContent {
		t.Fatalf("err %v", err)
	}
}

func TestAStarPrefixedChecksumNameMatches(t *testing.T) {
	s := newStub(t, "v1.1.0")
	s.sums = func(asset string, a []byte) string {
		sum := sha256.Sum256(a)
		return strings.ToUpper(hex.EncodeToString(sum[:])) + " *" + asset + "\n"
	}
	o, exe := s.install(t)
	if _, err := Run(context.Background(), o); err != nil || read(t, exe) != newContent {
		t.Fatalf("err %v", err)
	}
}

func TestTheAssetNameFollowsInstallSh(t *testing.T) {
	for _, c := range [][3]string{{"linux", "amd64", "vh_linux_amd64.tar.gz"}, {"darwin", "arm64", "vh_darwin_arm64.tar.gz"}} {
		if got := AssetName(c[0], c[1]); got != c[2] {
			t.Errorf("AssetName(%s, %s) = %s, want %s", c[0], c[1], got, c[2])
		}
	}
}

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{
		"v1.2.3": true, "v0.0.1": true, "v10.20.30": true, "v1.2.3-rc.1": true,
		"1.2.3": false, "v1.2": false, "v1.2.3.4": false, "devel": false, "devel+abc": false, "v1.2.3-dirty": false,
		"": false, "v1.2.3+meta": false, "xv1.2.3": false, "v1.2.3\n": false,
		// Pseudo-versions: what a clean checkout build of the module is stamped as.
		"v0.0.0-20261116131445-643cc29bf60f": false, "v1.2.4-0.20261116131445-643cc29bf60f": false,
		"v1.2.3-rc.1.0.20261116131445-643cc29bf60f": false, "v0.0.0-20261116131445-643cc29bf60f-dirty": false,
		"v1.2.3-0.1": true, "v1.2.3-rc.20261116131445": true,
	} {
		if got := IsRelease(v); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestBaseOK(t *testing.T) {
	for base, want := range map[string]bool{
		"https://api.github.com":      true,
		"https://example.com/x":       true,
		"http://127.0.0.1:8080":       true,
		"http://localhost":            true,
		"http://localhost:9/x":        true,
		"http://[::1]":                true,
		"http://[::1]:80/x":           true,
		"http://example.com":          false,
		"http://127.0.0.1.evil.com":   false,
		"http://127.0.0.1:80@evil.io": false,
		"http://[::1]evil.com":        false,
		"https://":                    false,
		"ftp://127.0.0.1":             false,
		"":                            false,
		"127.0.0.1":                   false,
	} {
		if got := baseOK(base); got != want {
			t.Errorf("baseOK(%q) = %v, want %v", base, got, want)
		}
	}
}

func TestManagedPrefix(t *testing.T) {
	for path, want := range map[string]string{
		"/usr/bin/vh":                 "/usr/bin",
		"/usr/bin":                    "/usr/bin",
		"/usr/bin/../local/bin/vh":    "",
		"/usr/local/bin/vh":           "",
		"/usr/binary/vh":              "",
		"/bin/vh":                     "/bin",
		"/opt/homebrew/bin/vh":        "/opt/homebrew",
		"/usr/local/Cellar/vh/1/bin":  "/usr/local/Cellar",
		"/home/linuxbrew/.linuxbrew":  "/home/linuxbrew",
		"/nix/store/x/bin/vh":         "/nix/store",
		"/snap/vh/1/vh":               "/snap",
		"/usr/lib/x/vh":               "/usr/lib",
		"/usr/libexec/vh":             "/usr/libexec",
		"/usr/sbin/vh":                "/usr/sbin",
		"/sbin/vh":                    "/sbin",
		"/home/me/.local/bin/vh":      "",
		"/tmp/x/vh":                   "",
		"/snapshot/vh":                "",
		"/usr/local/Cellarx/vh":       "",
		"/opt/homebrewed/vh":          "",
		"/home/linuxbrewer/vh":        "",
		"/nix/storey/vh":              "",
		"/nix/store":                  "/nix/store",
		"/usr/lib/../../home/me/x/vh": "",
	} {
		if got := managedPrefix(path); got != want {
			t.Errorf("managedPrefix(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestRefusalErrorJoinsTheDetailAndTheNextWithAnArrow(t *testing.T) {
	err := refuse("bad %s", "do %s", "x")
	if got := err.Error(); got != "bad x → do %s" {
		t.Errorf("error = %q", got)
	}
}

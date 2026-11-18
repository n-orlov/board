// Package update implements `vh update`: it finds a release of the public
// repository, checks that it is immutable, downloads the archive for this
// platform, verifies its SHA-256 against checksums.txt of the same release and
// replaces the running binary through a temporary file and a rename. It reads
// no token: the repository is public, so every request is anonymous.
//
// Nothing here prints. A refusal is returned as a *Refusal, which carries the
// reason and what to do next, and the binary on disk is unchanged whenever one
// is returned.
package update

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/n-orlov/village-hall/internal/version"
)

// Default locations of the release API and of the release downloads, and the
// default repository. They are the same as install.sh's.
const (
	DefaultRepo        = "n-orlov/village-hall"
	DefaultAPIBase     = "https://api.github.com"
	DefaultDownloadURL = "https://github.com"
)

// Options is everything one run needs. The command line and the environment
// are read by the caller; nothing here touches either.
type Options struct {
	Current          string // version of the running binary, e.g. v1.2.3
	Exe              string // path of the running binary
	Repo             string // OWNER/NAME
	APIBase          string // base URL of the release API
	DownloadBase     string // base URL of the release downloads
	RequireImmutable bool   // refuse a release not confirmed immutable
	Pinned           string // --version: the release tag to install, or ""
	OS, Arch         string // platform names as the asset uses them
	Client           *http.Client
}

// Outcome is what a Check or a Run found.
type Outcome struct {
	Current   string // version of the running binary
	Tag       string // release the run is about
	Newer     bool   // Tag is a different release from Current
	Updated   bool   // the binary was replaced (Run only)
	Path      string // the binary that was replaced (Run only)
	Immutable string // "true", "false" or "unknown": what the release API said
}

var (
	tagPattern  = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
)

// IsRelease reports whether v names a release: vMAJOR.MINOR.PATCH with an
// optional pre-release part. A checkout build is not one, whichever way the
// toolchain stamped it: devel+abc, a -dirty tree, or the pseudo-version a clean
// checkout of a module gets (v0.0.0-20261116131445-643cc29bf60f).
func IsRelease(v string) bool {
	return tagPattern.MatchString(v) && !strings.HasSuffix(v, version.DirtySuffix) && !module.IsPseudoVersion(v)
}

// Check reports which release a Run would install, and whether that differs
// from the running version. It writes nothing. A dev build is refused, since
// it has no release to compare with; a package-managed or unwritable install
// is not, because nothing is written.
func Check(ctx context.Context, o Options) (Outcome, error) {
	if err := o.validate(); err != nil {
		return Outcome{}, err
	}
	rel, err := o.release(ctx)
	if err != nil {
		return Outcome{}, err
	}
	return o.outcome(rel), nil
}

// Run replaces the running binary with the release. The refusals that need no
// network (dev build, package-managed install, unwritable directory) come
// first, then the release is looked up; the binary is touched last, by one
// rename.
func Run(ctx context.Context, o Options) (Outcome, error) {
	if err := o.validate(); err != nil {
		return Outcome{}, err
	}
	target, err := o.target()
	if err != nil {
		return Outcome{}, err
	}
	rel, err := o.release(ctx)
	if err != nil {
		return Outcome{}, err
	}
	out := o.outcome(rel)
	if !out.Newer {
		return out, nil
	}
	if err := o.install(ctx, rel, target); err != nil {
		return Outcome{}, err
	}
	out.Updated, out.Path = true, target
	return out, nil
}

// outcome compares the release with the running version. Without --version the
// release must be newer; with it, any different release is wanted, so a
// downgrade is allowed.
func (o Options) outcome(rel release) Outcome {
	out := Outcome{Current: o.Current, Tag: rel.tag, Immutable: rel.immutable}
	if o.Pinned != "" {
		out.Newer = o.Pinned != o.Current
	} else {
		out.Newer = semver.Compare(rel.tag, o.Current) > 0
	}
	return out
}

// validate checks the options before any request or file access.
func (o Options) validate() error {
	if !IsRelease(o.Current) {
		return refuse("this is a dev build (version %q), not a release", "install a release with install.sh", o.Current)
	}
	if !supported(o.OS, o.Arch) {
		return refuse("unsupported platform %s/%s: vh is built for linux and darwin on amd64 and arm64", "install a release by hand", o.OS, o.Arch)
	}
	if !repoPattern.MatchString(o.Repo) {
		return refuse("VH_REPO %q is not OWNER/NAME", "set VH_REPO=OWNER/NAME", o.Repo)
	}
	if o.Pinned != "" && !IsRelease(o.Pinned) {
		return refuse("%q is not a release tag (vX.Y.Z)", "run vh update --check to see the latest release", o.Pinned)
	}
	return o.validateBases()
}

// supported reports whether releases carry an archive for the platform.
func supported(goos, goarch string) bool {
	return slices.Contains([]string{"linux", "darwin"}, goos) && slices.Contains([]string{"amd64", "arm64"}, goarch)
}

// validateBases refuses a base URL that is neither https nor loopback http.
func (o Options) validateBases() error {
	for _, b := range []struct{ name, url string }{{"VH_API_URL", o.APIBase}, {"VH_DOWNLOAD_URL", o.DownloadBase}} {
		if !baseOK(b.url) {
			return refuse("%s %q is refused: only https is allowed (http only for 127.0.0.1, localhost or [::1])", "unset "+b.name+" to use the default", b.name, b.url)
		}
	}
	return nil
}

// baseOK accepts an https:// URL, or an http:// URL whose host is loopback.
func baseOK(base string) bool {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return slices.Contains([]string{"127.0.0.1", "localhost", "::1"}, u.Hostname())
	}
	return false
}

// Refusal is an update that did not go ahead, with nothing changed. Next says
// what to do about it.
type Refusal struct {
	Detail string
	Next   string
}

func (r *Refusal) Error() string { return r.Detail + " → " + r.Next }

func refuse(detail, next string, args ...any) error {
	return &Refusal{Detail: fmt.Sprintf(detail, args...), Next: next}
}

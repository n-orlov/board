package update

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	checksumsName = "checksums.txt"
	maxAPIBody    = 1 << 20
	maxChecksums  = 1 << 20
	maxArchive    = 256 << 20
	requestLimit  = 5 * time.Minute
)

// release is what the release API said about the release to install.
type release struct {
	tag       string
	immutable string // "true", "false" or "unknown"
}

// AssetName is the archive of this platform: vh_<os>_<arch>.tar.gz.
func AssetName(goos, goarch string) string { return "vh_" + goos + "_" + goarch + ".tar.gz" }

// client is the HTTP client: https only for redirects, and a bounded run time.
func (o Options) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{
		Timeout: requestLimit,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("a redirect to a URL that is not https is refused")
			}
			return nil
		},
	}
}

// get fetches url anonymously and returns at most limit bytes; a longer body
// is an error, not a truncated success.
func (o Options) get(ctx context.Context, url string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "vh-update")
	resp, err := o.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(body)) > limit {
		return nil, resp.StatusCode, fmt.Errorf("the answer is larger than %d bytes", limit)
	}
	return body, resp.StatusCode, nil
}

// release asks the release API for the latest release, or for the pinned one,
// and applies the immutability rule.
func (o Options) release(ctx context.Context) (release, error) {
	path := "/repos/" + o.Repo + "/releases/latest"
	if o.Pinned != "" {
		path = "/repos/" + o.Repo + "/releases/tags/" + o.Pinned
	}
	body, status, err := o.get(ctx, strings.TrimSuffix(o.APIBase, "/")+path, maxAPIBody)
	if status == http.StatusNotFound {
		return release{}, o.notFound()
	}
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("status %d", status)
	}
	rel := release{tag: o.Pinned, immutable: "unknown"}
	if err != nil {
		return o.unanswered(rel, err)
	}
	return o.admit(o.read(body, rel))
}

// unanswered is the release when the API gave no usable answer: refused when
// immutability is required, installable only when the tag is pinned otherwise.
func (o Options) unanswered(rel release, err error) (release, error) {
	if o.RequireImmutable {
		return rel, refuse("release %s of %s could not be confirmed immutable: the release API gave no usable answer (%v)",
			"retry later, or set VH_REQUIRE_IMMUTABLE=0 to install it anyway", cmp.Or(rel.tag, "latest"), o.Repo, err)
	}
	if rel.tag == "" {
		return rel, refuse("the release API gave no usable answer (%v), so the latest release of %s is unknown",
			"retry later, or pin one with --version vX.Y.Z", err, o.Repo)
	}
	return rel, nil
}

// notFound is the refusal for a release the API does not know.
func (o Options) notFound() error {
	if o.Pinned != "" {
		return refuse("release %s of %s does not exist", "run vh update --check to see the latest release", o.Pinned, o.Repo)
	}
	return refuse("%s has no release", "check VH_REPO", o.Repo)
}

// read takes the tag and the immutable flag out of a release object. Only a
// top-level `immutable` that is exactly true counts as immutable.
func (o Options) read(body []byte, rel release) release {
	var doc struct {
		Tag       string          `json:"tag_name"`
		Immutable json.RawMessage `json:"immutable"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return rel
	}
	switch string(bytes.TrimSpace(doc.Immutable)) {
	case "true":
		rel.immutable = "true"
	case "false":
		rel.immutable = "false"
	}
	if o.Pinned == "" {
		rel.tag = doc.Tag
	}
	return rel
}

// admit applies the immutability rule and checks the tag the API named.
func (o Options) admit(rel release) (release, error) {
	if !IsRelease(rel.tag) {
		return rel, refuse("the release API named no usable release tag for %s", "retry later, or pin one with --version vX.Y.Z", o.Repo)
	}
	if rel.immutable == "true" || !o.RequireImmutable {
		return rel, nil
	}
	if rel.immutable == "false" {
		return rel, refuse("release %s of %s is not an immutable release, and an immutable release is required",
			"set VH_REQUIRE_IMMUTABLE=0 to install it anyway", rel.tag, o.Repo)
	}
	return rel, refuse("release %s of %s could not be confirmed immutable: the release API gave no usable answer",
		"retry later, or set VH_REQUIRE_IMMUTABLE=0 to install it anyway", rel.tag, o.Repo)
}

// install downloads the archive and checksums.txt of rel, verifies the archive
// and replaces target.
func (o Options) install(ctx context.Context, rel release, target string) error {
	asset := AssetName(o.OS, o.Arch)
	base := strings.TrimSuffix(o.DownloadBase, "/") + "/" + o.Repo + "/releases/download/" + rel.tag + "/"
	archive, err := o.download(ctx, base+asset, maxArchive)
	if err != nil {
		return err
	}
	sums, err := o.download(ctx, base+checksumsName, maxChecksums)
	if err != nil {
		return err
	}
	if err := verify(asset, archive, sums); err != nil {
		return err
	}
	return replace(target, archive)
}

// download is a GET that must answer 200.
func (o Options) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	body, status, err := o.get(ctx, url, limit)
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("status %d", status)
	}
	if err != nil {
		return nil, refuse("could not download %s: %v", "check the connection and retry; nothing was installed", url, err)
	}
	return body, nil
}

// verify checks the archive against its single line in checksums.txt.
func verify(asset string, archive, sums []byte) error {
	var want []string
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			want = append(want, strings.ToLower(f[0]))
		}
	}
	if len(want) != 1 || len(want[0]) != 64 {
		return refuse("%s has no single SHA-256 line for %s", "report the release; nothing was installed", checksumsName, asset)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want[0] {
		return refuse("checksum mismatch for %s (expected %s, got %s)", "retry later or report the release; nothing was installed", asset, want[0], got)
	}
	return nil
}

package cli

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var durationPattern = regexp.MustCompile(`^([0-9]+)([smhd])$`)

var unitSeconds = map[string]int64{"s": 1, "m": 60, "h": 3600, "d": 86400}

// parseDuration reads a duration written as a whole number and a unit: s, m, h
// or d (`90s`, `45m`, `2h`, `7d`). It returns the seconds. A missing unit, a
// sign, a fraction, a zero and anything else is an error.
func parseDuration(s string) (int64, error) {
	m := durationPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid duration %q: want a whole number and a unit s, m, h or d, such as 90m", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n == 0 || n > math.MaxInt64/unitSeconds[m[2]] {
		return 0, fmt.Errorf("invalid duration %q: want between 1 and a very large number of %s", s, m[2])
	}
	return n * unitSeconds[m[2]], nil
}

// formatDuration is the shortest exact form of a number of seconds: 86400 is
// 1d, 5400 is 90m, 90 is 90s.
func formatDuration(seconds int64) string {
	for _, u := range []string{"d", "h", "m"} {
		if seconds >= unitSeconds[u] && seconds%unitSeconds[u] == 0 {
			return strconv.FormatInt(seconds/unitSeconds[u], 10) + u
		}
	}
	return strconv.FormatInt(seconds, 10) + "s"
}

// parseTime reads an RFC 3339 time or a `YYYY-MM-DD` date (midnight UTC) and
// returns it as an RFC 3339 UTC time.
func parseTime(s string) (string, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.Format(time.RFC3339), nil
	}
	return "", fmt.Errorf("invalid time %q: want RFC 3339 (2026-03-04T09:30:00Z) or YYYY-MM-DD", s)
}

// extRef is a link to a ticket, pull request or build.
type extRef struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// parseLink reads `label=url`: the label is everything before the first `=`.
func parseLink(s string) (extRef, error) {
	label, url, ok := strings.Cut(s, "=")
	if !ok || strings.TrimSpace(label) == "" || strings.TrimSpace(url) == "" {
		return extRef{}, fmt.Errorf("invalid link %q: want label=url", s)
	}
	return extRef{Label: label, URL: url}, nil
}

// flagTime reads the time flag name into body[field], when it is given.
func flagTime(p parsedArgs, body map[string]any, name, field string) error {
	v := last(p.values[name])
	if v == "" {
		return nil
	}
	t, err := parseTime(v)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	body[field] = t
	return nil
}

// flagCadence reads --cadence into body["cadence_seconds"], when it is given.
func flagCadence(p parsedArgs, body map[string]any) error {
	v := last(p.values["--cadence"])
	if v == "" {
		return nil
	}
	n, err := parseDuration(v)
	if err != nil {
		return fmt.Errorf("--cadence: %w", err)
	}
	body["cadence_seconds"] = n
	return nil
}

// flagLinks reads the repeated --link into body["external_refs"]. With empty
// it sets an empty list, which is what --no-links asks for (the caller has
// refused --link with --no-links).
func flagLinks(p parsedArgs, body map[string]any, empty bool) error {
	links := p.values["--link"]
	if empty {
		body["external_refs"] = []extRef{}
		return nil
	}
	if len(links) == 0 {
		return nil
	}
	refs := make([]extRef, 0, len(links))
	for _, l := range links {
		r, err := parseLink(l)
		if err != nil {
			return fmt.Errorf("--link: %w", err)
		}
		refs = append(refs, r)
	}
	body["external_refs"] = refs
	return nil
}

// commonFlags reads the flags task add and task edit share: --start, --end,
// --cadence and --link into body.
func commonFlags(p parsedArgs, body map[string]any, noLinks bool) error {
	if err := flagTime(p, body, "--start", "planned_start"); err != nil {
		return err
	}
	if err := flagTime(p, body, "--end", "planned_end"); err != nil {
		return err
	}
	if err := flagCadence(p, body); err != nil {
		return err
	}
	return flagLinks(p, body, noLinks)
}

package timeline

import (
	"errors"
	"fmt"
	"time"

	"github.com/n-orlov/village-hall/internal/domain"
)

// Window defaults and limit (SPEC 6.10).
const (
	DefaultBefore = 14 * 24 * time.Hour
	DefaultAhead  = 7 * 24 * time.Hour
	MaxSpan       = 400 * 24 * time.Hour
)

// ErrInvalid is a query the caller got wrong.
var ErrInvalid = errors.New("invalid timeline query")

// InvalidError is a parameter the query got wrong. It satisfies errors.Is(err,
// ErrInvalid), its text is the sentence to show the caller and Next says what
// to send instead.
type InvalidError struct{ Msg, Next string }

// Error is the sentence to show the caller.
func (e InvalidError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrInvalid) true for every InvalidError.
func (e InvalidError) Is(target error) bool { return target == ErrInvalid }

const nextWindow = "Send from and to as RFC 3339 times such as 2026-03-01T00:00:00Z, with to after from and at most 400 days apart, or leave them out for the last 14 days to 7 days ahead."

// window is the [from, to] instant range of a query: the times it sends, or
// the default around now for each it leaves out.
func window(q Query, now time.Time) (time.Time, time.Time, error) {
	from, err := instant("from", q.From, now.Add(-DefaultBefore))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := instant("to", q.To, now.Add(DefaultAhead))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, InvalidError{Msg: "to must be after from", Next: nextWindow}
	}
	if to.Sub(from) > MaxSpan {
		return time.Time{}, time.Time{}, InvalidError{Msg: "from and to are more than 400 days apart", Next: nextWindow}
	}
	return from, to, nil
}

// instant reads an RFC 3339 time, def when text is empty.
func instant(name, text string, def time.Time) (time.Time, error) {
	if text == "" {
		return def.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, InvalidError{Msg: fmt.Sprintf("%s %q is not an RFC 3339 time", name, text), Next: nextWindow}
	}
	return t.UTC(), nil
}

const nextClosedWithin = "Send closed_within as 7d, 30d, 90d or a number of days, or leave it out to hide closed tasks."

// since is the earliest closed_at that closed_within keeps, and whether the
// filter is on at all.
func since(q Query, now time.Time) (time.Time, bool, error) {
	if q.ClosedWithin == "" {
		return time.Time{}, false, nil
	}
	days, err := domain.ParseDays(q.ClosedWithin)
	if err != nil {
		return time.Time{}, false, InvalidError{Msg: err.Error(), Next: nextClosedWithin}
	}
	return now.AddDate(0, 0, -days), true, nil
}

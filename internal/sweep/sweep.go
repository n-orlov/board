// Package sweep runs the background expiry of board serve: it marks live
// leases whose time has run out as expired and deletes expired sessions, on a
// fixed interval (VH_SWEEP_INTERVAL, SPEC §5.1 and §13).
package sweep

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultInterval is the sweep interval when VH_SWEEP_INTERVAL is unset.
const DefaultInterval = 15 * time.Second

// Disabled is the setting that turns the sweeper off. "0" and "0s" do too.
const Disabled = "off"

// ParseInterval reads VH_SWEEP_INTERVAL. An empty value is DefaultInterval,
// a Go duration above zero is that interval, and "off" (or a zero duration)
// returns 0, which means no sweeper. Anything else is an error.
func ParseInterval(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return DefaultInterval, nil
	}
	if strings.EqualFold(v, Disabled) {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("VH_SWEEP_INTERVAL=%q: want a duration such as 15s, or %s", v, Disabled)
	}
	return d, nil
}

// Job is one thing the sweeper does on each tick.
type Job func(ctx context.Context) error

// Run calls every job once per interval until ctx ends. An interval of zero
// runs nothing and returns at once. A job's error goes to report (when set)
// and never stops the sweeper.
func Run(ctx context.Context, interval time.Duration, report func(error), jobs ...Job) {
	if interval <= 0 {
		return
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			Once(ctx, report, jobs...)
		}
	}
}

// Once runs every job one time, reporting the errors.
func Once(ctx context.Context, report func(error), jobs ...Job) {
	for _, j := range jobs {
		if err := j(ctx); err != nil && report != nil && ctx.Err() == nil {
			report(err)
		}
	}
}

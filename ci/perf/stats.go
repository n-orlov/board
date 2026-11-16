package perf

import (
	"errors"
	"math"
	"sort"
	"time"
)

// ErrNoSamples is returned for a percentile of nothing.
var ErrNoSamples = errors.New("no samples")

// Percentile returns the p-th percentile (0 < p <= 100) of the samples by the
// nearest-rank method: the smallest sample such that at least p percent of the
// samples are at or below it. The slice is not changed.
func Percentile(samples []time.Duration, p float64) (time.Duration, error) {
	if len(samples) == 0 {
		return 0, ErrNoSamples
	}
	if !(p > 0 && p <= 100) {
		return 0, errors.New("percentile must be above 0 and at most 100")
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	return sorted[rank-1], nil
}

// Millis is d in milliseconds, as the report states durations.
func Millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

package perf

import (
	"errors"
	"testing"
	"time"
)

func ms100(n ...int) []time.Duration {
	out := make([]time.Duration, len(n))
	for i, v := range n {
		out[i] = time.Duration(v) * time.Millisecond
	}
	return out
}

func TestPercentileIsTheNearestRank(t *testing.T) {
	ten := ms100(10, 20, 30, 40, 50, 60, 70, 80, 90, 100)
	hundred := make([]time.Duration, 100)
	for i := range hundred {
		hundred[i] = time.Duration(i+1) * time.Millisecond
	}
	for _, tc := range []struct {
		name    string
		samples []time.Duration
		p       float64
		want    time.Duration
	}{
		{"p50 of ten is the fifth", ten, 50, 50 * time.Millisecond},
		{"p95 of ten rounds the rank up to the tenth", ten, 95, 100 * time.Millisecond},
		{"p90 of ten is exactly the ninth", ten, 90, 90 * time.Millisecond},
		{"p99 of a hundred is the 99th", hundred, 99, 99 * time.Millisecond},
		{"p100 is the largest", hundred, 100, 100 * time.Millisecond},
		{"a tiny p is the smallest", hundred, 0.5, 1 * time.Millisecond},
		{"one sample is every percentile", ms100(7), 99, 7 * time.Millisecond},
		{"unsorted input", ms100(30, 10, 20), 100, 30 * time.Millisecond},
		{"unsorted input, low rank", ms100(30, 10, 20), 34, 20 * time.Millisecond},
	} {
		got, err := Percentile(tc.samples, tc.p)
		if err != nil || got != tc.want {
			t.Errorf("%s: Percentile = %v, %v, want %v", tc.name, got, err, tc.want)
		}
	}
}

func TestPercentileLeavesTheSamplesAlone(t *testing.T) {
	in := ms100(30, 10, 20)
	if _, err := Percentile(in, 50); err != nil {
		t.Fatal(err)
	}
	if in[0] != 30*time.Millisecond || in[1] != 10*time.Millisecond || in[2] != 20*time.Millisecond {
		t.Fatalf("samples were reordered: %v", in)
	}
}

func TestPercentileRefusesWhatItCannotAnswer(t *testing.T) {
	if _, err := Percentile(nil, 99); !errors.Is(err, ErrNoSamples) {
		t.Errorf("no samples: err = %v, want ErrNoSamples", err)
	}
	for _, p := range []float64{0, -1, 100.5, 101} {
		if _, err := Percentile(ms100(1, 2), p); err == nil {
			t.Errorf("p = %v: want an error", p)
		}
	}
}

func TestMillis(t *testing.T) {
	if got := Millis(1500 * time.Microsecond); got != 1.5 {
		t.Fatalf("Millis(1.5ms) = %v", got)
	}
}

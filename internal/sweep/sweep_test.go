package sweep

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseInterval(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		bad  bool
	}{
		{"", DefaultInterval, false},
		{"  ", DefaultInterval, false},
		{"2s", 2 * time.Second, false},
		{"1m30s", 90 * time.Second, false},
		{"off", 0, false},
		{"OFF", 0, false},
		{"0", 0, false},
		{"0s", 0, false},
		{"soon", 0, true},
		{"15", 0, true},
		{"-5s", 0, true},
	}
	for _, c := range cases {
		got, err := ParseInterval(c.in)
		if (err != nil) != c.bad || got != c.want {
			t.Errorf("ParseInterval(%q) = %v, %v; want %v, bad=%v", c.in, got, err, c.want, c.bad)
		}
	}
	if DefaultInterval != 15*time.Second {
		t.Errorf("default interval %v, want 15s", DefaultInterval)
	}
}

func TestRunTicksAndReports(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var runs, reports atomic.Int32
	done := make(chan struct{})
	go func() {
		Run(ctx, time.Millisecond, func(error) { reports.Add(1) },
			func(context.Context) error { runs.Add(1); return errors.New("boom") },
			func(context.Context) error { runs.Add(1); return nil })
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for reports.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if reports.Load() < 2 || runs.Load() < 4 {
		t.Fatalf("runs %d, reports %d: want the sweeper to keep going after an error", runs.Load(), reports.Load())
	}
}

func TestRunDisabledReturnsAtOnce(t *testing.T) {
	called := false
	Run(context.Background(), 0, nil, func(context.Context) error { called = true; return nil })
	if called {
		t.Fatal("a disabled sweeper ran a job")
	}
}

func TestOnceStaysQuietAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	Once(ctx, func(error) { t.Error("reported an error after shutdown") }, func(context.Context) error { return errors.New("x") })
}

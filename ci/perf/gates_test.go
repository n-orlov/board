package perf

import (
	"strings"
	"testing"
)

// passing is a measurement that sits exactly on every limit: the gates are
// "at most", so it must pass.
func passing() map[string]float64 {
	m := map[string]float64{}
	for _, g := range Gates {
		m[g.Metric] = g.Max
	}
	return m
}

func TestGatesAreSpec14sNumbers(t *testing.T) {
	want := map[string]float64{
		MetricIdleRSSBytes: 40e6, MetricColdEmptyMillis: 1000, MetricColdSeededMillis: 3000,
		MetricNowP99Millis: 20, MetricAttentionP99: 20, MetricRenewP99Millis: 10,
		MetricLoadErrors: 0, MetricSSEFailed: 0, MetricSSEMaxMillis: 5000,
		MetricCLINowP95: 100, MetricCLIVersionP95: 20,
	}
	if len(Gates) != len(want) {
		t.Fatalf("%d gates, want %d", len(Gates), len(want))
	}
	targets := map[string]bool{}
	for _, g := range Gates {
		limit, ok := want[g.Metric]
		if !ok || g.Max != limit {
			t.Errorf("gate %s: limit %v, want %v (known: %v)", g.Metric, g.Max, limit, ok)
		}
		targets[g.Target] = true
	}
	if len(targets) != 8 {
		t.Errorf("gates cover %d R14 targets, want 8: %v", len(targets), targets)
	}
}

func TestMeasurementsOnTheLimitPass(t *testing.T) {
	checks := Evaluate(passing())
	if len(checks) != len(Gates) {
		t.Fatalf("%d checks for %d gates", len(checks), len(Gates))
	}
	if f := Failures(checks); len(f) != 0 {
		t.Fatalf("failures on the limits: %+v", f)
	}
	for _, c := range checks {
		if !c.Measured || !c.Pass {
			t.Errorf("%s: measured=%v pass=%v", c.Metric, c.Measured, c.Pass)
		}
	}
}

func TestEveryBreachedGateFails(t *testing.T) {
	for _, g := range Gates {
		m := passing()
		m[g.Metric] = g.Max + 1
		failed := Failures(Evaluate(m))
		if len(failed) != 1 || failed[0].Metric != g.Metric {
			t.Errorf("breaching %s: failures = %+v, want exactly that gate", g.Metric, failed)
			continue
		}
		if !failed[0].Measured || failed[0].Value != g.Max+1 || !strings.Contains(failed[0].Detail, "above the limit") {
			t.Errorf("breaching %s: check = %+v", g.Metric, failed[0])
		}
	}
}

func TestAJustUnderMeasurementPassesAndAJustOverFails(t *testing.T) {
	m := passing()
	m[MetricNowP99Millis] = 19.99
	if f := Failures(Evaluate(m)); len(f) != 0 {
		t.Fatalf("19.99 ms against 20: %+v", f)
	}
	m[MetricNowP99Millis] = 20.01
	if f := Failures(Evaluate(m)); len(f) != 1 {
		t.Fatalf("20.01 ms against 20: %+v", f)
	}
}

func TestAGateThatWasNotMeasuredFails(t *testing.T) {
	for _, g := range Gates {
		m := passing()
		delete(m, g.Metric)
		failed := Failures(Evaluate(m))
		if len(failed) != 1 || failed[0].Metric != g.Metric || failed[0].Measured || !strings.Contains(failed[0].Detail, "not measured") {
			t.Errorf("dropping %s: failures = %+v", g.Metric, failed)
		}
	}
	if f := Failures(Evaluate(nil)); len(f) != len(Gates) {
		t.Fatalf("no measurements: %d failures, want %d", len(f), len(Gates))
	}
}

func TestDetailNamesTheNumbersPlainly(t *testing.T) {
	m := passing()
	m[MetricIdleRSSBytes] = 41234567.891
	failed := Failures(Evaluate(m))
	if len(failed) != 1 || failed[0].Detail != "41234567.89 bytes is above the limit of 40000000" {
		t.Fatalf("detail = %+v", failed)
	}
}

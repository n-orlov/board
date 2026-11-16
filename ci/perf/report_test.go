package perf

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

func TestAPassingRunIsReportedPassed(t *testing.T) {
	r := NewReport(DefaultConfig(), passing(), t0, t0.Add(5*time.Minute))
	if !r.Passed || r.Reduced || len(r.Checks) != len(Gates) {
		t.Fatalf("report = passed %v reduced %v checks %d", r.Passed, r.Reduced, len(r.Checks))
	}
}

func TestABreachedGateFailsTheRunAndTheReportSaysWhich(t *testing.T) {
	m := passing()
	m[MetricRenewP99Millis] = 10.5
	r := NewReport(DefaultConfig(), m, t0, t0)
	if r.Passed {
		t.Fatal("a run with a breached gate passed")
	}
	var bad []ReportCheck
	for _, c := range r.Checks {
		if !c.Pass {
			bad = append(bad, c)
		}
	}
	if len(bad) != 1 || bad[0].Metric != MetricRenewP99Millis || bad[0].Value != 10.5 || bad[0].Limit != 10 || !bad[0].Measured {
		t.Fatalf("failed checks = %+v", bad)
	}
}

func TestAReducedRunSaysSo(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Events = 1000
	r := NewReport(cfg, passing(), t0, t0)
	if !r.Reduced {
		t.Fatal("reduced run not marked")
	}
}

func TestPerfJSONHoldsTheMetricsAndTheVerdict(t *testing.T) {
	m := passing()
	m[MetricNowP99Millis] = 25
	path := filepath.Join(t.TempDir(), "ci-results", "perf.json")
	if err := NewReport(DefaultConfig(), m, t0, t0.Add(time.Minute)).WriteJSON(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Passed || got.Metrics[MetricNowP99Millis] != 25 || got.Config != DefaultConfig() || len(got.Checks) != len(Gates) {
		t.Fatalf("perf.json = %+v", got)
	}
	if !got.StartedAt.Equal(t0) || !got.FinishedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("times = %v %v", got.StartedAt, got.FinishedAt)
	}
}

func readSuite(t *testing.T, path string) junitSuit {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc junitSuites
	if err := xml.Unmarshal(data, &doc); err != nil || len(doc.Suites) != 1 {
		t.Fatalf("JUnit unreadable: %v, %d suites\n%s", err, len(doc.Suites), data)
	}
	return doc.Suites[0]
}

func TestJUnitHasOneCasePerGateAndFailsABreach(t *testing.T) {
	m := passing()
	m[MetricCLINowP95] = 101
	delete(m, MetricSSEFailed)
	path := filepath.Join(t.TempDir(), "junit-perf.xml")
	if err := NewReport(DefaultConfig(), m, t0, t0.Add(90*time.Second)).WriteJUnit(path); err != nil {
		t.Fatal(err)
	}
	suite := readSuite(t, path)
	if suite.Name != "perf" || suite.Tests != len(Gates) || suite.Failures != 2 || suite.Time != "90.000" {
		t.Fatalf("suite = %+v", suite)
	}
	failed := map[string]string{}
	for _, c := range suite.Cases {
		if c.Failure != nil {
			failed[c.Name] = c.Failure.Message
		}
	}
	if len(failed) != 2 || failed["vh now p95"] == "" || failed["SSE clients that did not receive the published event"] == "" {
		t.Fatalf("failed cases = %v", failed)
	}
}

func TestJUnitOfAPassingReducedRunHasNoFailureAndNamesTheSmoke(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SSEClients = 5
	path := filepath.Join(t.TempDir(), "junit-perf.xml")
	if err := NewReport(cfg, passing(), t0, t0).WriteJUnit(path); err != nil {
		t.Fatal(err)
	}
	suite := readSuite(t, path)
	if suite.Failures != 0 || suite.Name != "perf (reduced smoke run)" {
		t.Fatalf("suite = %+v", suite)
	}
}

func TestWritersReportAnUnwritablePath(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewReport(DefaultConfig(), passing(), t0, t0)
	if err := r.WriteJSON(filepath.Join(blocker, "perf.json")); err == nil {
		t.Error("WriteJSON under a file: want an error")
	}
	if err := r.WriteJUnit(filepath.Join(blocker, "junit.xml")); err == nil {
		t.Error("WriteJUnit under a file: want an error")
	}
}

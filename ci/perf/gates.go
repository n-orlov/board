package perf

import (
	"fmt"
	"math"
	"strconv"
)

// Gate is one limit of SPEC 14. Several gates belong to one R14 target, named by
// Target. A measurement is a number under Metric; the gate holds when it is at
// most Max.
type Gate struct {
	Target string
	Name   string
	Metric string
	Unit   string
	Max    float64
}

// The R14 targets, in the order the report lists them.
const (
	TargetIdleRSS   = "idle-rss"
	TargetColdStart = "cold-start"
	TargetReadP99   = "read-p99"
	TargetRenewP99  = "lease-renew-p99"
	TargetLoad      = "sustained-load"
	TargetSSE       = "sse-clients"
	TargetCLINow    = "cli-now-p95"
	TargetCLIStart  = "cli-version-p95"
)

// Metric names the harness records.
const (
	MetricIdleRSSBytes     = "idle_rss_bytes"
	MetricColdEmptyMillis  = "cold_start_empty_ms"
	MetricColdSeededMillis = "cold_start_seeded_ms"
	MetricNowP99Millis     = "now_p99_ms"
	MetricAttentionP99     = "attention_p99_ms"
	MetricRenewP99Millis   = "lease_renew_p99_ms"
	MetricLoadErrors       = "load_errors"
	MetricSSEFailed        = "sse_failed_clients"
	MetricSSEMaxMillis     = "sse_max_ms"
	MetricCLINowP95        = "vh_now_p95_ms"
	MetricCLIVersionP95    = "vh_version_p95_ms"
)

// Gates are SPEC 14's numbers. They are judged on the CI agent and never
// loosened to fit a host.
var Gates = []Gate{
	{TargetIdleRSS, "idle RSS after the idle wait", MetricIdleRSSBytes, "bytes", 40 * 1000 * 1000},
	{TargetColdStart, "cold start to /healthz 200, empty store", MetricColdEmptyMillis, "ms", 1000},
	{TargetColdStart, "cold start to /healthz 200, seeded store", MetricColdSeededMillis, "ms", 3000},
	{TargetReadP99, "GET /api/v1/now p99", MetricNowP99Millis, "ms", 20},
	{TargetReadP99, "GET /api/v1/attention p99", MetricAttentionP99, "ms", 20},
	{TargetRenewP99, "lease renew p99", MetricRenewP99Millis, "ms", 10},
	{TargetLoad, "errors under the sustained mixed load", MetricLoadErrors, "requests", 0},
	{TargetSSE, "SSE clients that did not receive the published event", MetricSSEFailed, "clients", 0},
	{TargetSSE, "slowest SSE client's wait for the published event", MetricSSEMaxMillis, "ms", 5000},
	{TargetCLINow, "vh now p95", MetricCLINowP95, "ms", 100},
	{TargetCLIStart, "vh version p95", MetricCLIVersionP95, "ms", 20},
}

// Check is a gate judged against a measurement.
type Check struct {
	Gate
	Measured bool
	Value    float64
	Pass     bool
	Detail   string
}

// Evaluate judges every gate against metrics. A gate whose metric was not
// measured does not pass: a harness that skipped a measurement fails the run
// instead of certifying it.
func Evaluate(metrics map[string]float64) []Check {
	checks := make([]Check, 0, len(Gates))
	for _, g := range Gates {
		checks = append(checks, judge(g, metrics))
	}
	return checks
}

func judge(g Gate, metrics map[string]float64) Check {
	v, ok := metrics[g.Metric]
	switch {
	case !ok:
		return Check{Gate: g, Detail: fmt.Sprintf("%s was not measured", g.Metric)}
	case v > g.Max:
		return Check{Gate: g, Measured: true, Value: v, Detail: fmt.Sprintf("%s %s is above the limit of %s", fmtNumber(v), g.Unit, fmtNumber(g.Max))}
	default:
		return Check{Gate: g, Measured: true, Value: v, Pass: true, Detail: fmt.Sprintf("%s %s, limit %s", fmtNumber(v), g.Unit, fmtNumber(g.Max))}
	}
}

func fmtNumber(v float64) string { return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64) }

// Failures returns the checks that did not pass.
func Failures(checks []Check) []Check {
	var out []Check
	for _, c := range checks {
		if !c.Pass {
			out = append(out, c)
		}
	}
	return out
}

// Package perf is the performance harness of SPEC 14 (R14): it seeds a SQLite
// store, starts the real vh binary on it and measures the gates below.
//
// This part of the package is plain Go and always compiled: the percentile
// maths, the gate table that judges a measurement, the report writers (perf.json
// and JUnit), the run configuration and the seeder, each with its own unit
// tests. The measuring itself (harness_test.go) is behind the perf build tag,
// because it needs a minute of wall clock and a built binary:
//
//	ci/perf.sh [dir]    # go test -tags perf ./ci/perf/, results in dir (default ci-results)
//
// The numbers in the gate table are SPEC 14's and are judged on the CI agent
// only. A run on a reduced seed (VH_PERF_EVENTS and the other VH_PERF_ settings
// of Config) is a smoke check of the harness itself: its report says so, and its
// numbers are not evidence of anything.
package perf

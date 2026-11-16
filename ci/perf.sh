#!/bin/sh
# ci/perf.sh [dir] -- the performance harness of SPEC 14 (R14): seeds a SQLite
# store with 1,000,000 events, 50,000 notes and 200 live leases, starts the real
# vh binary on it and measures the eight gates (idle RSS, cold start, p99 of
# /api/v1/now and /api/v1/attention, lease renew p99, 200 req/s for 60 s, 500
# SSE clients, `vh now` and `vh version` p95). It writes perf.json and
# junit-perf.xml into dir (default ci-results) and exits non-zero when a gate is
# breached or was not measured. It takes about ten minutes and runs where the Go
# toolchain is, so on the agent and in a smoke check:
#
#   ci/run.sh ci/perf.sh [dir]
#   ci/run.sh env VH_PERF_EVENTS=30000 VH_PERF_NOTES=1500 VH_PERF_IDLE_SECONDS=2 \
#       VH_PERF_REQUESTS=300 VH_PERF_LOAD_SECONDS=5 VH_PERF_SSE_CLIENTS=50 \
#       VH_PERF_CLI_RUNS=10 ci/perf.sh /tmp/perf-smoke
#
# The VH_PERF_ settings (ci/perf's Config) size a smoke run of the harness itself;
# its perf.json says "reduced": true and its numbers are not evidence.
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

dir=${1:-ci-results}
mkdir -p "$dir"
VH_PERF_OUT=$(cd "$dir" && pwd)
export VH_PERF_OUT
rm -f "$VH_PERF_OUT/perf.json" "$VH_PERF_OUT/junit-perf.xml"

go test -tags perf -count=1 -timeout 25m -v -run '^TestPerf$' ./ci/perf/

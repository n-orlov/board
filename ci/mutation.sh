#!/bin/sh
# ci/mutation.sh -- the Go mutation gate's entry point (SPEC 15.4, the
# Jenkins Mutation stage's command). Runs gremlins over every package listed
# in ci/quality.json's mutation_go.packages (the Phase 0/1 domain and store
# plus the Phase 2 application logic; internal/server and internal/cli are
# narrowed to the files in mutation_go.scopes), writes one JSON report per
# package, then runs
# ci/mutationgate, which fails below mutation_go.efficacy or when a package
# generated no mutant. It then runs Stryker over web/src/lib and fails below
# stryker.break (whole run) or stryker.module_break (any one module). The
# thresholds live in ci/quality.json only.
#
#   ci/run.sh ci/mutation.sh                  # reports in ci-results/mutation
#   ci/run.sh ci/mutation.sh /tmp/m [config]  # a test's own outdir and config
#   ci/run.sh ci/mutation.sh ci-results/mutation ci/quality.json logic
#                                             # one lane of mutation_go.lanes
#
# A lane is a group of packages the Jenkinsfile runs as its own stage (each
# under its own 30-minute timeout, none raised): one stage cannot hold every
# package's mutants. The lane "all" (the default) is every package plus
# Stryker. Stryker runs in the lane "base" and in "all", and only there.
# Each lane judges and replaces only its own packages' reports, so the lanes
# leave the reports of all of them in the one directory ci/quality.sh reads.
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

outdir=${1:-ci-results/mutation}
config=${2:-ci/quality.json}
lane=${3:-all}

# Same scrub as ci/suite.sh and ci/quality.sh: a stray BOARD_* variable must
# never reach the tests gremlins runs.
for board_var in $(env | sed -n 's/^\(BOARD_[A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' | sort -u); do
    unset "$board_var"
done

# gremlins bounds each mutant's test run by a multiple of the covered run's
# time. With a warm build cache that time is a fraction of a second, less than
# the rebuild a mutant needs, so at gremlins' default every mutant of a
# package like internal/store timed out and none was judged. 60 is the
# multiple that judges every mutant; a mutant that really loops still ends.
timeout_coefficient=60

# gremlins takes that covered run's time from `go test -cover`, and go's test
# cache can answer that run in a few milliseconds whatever the suite really
# costs: a package whose tests take 30 s then gets a bound of seconds and every
# mutant of it times out unjudged. -count=1 makes the covered run a real run,
# so the coefficient multiplies the suite's real duration. The coefficient
# itself is unchanged.
GOFLAGS="${GOFLAGS:+$GOFLAGS }-count=1"
export GOFLAGS

mkdir -p "$outdir"

packages=$(go run ./ci/mutationgate -config "$config" -list -lane "$lane")
for pkg in $packages; do
    rm -f "${outdir:?}/$(printf '%s' "$pkg" | tr '/' '-').json"
done
for pkg in $packages; do
    name=$(printf '%s' "$pkg" | tr '/' '-')
    # A package with a scope in quality.json is narrowed to its listed files:
    # every other file of it is excluded (one -E per file, from mutationgate).
    set --
    for pattern in $(go run ./ci/mutationgate -config "$config" -excludes "$pkg"); do
        set -- "$@" -E "$pattern"
    done
    echo "ci/mutation.sh: gremlins unleash ./$pkg ($# exclude flags)"
    gremlins unleash "./$pkg" --timeout-coefficient "$timeout_coefficient" "$@" -o "$outdir/$name.json"
done

status=0
go run ./ci/mutationgate -config "$config" -dir "$outdir" -lane "$lane" || status=1

# The frontend: Stryker over web/src/lib (web/stryker.config.mjs), then the
# per-module break from ci/quality.json (web/scripts/mutation/mutation.ts).
# Its JSON report is copied to "$outdir/stryker/mutation.json", where
# ci/quality.sh's stryker row reads it.
if [ -f web/stryker.config.mjs ] && { [ "$lane" = all ] || [ "$lane" = base ]; }; then
    echo "ci/mutation.sh: stryker over web/src/lib"
    mkdir -p "$outdir/stryker"
    rm -f "$outdir/stryker/mutation.json"
    if [ ! -x web/node_modules/.bin/stryker ]; then
        (cd web && npm ci --no-audit --no-fund) || status=1
    fi
    (cd web && rm -f reports/mutation/mutation.json && npm run mutation) || status=1
    [ -f web/reports/mutation/mutation.json ] && cp web/reports/mutation/mutation.json "$outdir/stryker/mutation.json"
fi
exit "$status"

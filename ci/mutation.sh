#!/bin/sh
# ci/mutation.sh -- the Go mutation gate's entry point (SPEC 15.4, the
# Jenkins Mutation stage's command). Runs gremlins over every package listed
# in ci/quality.json's mutation_go.packages (internal/domain and
# internal/store), writes one JSON report per package, then runs
# ci/mutationgate, which fails below mutation_go.efficacy or when a package
# generated no mutant. It then runs Stryker over web/src/lib and fails below
# stryker.break (whole run) or stryker.module_break (any one module). The
# thresholds live in ci/quality.json only.
#
#   ci/run.sh ci/mutation.sh                  # reports in ci-results/mutation
#   ci/run.sh ci/mutation.sh /tmp/m [config]  # a test's own outdir and config
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

outdir=${1:-ci-results/mutation}
config=${2:-ci/quality.json}

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

mkdir -p "$outdir"
rm -f "${outdir:?}"/*.json

packages=$(go run ./ci/mutationgate -config "$config" -list)
for pkg in $packages; do
    name=$(printf '%s' "$pkg" | tr '/' '-')
    echo "ci/mutation.sh: gremlins unleash ./$pkg"
    gremlins unleash "./$pkg" --timeout-coefficient "$timeout_coefficient" -o "$outdir/$name.json"
done

status=0
go run ./ci/mutationgate -config "$config" -dir "$outdir" || status=1

# The frontend: Stryker over web/src/lib (web/stryker.config.mjs), then the
# per-module break from ci/quality.json (web/scripts/mutation/mutation.ts).
# Its JSON report is copied to "$outdir/stryker/mutation.json", where
# ci/quality.sh's stryker row reads it.
if [ -f web/stryker.config.mjs ]; then
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

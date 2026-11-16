#!/bin/sh
# ci/mutation.sh -- the Go mutation gate's entry point (SPEC 15.4, the
# Jenkins Mutation stage's command). Runs gremlins over every package listed
# in ci/quality.json's mutation_go.packages (the Phase 0/1 domain and store
# plus the Phase 2 application logic; internal/server, internal/cli and
# internal/tasks are narrowed to the files in mutation_go.scopes, and
# `ci/mutationgate -unmeasured` lists any application file changed since
# mutation_go.phase_base that no unit measures), writes one JSON report per
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
# A lane is a group of packages that can be run by hand on their own. The
# Jenkinsfile's Mutation unit runs the lane "all" (full mode, or diff mode
# restricted to the changed lines). The lane "all" (the default) is every
# package plus Stryker. Stryker runs in the lane "base" and in "all", and only there.
# A package split into parts (mutation_go.parts) is run once per part, a part
# written "internal/server#ops", with its own report and its own judgement.
# Each lane judges and replaces only its own packages' reports, so the lanes
# leave the reports of all of them in the one directory ci/quality.sh reads.
#
# Diff mode (BOARD_MUTATION_DIFF=<ref>, the Jenkinsfile's branch and PR
# builds): only the units with a measured file changed since the merge base
# with <ref> run, gremlins mutates only the changed lines (gremlins --diff,
# which compares the working tree with `git diff --merge-base <ref>`), and
# ci/mutationgate -diff judges them: a unit with no mutant passes, one with
# mutants is held to the same efficacy floor. Stryker runs only when
# web/src/lib changed since that merge base (then over all of it). The
# unchanged units are never handed to gremlins: with an empty diff gremlins
# mutates everything.
#
# BOARD_MUTATION_WORKERS: gremlins' worker count (default: one per CPU).
# BOARD_MUTATION_TEST_CPU: GOMAXPROCS for gremlins and every go build and test
# it runs, so workers x test CPUs stays near the core count. Not gremlins'
# own --test-cpu: v0.6.0 passes it as the single argument "-cpu N", which go
# test rejects, so every mutant would count as KILLED.
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

outdir=${1:-ci-results/mutation}
config=${2:-ci/quality.json}
lane=${3:-all}

# Read before the scrub below (see the header).
workers=${BOARD_MUTATION_WORKERS:-}
test_cpu=${BOARD_MUTATION_TEST_CPU:-}
diff_ref=${BOARD_MUTATION_DIFF:-}
# The attempt's run id (ci/run.sh forwards RALPHD_RUN_ID as BOARD_CI_RUN_ID): the
# tests gremlins runs label their Postgres testcontainers with it, so the
# Jenkinsfile's per-attempt cleanup reaps the ones a killed mutant run leaves.
ci_run_id=${BOARD_CI_RUN_ID:-}

# Same scrub as ci/suite.sh and ci/quality.sh: a stray BOARD_* variable must
# never reach the tests gremlins runs.
# The one exception is BOARD_CI_RUN_ID, handed to gremlins explicitly below.
for board_var in $(env | sed -n -e 's/^\(BOARD_[A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' -e 's/^\(VH_[A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' | sort -u); do
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
# itself is unchanged. -timeout=30m: go test's default 10 min kills the covered
# run of internal/server on an arm64 phone (about 5 min on amd64, over 10 there).
GOFLAGS="${GOFLAGS:+$GOFLAGS }-count=1 -timeout=30m"
export GOFLAGS

mkdir -p "$outdir"
rm -f "$outdir/unit-verdict.txt" "$outdir/mutationgate.txt"

# Every report of the lane goes, so a diff-mode run never leaves an older
# full run's report of an unchanged unit behind.
for pkg in $(go run ./ci/mutationgate -config "$config" -list -lane "$lane"); do
    rm -f "${outdir:?}/$(printf '%s' "$pkg" | tr '/#' '--').json"
done
if [ -n "$diff_ref" ]; then
    echo "ci/mutation.sh: diff mode against the merge base with $diff_ref ($(git merge-base "$diff_ref" HEAD))"
    # gremlins v0.6.0 --diff runs `git diff --merge-base <ref>` in the module
    # root, which prints paths from the root, and matches them against paths
    # relative to the package it mutates ("list.go", not
    # "internal/notes/list.go"): nothing ever matches, every mutant is
    # SKIPPED. A git shim first in gremlins' PATH adds --relative=<package
    # dir> to that one command, so the paths are the package's own. Every
    # other git command passes through untouched.
    real_git=$(command -v git)
    shim=$(mktemp -d)
    trap 'rm -rf "$shim"' EXIT
    cat >"$shim/git" <<EOF
#!/bin/sh
if [ "\$1" = diff ] && [ -n "\${MUTATION_DIFF_RELATIVE:-}" ]; then
    shift
    exec "$real_git" diff --relative="\$MUTATION_DIFF_RELATIVE" "\$@"
fi
exec "$real_git" "\$@"
EOF
    chmod +x "$shim/git"
fi
packages=$(go run ./ci/mutationgate -config "$config" -list -lane "$lane" ${diff_ref:+-diff "$diff_ref"})
if [ -n "$diff_ref" ] && [ -z "$packages" ]; then
    echo "ci/mutation.sh: no measured Go file of lane $lane changed: no gremlins run"
fi
for pkg in $packages; do
    name=$(printf '%s' "$pkg" | tr '/#' '--')
    dir=${pkg%%#*}
    # A package with a scope in quality.json is narrowed to its listed files:
    # every other file of it is excluded (one -E per file, from mutationgate).
    set --
    for pattern in $(go run ./ci/mutationgate -config "$config" -excludes "$pkg"); do
        set -- "$@" -E "$pattern"
    done
    echo "ci/mutation.sh: gremlins unleash ./$pkg ($# exclude flags)"
    # nice: a full run takes every core for hours; other jobs on the host win.
    # The GOMAXPROCS cap applies only here, never to the gate's own go run.
    env ${ci_run_id:+BOARD_CI_RUN_ID=$ci_run_id} ${test_cpu:+GOMAXPROCS=$test_cpu} ${diff_ref:+PATH="$shim:$PATH" MUTATION_DIFF_RELATIVE="$dir"} \
        nice -n 10 gremlins unleash "./$dir" \
        --timeout-coefficient "$timeout_coefficient" ${workers:+--workers "$workers"} \
        ${diff_ref:+--diff "$diff_ref"} "$@" -o "$outdir/$name.json"
done

status=0
# The unit's own verdicts go to "$outdir/unit-verdict.txt" (one "<gate>:
# passed|failed|not run" line per gate, the names of ci/quality.json's gates),
# which the build's CI summary page (ci/jenkins-report.sh) shows for the
# mutation-go and stryker rows that a Core quality run defers. It only records
# what this script already judged: the exit status is unchanged.
go_verdict=passed
go run ./ci/mutationgate -config "$config" -dir "$outdir" -lane "$lane" ${diff_ref:+-diff "$diff_ref"} \
    > "$outdir/mutationgate.txt" 2>&1 || go_verdict=failed
cat "$outdir/mutationgate.txt"
[ "$go_verdict" = passed ] || status=1

# The frontend: Stryker over web/src/lib (web/stryker.config.mjs), then the
# per-module break from ci/quality.json (web/scripts/mutation/mutation.ts).
# Its JSON report is copied to "$outdir/stryker/mutation.json", where
# ci/quality.sh's stryker row reads it.
stryker=no
stryker_verdict="not run"
if [ -f web/stryker.config.mjs ] && { [ "$lane" = all ] || [ "$lane" = base ]; }; then
    stryker=yes
    if [ -n "$diff_ref" ] && git diff --quiet --merge-base "$diff_ref" -- web/src/lib; then
        echo "ci/mutation.sh: web/src/lib unchanged since the merge base with $diff_ref: no stryker run"
        stryker=no
    fi
fi
if [ "$stryker" = yes ]; then
    echo "ci/mutation.sh: stryker over web/src/lib"
    stryker_verdict=passed
    mkdir -p "$outdir/stryker"
    rm -f "$outdir/stryker/mutation.json"
    if [ ! -x web/node_modules/.bin/stryker ]; then
        (cd web && npm ci --no-audit --no-fund) || { status=1; stryker_verdict=failed; }
    fi
    (cd web && rm -f reports/mutation/mutation.json && npm run mutation) || { status=1; stryker_verdict=failed; }
    [ -f web/reports/mutation/mutation.json ] && cp web/reports/mutation/mutation.json "$outdir/stryker/mutation.json"
fi
printf 'mutation-go: %s\nstryker: %s\n' "$go_verdict" "$stryker_verdict" > "$outdir/unit-verdict.txt"
exit "$status"

#!/bin/sh
# ci/mutation-merge.sh -- full-mode mutation's last step (SPEC 15.4): the
# Jenkinsfile runs ci/mutation.sh as three parallel units, each over its own
# lanes and into its own outdir, and this script judges their union as one
# gate, the same as a single `ci/mutation.sh` run over the lane "all".
#
#   ci/run.sh ci/mutation-merge.sh ci-results/mutation ci/quality.json \
#       ci-results/mutation-units/a ci-results/mutation-units/b ...
#
# It copies every unit's gremlins reports (<pkg>.json) into <outdir>, where
# ci/quality.sh and the CI summary read them; a report found in two unit dirs
# fails the merge (each package is measured exactly once). Then it runs
# ci/mutationgate over every package (lane "all"): a package no unit reported,
# because its unit died or its lane was left out, fails as "FAILED, report".
# Stryker's report (stryker/mutation.json) and verdict come from the one unit
# that ran it; full mode always runs it, so no such unit fails the stryker row.
# Incremental mode (ci/mutation-plan.sh) passes one more dir: the reference
# build's reports of the units that did not run, and its Stryker verdict when
# Stryker did not run either; the merge treats it as one more unit.
# Writes <outdir>/mutationgate.txt and <outdir>/unit-verdict.txt, the same
# files ci/mutation.sh writes. Exit 1 when either gate failed.
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

if [ $# -lt 3 ]; then
    echo "usage: ci/mutation-merge.sh <outdir> <config> <unit outdir>..." >&2
    exit 2
fi
outdir=$1
config=$2
shift 2

rm -rf "$outdir"
mkdir -p "$outdir"
status=0
duplicate=no
stryker_verdict=""
for src in "$@"; do
    if [ ! -d "$src" ]; then
        echo "ci/mutation-merge.sh: $src is missing (its unit left no results)"
        continue
    fi
    for report in "$src"/*.json; do
        [ -f "$report" ] || continue
        name=$(basename "$report")
        if [ -e "$outdir/$name" ]; then
            echo "ci/mutation-merge.sh: $name is reported by two units; every package belongs to exactly one"
            duplicate=yes
            continue
        fi
        cp "$report" "$outdir/$name"
    done
    v=$(sed -n 's/^stryker: //p' "$src/unit-verdict.txt" 2>/dev/null || true)
    if [ -n "$v" ] && [ "$v" != "not run" ]; then
        if [ -n "$stryker_verdict" ]; then
            echo "ci/mutation-merge.sh: Stryker ran in two units"
            v=failed
        fi
        [ "$stryker_verdict" = failed ] || stryker_verdict=$v
        if [ -f "$src/stryker/mutation.json" ]; then
            mkdir -p "$outdir/stryker"
            cp "$src/stryker/mutation.json" "$outdir/stryker/mutation.json"
        fi
    fi
done
if [ -z "$stryker_verdict" ]; then
    echo "ci/mutation-merge.sh: no unit ran Stryker"
    stryker_verdict=failed
fi
[ "$stryker_verdict" = passed ] || status=1

go_verdict=passed
go run ./ci/mutationgate -config "$config" -dir "$outdir" -lane all \
    > "$outdir/mutationgate.txt" 2>&1 || go_verdict=failed
cat "$outdir/mutationgate.txt"
[ "$duplicate" = no ] || go_verdict=failed
[ "$go_verdict" = passed ] || status=1
printf 'mutation-go: %s\nstryker: %s\n' "$go_verdict" "$stryker_verdict" > "$outdir/unit-verdict.txt"
exit "$status"

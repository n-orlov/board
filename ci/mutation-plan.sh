#!/bin/sh
# ci/mutation-plan.sh -- incremental mutation's first step (SPEC 15.3): the
# Jenkinsfile's nightly reuses the last full run's gremlins reports (its
# reference build, copied into <reference dir> as archived: ci-results/...)
# for every unit no change since could reach, and runs gremlins over the
# rest in the full-mode units; ci/mutation-merge.sh then judges the units'
# reports and the reused ones together, every unit exactly once.
#
#   ci/run.sh ci/mutation-plan.sh <reference dir> <reused outdir> <config> \
#       <plan file> <group>=<lanes>...
#
# A group is one full-mode unit (the Jenkinsfile's FULL_MUTATION: its outdir
# name and its lane list). The plan file gets key=value lines:
#
#   mode=full, reason=<why>         every unit runs: Sunday's weekly full run
#                                   (BOARD_MUTATION_WEEKLY_FULL=yes, the
#                                   nightly timer), no usable reference, or a
#                                   shared input changed (ci/mutationgate
#                                   -stale says which)
#   mode=incremental                otherwise, with
#   ref=<sha>                       the reference build's commit
#   run=<n>, reused=<m>             units run and reused (n + m = every unit)
#   stryker=run|reuse               Stryker runs when web/ changed since ref
#                                   (or the reference's verdict was not passed)
#   group.<group>=<lane list>       what the group runs: its stale units, each
#                                   named on its own; empty when nothing (the
#                                   Jenkinsfile then skips it). When Stryker
#                                   runs, the group holding lane base runs
#                                   "base" too, since ci/mutation.sh runs
#                                   Stryker only for a list holding it (base's
#                                   two packages take about 2 minutes).
#
# <reused outdir> gets the reference's reports of every unit that does not
# run, and, when Stryker is reused, the reference's Stryker verdict (in
# unit-verdict.txt) and report. A unit the reference left no report for runs.
# Exit 0 with a plan; anything else means no plan (the Jenkinsfile then runs
# in full).
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

if [ $# -lt 5 ]; then
    echo "usage: ci/mutation-plan.sh <reference dir> <reused outdir> <config> <plan file> <group>=<lanes>..." >&2
    exit 2
fi
raw=$1
reused_dir=$2
config=$3
plan=$4
shift 4
weekly=${BOARD_MUTATION_WEEKLY_FULL:-}
reports="$raw/ci-results/mutation"

mkdir -p "$(dirname "$plan")"
full() {
    printf 'mode=full\nreason=%s\n' "$1" > "$plan"
    echo "ci/mutation-plan.sh: full run: $1"
    exit 0
}
report_of() {
    printf '%s/%s.json' "$1" "$(printf '%s' "$2" | tr '/#' '--')"
}
listed() { # listed <unit> <unit>...: is the first among the rest?
    u=$1
    shift
    for x in "$@"; do
        [ "$x" != "$u" ] || return 0
    done
    return 1
}
gate() {
    go run ./ci/mutationgate -config "$config" "$@"
}

if [ "$weekly" = yes ] && [ "$(date +%u)" = 7 ]; then
    full "Sunday's weekly full run"
fi
[ -f "$reports/unit-verdict.txt" ] || full "the reference build left no mutation results"
ref=$(cat "$raw/ci-results/commit.txt" 2>/dev/null ||
    sed -n 's/.*"commit": *"\([0-9a-f]*\)".*/\1/p' "$raw/ci-results/release-candidate.json" 2>/dev/null || true)
[ -n "$ref" ] || full "the reference build left no commit"
git cat-file -e "$ref^{commit}" 2>/dev/null || full "the reference commit $ref is not in this checkout"

errs=$(mktemp)
trap 'rm -f "$errs"' EXIT
rc=0
stale=$(gate -stale "$ref" 2>"$errs") || rc=$?
if [ "$rc" -ne 0 ]; then
    reason=$(sed -n 's/^mutationgate: full: //p' "$errs")
    [ -z "$reason" ] || full "$reason"
    cat "$errs" >&2
    exit 1
fi
all=$(gate -list)
for u in $all; do
    # shellcheck disable=SC2086 # word lists of units
    listed "$u" $stale || [ -f "$(report_of "$reports" "$u")" ] || stale="$stale $u"
done

stryker=reuse
git diff --quiet "$ref" -- web || stryker=run
[ "$(sed -n 's/^stryker: //p' "$reports/unit-verdict.txt")" = passed ] || stryker=run

groups=""
run_units=""
stryker_group=""
for g in "$@"; do
    name=${g%%=*}
    lanes=${g#*=}
    selected=""
    for u in $(gate -list -lane "$lanes"); do
        # shellcheck disable=SC2086
        if listed "$u" $stale; then
            selected="$selected,$u"
        fi
    done
    selected=${selected#,}
    case ",$lanes," in
    *,base,*)
        stryker_group=$name
        [ "$stryker" = reuse ] || selected="base${selected:+,$selected}"
        ;;
    esac
    [ -z "$selected" ] || run_units="$run_units $(gate -list -lane "$selected")"
    groups="${groups}group.$name=$selected
"
done
if [ -z "$stryker_group" ]; then
    echo "ci/mutation-plan.sh: no group holds the lane base, so none runs Stryker" >&2
    exit 1
fi

rm -rf "$reused_dir"
mkdir -p "$reused_dir"
cp "$reports"/*.json "$reused_dir"/
run_count=0
for u in $all; do
    # shellcheck disable=SC2086
    if listed "$u" $run_units; then
        rm -f "$(report_of "$reused_dir" "$u")"
        run_count=$((run_count + 1))
    fi
done
# shellcheck disable=SC2086
total=$(printf '%s\n' $all | grep -c .)
if [ "$stryker" = reuse ]; then
    printf 'mutation-go: not run\nstryker: passed\n' > "$reused_dir/unit-verdict.txt"
    if [ -f "$reports/stryker/mutation.json" ]; then
        mkdir -p "$reused_dir/stryker"
        cp "$reports/stryker/mutation.json" "$reused_dir/stryker/mutation.json"
    fi
fi

printf 'mode=incremental\nref=%s\nrun=%s\nreused=%s\nstryker=%s\n%s' \
    "$ref" "$run_count" "$((total - run_count))" "$stryker" "$groups" > "$plan"
echo "ci/mutation-plan.sh: incremental against $ref: $run_count of $total units run, Stryker: $stryker"
cat "$plan"

#!/bin/sh
# ci/openapi-drift.sh -- fail when the checked-in generated code differs from
# what ci/openapi-gen.sh produces from the OpenAPI document (oapi-codegen for
# Go, openapi-typescript for TypeScript). Regenerate with ci/openapi-gen.sh.
#
# The three paths default to the checked-in ones and can be overridden through
# BOARD_OPENAPI_SPEC, BOARD_OPENAPI_GO and BOARD_OPENAPI_TS (ci/lintcheck uses
# that to plant a stale file). A missing generated file is drift, never a pass.
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

spec=${BOARD_OPENAPI_SPEC:-api/openapi.yaml}
go_file=${BOARD_OPENAPI_GO:-internal/server/openapi.gen.go}
ts_file=${BOARD_OPENAPI_TS:-web/src/lib/api/schema.d.ts}

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT

sh ci/openapi-gen.sh "$spec" "$scratch/openapi.gen.go" "$scratch/schema.d.ts" >/dev/null

drift=0
for pair in "$go_file:$scratch/openapi.gen.go" "$ts_file:$scratch/schema.d.ts"; do
    committed=${pair%%:*}
    fresh=${pair#*:}
    if [ ! -f "$committed" ]; then
        echo "ci/openapi-drift.sh: $committed is missing" >&2
        drift=1
    elif ! diff -u "$committed" "$fresh" >&2; then
        echo "ci/openapi-drift.sh: $committed is stale -- run ci/openapi-gen.sh and commit the result" >&2
        drift=1
    fi
done

if [ "$drift" -ne 0 ]; then
    exit 1
fi
echo "ci/openapi-drift.sh: generated Go and TypeScript match $spec"

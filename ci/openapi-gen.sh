#!/bin/sh
# ci/openapi-gen.sh -- generate the Go and TypeScript code from the OpenAPI
# document. This is the ONE generator invocation: a developer runs it to
# refresh the checked-in code, and ci/openapi-drift.sh runs it into a scratch
# directory to prove the checked-in code is current.
#
#   ci/openapi-gen.sh [SPEC [GO_OUT [TS_OUT]]]
#
# Defaults: api/openapi.yaml -> internal/server/openapi.gen.go (oapi-codegen strict-server,
# pinned by go.mod's tool directive) and web/src/lib/api/schema.d.ts
# (openapi-typescript, pinned by web/package-lock.json). Relative paths are
# relative to the repository root. Run it under ci/run.sh, or anywhere Go and
# `npm ci` in web/ are available.
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

spec=${1:-api/openapi.yaml}
go_out=${2:-internal/server/openapi.gen.go}
ts_out=${3:-web/src/lib/api/schema.d.ts}

case "$spec" in /*) ;; *) spec=$repo_root/$spec ;; esac
case "$go_out" in /*) ;; *) go_out=$repo_root/$go_out ;; esac
case "$ts_out" in /*) ;; *) ts_out=$repo_root/$ts_out ;; esac

# The board-ci image sets GOTOOLCHAIN=local; keep that true outside it too.
GOTOOLCHAIN=${GOTOOLCHAIN:-local}
export GOTOOLCHAIN

echo "ci/openapi-gen.sh: oapi-codegen -> $go_out"
go tool oapi-codegen -package server -generate std-http,strict-server,models -o "$go_out" "$spec"

# The generated file is linted like any other (.golangci.yml disables the
# generated-code skip), so two kinds of line the generator emits are rewritten here to
# satisfy errcheck, staticcheck and gosec. Each rewrite must find its line, or the
# generator changed and this script needs to follow.
patch_go() {
    pattern=$1
    replacement=$2
    if ! grep -q "$pattern" "$go_out"; then
        echo "ci/openapi-gen.sh: oapi-codegen no longer emits '$pattern'; update the post-processing" >&2
        exit 1
    fi
    sed -i "s|$pattern|$replacement|" "$go_out"
}
patch_go 'defer closer.Close()' 'defer func() { _ = closer.Close() }()'
if ! grep -qF 'w.Write([]byte(fmt.Sprint(response)))' "$go_out"; then
    echo "ci/openapi-gen.sh: oapi-codegen no longer emits the text response Write; update the post-processing" >&2
    exit 1
fi
sed -i 's|w\.Write(\[\]byte(fmt\.Sprint(response)))$|fmt.Fprint(w, response) //nolint:gosec // G705: the body is a typed string the handler chose, not request input|' "$go_out"

# A query parameter that cannot be bound is answered by a six-line block that oapi-codegen repeats
# for every parameter, which makes an operation with a few filters exceed the CRAP ceiling no test
# can lower (its required-parameter branch is unreachable for an optional one). Each block is
# rewritten to one call of the hand-written (*ServerInterfaceWrapper).bindFailed in
# internal/server/params.go, which makes the same two answers.
if ! grep -q 'var requiredError \*runtime.RequiredParameterError' "$go_out"; then
    echo "ci/openapi-gen.sh: oapi-codegen no longer emits the required-parameter block; update the post-processing" >&2
    exit 1
fi
perl -0pi -e 's/\t\tvar requiredError \*runtime\.RequiredParameterError\n\t\tif errors\.As\(err, &requiredError\) \{\n\t\t\tsiw\.ErrorHandlerFunc\(w, r, &RequiredParamError\{ParamName: ("[^"]*")\}\)\n\t\t\} else \{\n\t\t\tsiw\.ErrorHandlerFunc\(w, r, &InvalidParamFormatError\{ParamName: "[^"]*", Err: err\}\)\n\t\t\}\n/\t\tsiw.bindFailed(w, r, $1, err)\n/g' "$go_out"
if grep -q 'var requiredError \*runtime.RequiredParameterError' "$go_out"; then
    echo "ci/openapi-gen.sh: the required-parameter block changed shape; update the post-processing" >&2
    exit 1
fi

# The rewrite can leave "errors" unused; drop the import then, and only then.
if ! grep -q 'errors\.' "$go_out"; then
    sed -i '/^\t"errors"$/d' "$go_out"
fi

echo "ci/openapi-gen.sh: openapi-typescript -> $ts_out"
(cd web && npx --no-install openapi-typescript "$spec" -o "$ts_out")

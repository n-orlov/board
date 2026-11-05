#!/bin/sh
# ci/lint.sh — fail-fast lint gate (R14, R145, R188): `gofmt -l` over every
# tracked Go file, then `go vet ./...`, then a `go mod tidy` drift check on
# go.mod/go.sum, then golangci-lint (ci/golangci.sh, the same invocation
# ci/quality.sh's golangci gate runs), then shellcheck over `git ls-files
# '*.sh'`, then the frontend (ESLint, svelte-check, `tsc --noEmit`, all from
# web/), then the OpenAPI document (Redocly lint of api/openapi.yaml) and
# generated-code drift (ci/openapi-drift.sh: oapi-codegen and
# openapi-typescript), in that order, stopping at the first failure so CI
# reports the earliest, cheapest signal rather than piling up unrelated ones.
# gofmt, vet and tidy live only here; golangci-lint's own formatters are not
# enabled, so nothing is checked twice. ci/lintcheck proves each checker fails
# on a planted violation.
#
# Assumes `go` (and its bundled `gofmt`) are already on PATH -- true both in
# the ci/run.sh sibling container (the Jenkinsfile's Lint stage, and local
# runs from a sandbox). Invoke it:
#
#   ci/lint.sh              # on a host/runner with Go installed directly
#   ci/run.sh ci/lint.sh     # from a sandbox without a local Go toolchain
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

echo "ci/lint.sh: gofmt -l"
fmt_files=$(git ls-files '*.go')
fmt_out=""
if [ -n "$fmt_files" ]; then
    fmt_out=$(git ls-files -z '*.go' | xargs -0 gofmt -l)
fi
if [ -n "$fmt_out" ]; then
    echo "ci/lint.sh: gofmt drift in:" >&2
    echo "$fmt_out" >&2
    exit 1
fi

echo "ci/lint.sh: go vet ./..."
go vet ./...

echo "ci/lint.sh: go mod tidy drift check"
tmp_mod=$(mktemp)
tmp_sum=$(mktemp)
cp go.mod "$tmp_mod"
cp go.sum "$tmp_sum"

drift=0
go mod tidy
if ! diff -u "$tmp_mod" go.mod || ! diff -u "$tmp_sum" go.sum; then
    drift=1
fi

# Restore the working tree exactly as it stood before `go mod tidy` ran,
# whether or not drift was found, so this script never leaves a modified
# go.mod/go.sum behind for the caller to notice or commit by accident.
cp "$tmp_mod" go.mod
cp "$tmp_sum" go.sum
rm -f "$tmp_mod" "$tmp_sum"

if [ "$drift" -ne 0 ]; then
    echo "ci/lint.sh: go mod tidy would change go.mod/go.sum -- run it locally and commit the diff" >&2
    exit 1
fi

echo "ci/lint.sh: golangci-lint"
sh ci/golangci.sh

echo "ci/lint.sh: shellcheck"
sh_files=$(git ls-files '*.sh')
if [ -z "$sh_files" ]; then
    echo "ci/lint.sh: no tracked shell scripts to check" >&2
    exit 1
fi
git ls-files -z '*.sh' | xargs -0 shellcheck

# The frontend checkers are web/'s own devDependencies; install them (from the
# lockfile, as ci/suite.sh does) when a fresh checkout has no node_modules yet.
if [ ! -d web/node_modules ]; then
    echo "ci/lint.sh: npm ci (web/node_modules missing)"
    (cd web && npm ci --no-audit --no-fund)
fi

echo "ci/lint.sh: ESLint"
(cd web && npm run --silent lint)

echo "ci/lint.sh: svelte-check"
(cd web && npm run --silent check:svelte)

echo "ci/lint.sh: tsc --noEmit"
(cd web && npm run --silent check)

echo "ci/lint.sh: OpenAPI lint"
(cd web && npm run --silent openapi:lint -- ../api/openapi.yaml)

echo "ci/lint.sh: OpenAPI generated-code drift"
sh ci/openapi-drift.sh

echo "ci/lint.sh: clean (gofmt, go vet, go mod tidy, golangci-lint, shellcheck, ESLint, svelte-check, tsc, OpenAPI lint and drift all pass)"

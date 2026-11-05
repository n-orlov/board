#!/bin/sh
# Start the real board binary for the Playwright specs: build the embedded UI
# when it is missing, build the binary, migrate a fresh SQLite store in a
# temporary directory, seed it (an API key the specs may mint nothing without),
# and serve on $E2E_PORT until Playwright stops this script.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
root=$(dirname "$here")
port=${E2E_PORT:?E2E_PORT is set by playwright.config.ts}

work=$(mktemp -d "${TMPDIR:-/tmp}/board-e2e.XXXXXX")
pid=
cleanup() {
    if [ -n "$pid" ]; then
        kill "$pid" 2>/dev/null || true
    fi
    rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 143' TERM INT

cd "$root"
if [ ! -f internal/webui/dist/index.html ]; then
    (cd web && npm ci --no-audit --no-fund && npm run build) >&2
fi
go build -o "$work/board" ./cmd/board

# Only the variables this store needs: nothing from the caller's BOARD_* leaks in.
for v in $(env | sed -n 's/^\(BOARD_[A-Za-z0-9_]*\)=.*/\1/p'); do unset "$v"; done
export BOARD_DB_URL="sqlite://$work/board.db"

"$work/board" admin migrate >&2
"$work/board" admin keys create e2e-seed >/dev/null

BOARD_LISTEN="127.0.0.1:$port" "$work/board" serve &
pid=$!
wait "$pid"

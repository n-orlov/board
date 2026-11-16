#!/bin/sh
# Start the real board binary for the Playwright specs: build the embedded UI
# when it is missing, build the binary and the mock OIDC provider into $E2E_WORK,
# migrate a fresh SQLite store there, and serve on $E2E_PORT, with browser login
# against the mock provider, until Playwright stops this script. The
# store holds no key: global-setup.ts mints one with `vh admin keys create`
# once this server is up, and global-teardown.ts removes $E2E_WORK.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
root=$(dirname "$here")
port=${E2E_PORT:?E2E_PORT is set by playwright.config.ts}

work=${E2E_WORK:?E2E_WORK is set by playwright.config.ts}
pid=
mockpid=
cleanup() {
    for p in "$pid" "$mockpid"; do
        if [ -n "$p" ]; then
            kill "$p" 2>/dev/null || true
        fi
    done
}
trap cleanup EXIT
trap 'exit 143' TERM INT

cd "$root"
if [ ! -f internal/webui/dist/index.html ]; then
    (cd web && npm ci --no-audit --no-fund && npm run build) >&2
fi
go build -o "$work/board" ./cmd/vh
go build -o "$work/fake-oidc" ./cmd/fake-oidc

# Only the variables this store needs: nothing from the caller's BOARD_* or VH_* leaks in.
for v in $(env | sed -n -e 's/^\(BOARD_[A-Za-z0-9_]*\)=.*/\1/p' -e 's/^\(VH_[A-Za-z0-9_]*\)=.*/\1/p'); do unset "$v"; done
export BOARD_DB_URL="sqlite://$work/board.db"

"$work/board" admin migrate >&2

# The mock identity provider signs in $E2E_EMAIL. Its client secret is made up for
# this run and handed to both processes through their environment, never a file.
E2E_EMAIL=${E2E_EMAIL:-operator@example.test}
secret=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
FAKE_OIDC_CLIENT_SECRET=$secret "$work/fake-oidc" -client-id board -email "$E2E_EMAIL" >"$work/fake-oidc.url" &
mockpid=$!
i=0
while [ ! -s "$work/fake-oidc.url" ]; do
    i=$((i + 1))
    if [ "$i" -gt 100 ]; then
        echo "serve.sh: the mock OIDC provider did not start" >&2
        exit 1
    fi
    sleep 0.1
done

BOARD_LISTEN="127.0.0.1:$port" \
    BOARD_PUBLIC_URL="http://127.0.0.1:$port" \
    BOARD_OIDC_ISSUER="$(cat "$work/fake-oidc.url")" \
    BOARD_OIDC_CLIENT_ID=board \
    BOARD_OIDC_CLIENT_SECRET="$secret" \
    BOARD_OIDC_ALLOWED_EMAILS="$E2E_EMAIL" \
    "$work/board" serve &
pid=$!
wait "$pid"

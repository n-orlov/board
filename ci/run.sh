#!/bin/sh
# Run a command in a throwaway sibling container that has the project toolchain
# (Go, Node 24, the Playwright browsers and the gate tools). Works both inside a
# ralphd job container and directly on the host.
#
#   ci/run.sh go test ./...
#   ci/run.sh sh -c 'cd web && npm run test'
#   BOARD_GODOG_PATHS=initiatives.feature ci/run.sh go test ./features/ -run TestFeatures
#
# Invariants that matter:
#   - the bind source must be a HOST path, because siblings run on the host daemon;
#     inside a ralphd job that path is $RALPHD_HOST_WORKSPACE, not /workspace
#   - the sibling runs as the calling uid/gid, so it never leaves root-owned files
#   - the Go module/build cache and the npm cache live in named volumes shared
#     across runs, so warm invocations skip dependency downloads
#   - a sibling starts with a fresh environment, so every BOARD_* variable the
#     caller set in front of ci/run.sh is forwarded explicitly (see below), and
#     nothing else is: without that, selectors like BOARD_GODOG_PATHS were
#     silently dropped and the whole feature suite ran instead of the named files
set -eu

workspace=${RALPHD_HOST_WORKSPACE:-$PWD}
image=${BOARD_CI_IMAGE:-board-ci:local}
volume=${BOARD_CI_CACHE_VOLUME:-board-go-cache}
npm_volume=${BOARD_CI_NPM_VOLUME:-board-npm-cache}

if ! docker image inspect "$image" >/dev/null 2>&1; then
    echo "error: image $image not found; build it with:" >&2
    echo "  docker build -t $image -f ci/Dockerfile ci" >&2
    exit 1
fi

# Deliberately shared across runs: the caches are the whole point, so they are
# named after the toolchain rather than a run id. Remove one to force a cold build.
if ! docker volume inspect "$volume" >/dev/null 2>&1; then
    docker volume create --label board.cache=go "$volume" >/dev/null
fi
if ! docker volume inspect "$npm_volume" >/dev/null 2>&1; then
    docker volume create --label board.cache=npm "$npm_volume" >/dev/null
fi

# The sibling command is assembled by appending the docker prefix *after* the
# caller's own argv and then rotating that argv to the end, so the forwarded `env`
# assignments can sit between the image name and the command being run.
user_argc=$#

set -- "$@" docker run --rm \
    --user "$(id -u):$(id -g)" \
    --workdir /w \
    --mount "type=bind,src=$workspace,dst=/w" \
    --mount "type=volume,src=$volume,dst=/go-cache" \
    --mount "type=volume,src=$npm_volume,dst=/npm-cache" \
    ${RALPHD_RUN_ID:+--label ralphd.run=$RALPHD_RUN_ID} \
    ${RALPHD_RUN_ID:+--label ralphd.role=sibling} \
    "$image"

# Forward every BOARD_* variable present in the caller's environment, as an `env`
# prefix on the command itself. This makes
#   BOARD_GODOG_PATHS=x ci/run.sh go test ./features/ -run TestFeatures
# behave exactly like the longhand form
#   ci/run.sh env BOARD_GODOG_PATHS=x go test ./features/ -run TestFeatures
# which keeps working unchanged. Nothing is added when no BOARD_* variable is set.
forwarded=$(env | sed -n 's/^\(BOARD_[A-Za-z0-9_]*\)=.*/\1/p' | sort -u)
if [ -n "$forwarded" ]; then
    set -- "$@" env
    for name in $forwarded; do
        eval "set -- \"\$@\" \"$name=\${$name}\""
    done
fi

rotated=0
while [ "$rotated" -lt "$user_argc" ]; do
    head=$1
    shift
    set -- "$@" "$head"
    rotated=$((rotated + 1))
done

exec "$@"

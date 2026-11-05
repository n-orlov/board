#!/bin/sh
# ci/image-check.sh -- inspects what ci/image-build.sh built (SPEC 12.4),
# pushing nothing:
#
#   1. both platforms' manifests are in the OCI layout, each runs as a
#      non-root user from /board, and each /board is an ELF binary for that
#      platform (ci/imagecheck, run through ci/run.sh);
#   2. the loaded amd64 image (board:ci) answers `board version`;
#   3. a container of it answers GET /healthz with 200.
#
# On the Jenkins agent, outside ci/run.sh for the docker parts (they need the
# docker CLI), in the Quality stage after ci/image-build.sh.
#
#   ci/image-check.sh [dir]    # default ci-results
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

dir=${1:-ci-results}
tag=board:ci
name="board-image-check-$$"
probe_seconds=${BOARD_IMAGE_PROBE_SECONDS:-30}

ci/run.sh go run ./ci/imagecheck "$dir/board-image-oci.tar"

echo "ci/image-check.sh: board version"
docker run --rm "$tag" version

# shellcheck disable=SC2317 # invoked by the trap below
cleanup() { docker rm --force "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

# BOARD_LISTEN is the image's :8080; publish it on a free loopback port.
docker run --detach --name "$name" --publish 127.0.0.1::8080 "$tag" >/dev/null
port=$(docker port "$name" 8080/tcp | sed -n 's/.*://p' | head -n 1)
[ -n "$port" ] || { echo "ci/image-check.sh: no published port for $name" >&2; exit 1; }

echo "ci/image-check.sh: GET /healthz on 127.0.0.1:$port"
attempt=0
while [ "$attempt" -lt "$probe_seconds" ]; do
	if code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/healthz") && [ "$code" = 200 ]; then
		echo "ci/image-check.sh: /healthz answered 200"
		exit 0
	fi
	attempt=$((attempt + 1))
	sleep 1
done
echo "ci/image-check.sh: /healthz did not answer 200 within ${probe_seconds}s; container log:" >&2
docker logs "$name" >&2 || true
exit 1

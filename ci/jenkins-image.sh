#!/bin/sh
# jenkins-image.sh: on a Jenkins agent, build the board-ci:local image that
# ci/run.sh uses and make the Go cache volume ($BOARD_CI_CACHE_VOLUME) usable
# by the agent's uid. Shared by Jenkinsfile and Jenkinsfile.release.
set -eu

: "${BOARD_CI_CACHE_VOLUME:?set BOARD_CI_CACHE_VOLUME}"

docker build -t board-ci:local -f ci/Dockerfile ci
# The image owns /go-cache as 1000:1000; the agent runs as its own uid, so
# its cache volume is chowned to it once.
docker volume inspect "$BOARD_CI_CACHE_VOLUME" >/dev/null 2>&1 ||
  docker volume create --label board.cache=go "$BOARD_CI_CACHE_VOLUME" >/dev/null
me="$(id -u):$(id -g)"
owner=$(docker run --rm --user 0 --entrypoint stat \
  --mount "type=volume,src=$BOARD_CI_CACHE_VOLUME,dst=/go-cache" board-ci:local -c %u:%g /go-cache)
if [ "$owner" != "$me" ]; then
  echo "chowning $BOARD_CI_CACHE_VOLUME from $owner to $me"
  docker run --rm --user 0 --entrypoint chown \
    --mount "type=volume,src=$BOARD_CI_CACHE_VOLUME,dst=/go-cache" board-ci:local -R "$me" /go-cache
fi

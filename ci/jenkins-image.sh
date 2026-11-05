#!/bin/sh
# jenkins-image.sh: on a Jenkins agent, build the board-ci:local image that
# ci/run.sh uses and make the Go cache volume ($BOARD_CI_CACHE_VOLUME) and the
# npm cache volume ($BOARD_CI_NPM_VOLUME) usable by the agent's uid.
set -eu

: "${BOARD_CI_CACHE_VOLUME:?set BOARD_CI_CACHE_VOLUME}"
: "${BOARD_CI_NPM_VOLUME:?set BOARD_CI_NPM_VOLUME}"

docker build -t board-ci:local -f ci/Dockerfile ci
# The image owns /go-cache and /npm-cache as 1000:1000; the agent runs as its
# own uid, so each cache volume is chowned to it once.
me="$(id -u):$(id -g)"
fix_volume() { # name, mount point, label
  docker volume inspect "$1" >/dev/null 2>&1 ||
    docker volume create --label "board.cache=$3" "$1" >/dev/null
  owner=$(docker run --rm --user 0 --entrypoint stat \
    --mount "type=volume,src=$1,dst=$2" board-ci:local -c %u:%g "$2")
  if [ "$owner" != "$me" ]; then
    echo "chowning $1 from $owner to $me"
    # An empty volume is populated from the image again at every container
    # start, which resets its owner: the marker file keeps it non-empty.
    docker run --rm --user 0 --entrypoint sh \
      --mount "type=volume,src=$1,dst=$2" board-ci:local -c 'touch "$1/.board-cache" && chown -R "$2" "$1"' sh "$2" "$me"
  fi
}
fix_volume "$BOARD_CI_CACHE_VOLUME" /go-cache go
fix_volume "$BOARD_CI_NPM_VOLUME" /npm-cache npm

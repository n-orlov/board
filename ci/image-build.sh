#!/bin/sh
# ci/image-build.sh -- builds the container image from the root Dockerfile and
# saves it as a tar, which ci/quality.sh's trivy-image and sbom-image gates
# scan. On the Jenkins agent, outside ci/run.sh (the build needs the docker
# CLI, which the CI image does not carry), in the Quality stage. The image is
# built for the agent's own platform and is never pushed.
#
#   ci/image-build.sh [tar]    # default ci-results/board-image.tar
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

tar_path=${1:-ci-results/board-image.tar}
tag=board:ci

mkdir -p "$(dirname "$tar_path")"
rm -f "$tar_path"
docker build -t "$tag" -f Dockerfile .
docker save -o "$tar_path" "$tag"
[ -s "$tar_path" ] || { echo "ci/image-build.sh: $tar_path is empty" >&2; exit 1; }
echo "ci/image-build.sh: $tag saved to $tar_path"

#!/bin/sh
# ci/release-build.sh -- builds every release asset (SPEC 12.5) into one
# directory and pushes, uploads and tags nothing:
#
#   vh_<os>_<arch>.tar.gz      linux and darwin x amd64 and arm64, each holding
#                              `vh` at its root, built with CGO_ENABLED=0 and
#                              -trimpath, the version and the revision stamped in
#   vh_<os>_<arch>.cdx.json    a CycloneDX SBOM per tarball (Trivy)
#   village-hall-image.cdx.json  the SBOM of the linux/amd64 image
#   install.sh, INSTALL-AGENT.md  copied from this commit
#   checksums.txt              `<sha256>  <file>` for every other file above
#   image/board-image-oci.tar  the multi-arch image `village-hall:<version>` as an
#                              OCI layout (linux/amd64 and linux/arm64), built by
#                              ci/image-build.sh; not a release asset: the publish
#                              step pushes it (ci/release-publish.sh)
#
# The files at the top of <outdir> are the release's assets; image/ holds the
# image. Run it on the CI agent outside ci/run.sh (the image build needs the
# docker CLI); the Go, npm, tar and Trivy parts go through ci/run.sh, so <outdir>
# must be inside this checkout. ci/release-check.sh runs it as the dry run.
#
#   ci/release-build.sh <version> <outdir>    # version: vMAJOR.MINOR.PATCH[-pre]
#
# The revision is BOARD_IMAGE_REVISION or the checked-out commit.
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

die() {
	echo "ci/release-build.sh: $*" >&2
	exit 1
}

[ $# -eq 2 ] || die "usage: ci/release-build.sh <version> <outdir>"
version=$1
if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
	die "version \"$version\" is not vMAJOR.MINOR.PATCH[-pre]"
fi

rm -rf "$2"
mkdir -p "$2"
outdir=$(cd "$2" && pwd -P)
rel=${outdir#"$repo_root"/}
[ "$rel" != "$outdir" ] || die "$2 is not inside the checkout $repo_root"

revision=${BOARD_IMAGE_REVISION:-$(git rev-parse HEAD)}
epoch=$(git log -1 --format=%ct "$revision" 2>/dev/null || date +%s)
work="$rel/.work"
mkdir -p "$outdir/.work"
trap 'rm -rf "$outdir/.work"' EXIT INT TERM

targets="linux_amd64 linux_arm64 darwin_amd64 darwin_arm64"
ldflags="-s -w -X github.com/n-orlov/village-hall/internal/version.version=$version -X github.com/n-orlov/village-hall/internal/version.revision=$revision"

echo "ci/release-build.sh: the web UI, embedded into every binary"
ci/run.sh sh -c '[ -d web/node_modules ] || (cd web && npm ci --no-audit --no-fund); cd web && npm run build'

for target in $targets; do
	os=${target%_*}
	arch=${target#*_}
	echo "ci/release-build.sh: vh $version for $os/$arch"
	mkdir -p "$outdir/.work/$target"
	ci/run.sh env CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
		go build -trimpath -buildvcs=false -tags timetzdata -ldflags "$ldflags" \
		-o "$work/$target/vh" ./cmd/vh
	# Same bytes for the same inputs: sorted, root-owned, the commit's own time.
	ci/run.sh sh -c "tar -C '$work/$target' --sort=name --owner=0 --group=0 --numeric-owner --mtime='@$epoch' --mode=0755 -cf - vh | gzip -n -9 > '$rel/vh_$target.tar.gz'"
done

# Trivy reads the module list a Go binary carries; the license scanner needs no
# vulnerability database. The cache lives in the CI image's /go-cache volume.
for target in $targets; do
	echo "ci/release-build.sh: SBOM of vh_$target.tar.gz"
	ci/run.sh sh -c "rm -rf /tmp/sbom && mkdir -p /tmp/sbom/vh_$target && cp '$work/$target/vh' /tmp/sbom/vh_$target/ && trivy rootfs --scanners license --format cyclonedx --output '$rel/vh_$target.cdx.json' --cache-dir /go-cache/trivy --skip-version-check --quiet /tmp/sbom/vh_$target"
done

echo "ci/release-build.sh: the multi-arch image village-hall:$version"
mkdir -p "$outdir/image"
BOARD_IMAGE_VERSION=$version BOARD_IMAGE_REVISION=$revision \
	BOARD_IMAGE_NAME="village-hall:$version" BOARD_IMAGE_TAG="village-hall:$version" \
	ci/image-build.sh "$rel/image/board-image.tar"
echo "ci/release-build.sh: SBOM of the image"
ci/run.sh trivy image --input "$rel/image/board-image.tar" --scanners license --format cyclonedx \
	--output "$rel/village-hall-image.cdx.json" --cache-dir /go-cache/trivy --skip-version-check --quiet
# The docker-save tar only fed the SBOM; the OCI layout is the image's artifact.
rm -f "$outdir/image/board-image.tar"

cp -p install.sh INSTALL-AGENT.md "$outdir/"

echo "ci/release-build.sh: checksums.txt"
ci/run.sh sh -c "cd '$rel' && sha256sum \$(ls -p | grep -v '/\$' | grep -vx checksums.txt | LC_ALL=C sort) > checksums.txt"

for want in $(for target in $targets; do echo "vh_$target.tar.gz vh_$target.cdx.json"; done) \
	village-hall-image.cdx.json install.sh INSTALL-AGENT.md checksums.txt image/board-image-oci.tar; do
	[ -s "$outdir/$want" ] || die "$want was not produced"
done
echo "ci/release-build.sh: $version at $revision built into $outdir"

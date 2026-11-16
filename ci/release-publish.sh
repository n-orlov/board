#!/bin/sh
# ci/release-publish.sh -- publishes a release (SPEC 15.3), in these seven
# steps, in this order:
#
#   1. verify the candidate: the CI build is SUCCESS on main, its
#      release-candidate.json names the commit being released, and VERSION is
#      vMAJOR.MINOR.PATCH[-pre]
#   2. refuse when the tag or the release already exists
#   3. create a draft release at the candidate commit
#   4. upload every asset (the files at the top of <outdir>)
#   5. push the image tagged VERSION and put its digest in the release notes
#   6. publish the draft
#   7. read the release back and require `immutable: true`; otherwise fail,
#      saying what to fix
#
# DRY_RUN=true (the default) prints each step and calls nothing that writes:
# steps 1 and 2 only read, steps 3 to 7 say what they would do.
#
#   ci/release-publish.sh <outdir>
#
# <outdir> is what ci/release-build.sh wrote. The rest is the environment:
#
#   VERSION     vMAJOR.MINOR.PATCH[-pre] (required)
#   CI_BUILD    the board/ci main build number; empty = whatever the candidate says
#   REGISTRY    the registry the image is pushed to, host[:port][/path] (required)
#   IMAGE_NAME  the repository name in the registry (default village-hall)
#   DRY_RUN     true (default) or false
#
# Optional: CANDIDATE (the release-candidate.json, default
# candidate/ci-results/release-candidate.json), RELEASE_SHA (the commit that is
# checked out, default `git rev-parse HEAD`), REPO (owner/name, default the
# origin remote), JENKINS_USER and JENKINS_TOKEN (to read the CI build),
# REGISTRY_USER and REGISTRY_PASSWORD (to push), and gh's own GH_TOKEN. The
# image is pushed from the OCI layout by skopeo in a throwaway container, with
# a docker login kept in a temporary config, so no secret is in an argument list.
set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

# Pinned by digest, like every image here. Bumping it is this file's only edit.
skopeo_image='quay.io/skopeo/stable:v1.20.0@sha256:47853bb9fb24202af9110531ebd6e43c5f97701254ca290596640290d17942f4'

die() {
	echo "ci/release-publish.sh: $*" >&2
	exit 1
}

[ $# -eq 1 ] || die "usage: ci/release-publish.sh <outdir>   (VERSION, CI_BUILD, REGISTRY, IMAGE_NAME and DRY_RUN in the environment)"
outdir=$1
version=${VERSION-}
ci_build=${CI_BUILD-}
registry=${REGISTRY-}
image_name=${IMAGE_NAME:-village-hall}
dry_run=${DRY_RUN:-true}
candidate=${CANDIDATE:-candidate/ci-results/release-candidate.json}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

step() {
	echo "ci/release-publish.sh: step $1/7: $2"
}

# dry returns 0 after saying what a dry run would do; a real run returns 1.
dry() {
	if [ "$dry_run" = true ]; then
		echo "ci/release-publish.sh: DRY RUN, would $*"
		return 0
	fi
	return 1
}

# json_top <key> prints the raw value of a top-level member of the JSON object on stdin.
json_top() {
	awk -v want="$1" '
    { text = text $0 "\n" }
    END {
      n = length(text); depth = 0; i = 1
      while (i <= n) {
        c = substr(text, i, 1)
        if (c == "{" || c == "[") { depth++; i++; continue }
        if (c == "}" || c == "]") { depth--; i++; continue }
        if (c != "\"") { i++; continue }
        j = i + 1; tok = ""
        while (j <= n) {
          d = substr(text, j, 1)
          if (d == "\\") { tok = tok d substr(text, j + 1, 1); j += 2; continue }
          if (d == "\"") break
          tok = tok d; j++
        }
        i = j + 1
        if (depth != 1) continue
        k = i
        while (substr(text, k, 1) ~ /[ \t\r\n]/) k++
        if (substr(text, k, 1) != ":") continue
        k++
        while (substr(text, k, 1) ~ /[ \t\r\n]/) k++
        v = ""
        if (substr(text, k, 1) == "\"") {
          m = k + 1
          while (m <= n && substr(text, m, 1) != "\"") { if (substr(text, m, 1) == "\\") m++; m++ }
          v = substr(text, k, m - k + 1)
        } else {
          m = k
          while (m <= n && substr(text, m, 1) !~ /[ \t\r\n,}\]]/) m++
          v = substr(text, k, m - k)
        }
        if (tok == want) { print v; exit }
        i = k
      }
    }'
}

unquote() {
	sed -n 's/^"\(.*\)"$/\1/p'
}

# ---------------------------------------------------------------- 1. the candidate
step 1 "verify the candidate"
case "$dry_run" in true | false) ;; *) die "DRY_RUN \"$dry_run\" is neither true nor false" ;; esac
if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
	die "VERSION \"$version\" is not vMAJOR.MINOR.PATCH[-pre]; nothing was published"
fi
if [ -n "$ci_build" ] && ! printf '%s\n' "$ci_build" | grep -Eq '^[0-9]+$'; then
	die "CI_BUILD \"$ci_build\" is not a build number; nothing was published"
fi
[ -n "$registry" ] || die "REGISTRY is empty: name the registry the image is pushed to; nothing was published"
printf '%s\n' "$registry" | grep -Eq '^[A-Za-z0-9.-]+(:[0-9]+)?(/[a-z0-9._-]+)*$' ||
	die "REGISTRY \"$registry\" is not host[:port][/path] (no scheme); nothing was published"
printf '%s\n' "$image_name" | grep -Eq '^[a-z0-9]+([._/-][a-z0-9]+)*$' ||
	die "IMAGE_NAME \"$image_name\" is not a repository name; nothing was published"
[ -d "$outdir" ] || die "$outdir is not a directory (the output of ci/release-build.sh); nothing was published"
oci="$outdir/image/board-image-oci.tar"
[ -s "$oci" ] || die "$oci is missing or empty: run ci/release-build.sh first; nothing was published"
[ -s "$outdir/checksums.txt" ] || die "$outdir/checksums.txt is missing or empty: run ci/release-build.sh first; nothing was published"

repo=${REPO:-}
if [ -z "$repo" ]; then
	url=$(git remote get-url origin 2>/dev/null) || die "no REPO and no origin remote to read the repository from"
	repo=$(printf '%s\n' "$url" | sed -e 's#/*$##' -e 's#\.git$##' -e 's#^.*[:/]\([^/:]*/[^/:]*\)$#\1#')
fi
printf '%s\n' "$repo" | grep -Eq '^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$' || die "repository \"$repo\" is not owner/name"

[ -f "$candidate" ] || die "no release candidate at $candidate: only a fully green main build of the CI job archives one; nothing was published"
sha=$(json_top commit <"$candidate" | unquote)
build_url=$(json_top build_url <"$candidate" | unquote)
build_number=$(json_top build_number <"$candidate")
printf '%s\n' "$sha" | grep -Eq '^[0-9a-f]{40}$' || die "the candidate's commit \"$sha\" is not a full sha; nothing was published"
if [ -n "$ci_build" ] && [ "$build_number" != "$ci_build" ]; then
	die "CI_BUILD is $ci_build but the candidate is from build $build_number; nothing was published"
fi
printf '%s\n' "$build_url" | grep -Eq '/job/main/[0-9]+/?$' ||
	die "the candidate's build \"$build_url\" is not a build of the main branch (.../job/main/<n>); nothing was published"
checked_out=${RELEASE_SHA:-$(git rev-parse HEAD 2>/dev/null || true)}
[ "$checked_out" = "$sha" ] ||
	die "the candidate commit is $sha but $checked_out is checked out: release exactly the candidate commit; nothing was published"

build_json="$work/build.json"
build_api="${build_url%/}/api/json?tree=result,building,actions[lastBuiltRevision[SHA1]]"
if [ -n "${JENKINS_USER-}" ] && [ -n "${JENKINS_TOKEN-}" ]; then
	printf 'user = "%s:%s"\n' "$JENKINS_USER" "$JENKINS_TOKEN" |
		curl -fsS -g --config - -o "$build_json" "$build_api" || die "could not read the CI build $build_url; nothing was published"
else
	curl -fsS -g -o "$build_json" "$build_api" || die "could not read the CI build $build_url; nothing was published"
fi
result=$(json_top result <"$build_json" | unquote)
[ "$result" = SUCCESS ] || die "the CI build $build_url is ${result:-not finished}, not SUCCESS: only a fully green main build is releasable; nothing was published"
grep -o '"SHA1" *: *"[0-9a-f]*"' "$build_json" | grep -q "\"$sha\"" ||
	die "the CI build $build_url did not build the candidate commit $sha; nothing was published"
echo "ci/release-publish.sh: $version is the candidate $sha from $build_url"

# ---------------------------------------------------------------- 2. no tag, no release
step 2 "refuse an existing tag or release"
# A read that fails only means "absent" when the API says 404; anything else is an error.
absent() {
	what=$1
	shift
	if "$@" >"$work/probe.out" 2>&1; then
		die "the $what $version already exists in $repo: releases are immutable, pick a new version; nothing was published"
	fi
	grep -Eqi '404|not found' "$work/probe.out" || die "could not check whether the $what $version exists in $repo: $(cat "$work/probe.out"); nothing was published"
}
absent tag gh api "repos/$repo/git/ref/tags/$version"
absent release gh release view "$version" --repo "$repo"

# ---------------------------------------------------------------- 3. the draft
step 3 "create a draft release at $sha"
if ! dry "create the draft release $version of $repo at $sha"; then
	gh release create "$version" --repo "$repo" --draft --target "$sha" --title "$version" \
		--notes "$version, built from $sha by $build_url. The container image digest is added before this release is published."
fi

# ---------------------------------------------------------------- 4. the assets
step 4 "upload every asset"
assets=$(find "$outdir" -maxdepth 1 -type f | sort)
[ -n "$assets" ] || die "$outdir holds no assets"
if ! dry "upload $(printf '%s\n' "$assets" | wc -l | tr -d ' ') assets: $(printf '%s\n' "$assets" | sed 's#.*/##' | tr '\n' ' ')"; then
	printf '%s\n' "$assets" | while IFS= read -r asset; do
		gh release upload "$version" "$asset" --repo "$repo"
	done
fi

# ---------------------------------------------------------------- 5. the image
ref="$registry/$image_name:$version"
step 5 "push the image $ref and put its digest in the release notes"
logged=""
# skopeo <copy|inspect> args...: skopeo in a throwaway container, with the login if there is one.
skopeo() {
	sub=$1
	shift
	if [ -n "$logged" ]; then
		docker run --rm --user "$(id -u):$(id -g)" -v "$work/docker:/auth:ro" -v "$image_dir:/image:ro" \
			"$skopeo_image" "$sub" --authfile /auth/config.json "$@"
	else
		docker run --rm --user "$(id -u):$(id -g)" -v "$image_dir:/image:ro" "$skopeo_image" "$sub" "$@"
	fi
}
if ! dry "push $oci to $ref and add the digest to the release notes"; then
	image_dir=$(cd "$outdir/image" && pwd -P)
	if [ -n "${REGISTRY_USER-}" ] && [ -n "${REGISTRY_PASSWORD-}" ]; then
		mkdir -p "$work/docker"
		printf '%s\n' "$REGISTRY_PASSWORD" |
			DOCKER_CONFIG="$work/docker" docker login "${registry%%/*}" --username "$REGISTRY_USER" --password-stdin >/dev/null
		logged=1
	fi
	skopeo copy --all "oci-archive:/image/board-image-oci.tar" "docker://$ref"
	digest=$(skopeo inspect --format '{{.Digest}}' "docker://$ref")
	printf '%s\n' "$digest" | grep -Eq '^sha256:[0-9a-f]{64}$' || die "the pushed image $ref has no readable digest (\"$digest\"); the draft $version is left unpublished"
	cat >"$work/notes.md" <<EOF
$version, built from $sha by $build_url.

Container image: \`$ref\`, pinned by digest: \`$registry/$image_name@$digest\`
EOF
	gh release edit "$version" --repo "$repo" --notes-file "$work/notes.md"
fi

# ---------------------------------------------------------------- 6. publish
step 6 "publish the draft"
if ! dry "publish the draft release $version"; then
	gh release edit "$version" --repo "$repo" --draft=false
fi

# ---------------------------------------------------------------- 7. immutable
step 7 "read the release back and require immutable: true"
if ! dry "read $version back and require immutable: true"; then
	gh api "repos/$repo/releases/tags/$version" >"$work/published.json" || die "could not read the published release $version back; check it by hand"
	immutable=$(json_top immutable <"$work/published.json")
	if [ "$immutable" != true ]; then
		die "release $version of $repo is published but is not immutable (immutable: ${immutable:-absent}). Enable immutable releases for the repository (Settings, General, Releases, release immutability; or PUT /repos/$repo/immutable-releases), then release a NEW version: this tag stays mutable and must not be reused"
	fi
	echo "ci/release-publish.sh: $version of $repo is published and immutable"
fi

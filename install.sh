#!/bin/sh
# Install vh (Village Hall's CLI) from a GitHub release of the public repository.
#
#   curl -fsSL https://raw.githubusercontent.com/n-orlov/village-hall/main/install.sh | sh
#
# Env:  VH_VERSION=vX.Y.Z        pin a release (default: latest)
#       VH_INSTALL_DIR=DIR       where to put the binary (default: ~/.local/bin)
#       VH_REPO=OWNER/NAME       the repository to read releases from (default: n-orlov/village-hall)
#       VH_REQUIRE_IMMUTABLE=0   install a release that is not confirmed immutable
#                                (exactly 0; any other value keeps the check on)
#       VH_API_URL, VH_DOWNLOAD_URL
#                                base URLs of the release API (default https://api.github.com)
#                                and of the release downloads (default https://github.com).
#                                For tests: https://, or http:// only on 127.0.0.1,
#                                localhost or [::1]; anything else is refused.
#
# The repository is public, so every request is anonymous: no token is read or sent.
#
# Integrity: the release API must say the release is immutable, and the asset must match
# its line in checksums.txt, which comes from the same release. Every curl is HTTPS-only
# (--proto =https; plain http only for a loopback test override).
set -eu

repo="${VH_REPO:-n-orlov/village-hall}"
version="${VH_VERSION:-latest}"
dir="${VH_INSTALL_DIR:-$HOME/.local/bin}"
api_base="${VH_API_URL:-https://api.github.com}"
dl_base="${VH_DOWNLOAD_URL:-https://github.com}"
checksums=checksums.txt

tmp=
staged=
cleanup() {
  [ -z "$staged" ] || rm -f "$staged"
  [ -z "$tmp" ] || rm -rf "$tmp"
}
trap cleanup EXIT
trap 'exit 1' INT TERM

die() { echo "vh install: $*" >&2; exit 1; }

# base_ok accepts an https:// URL, or an http:// URL whose parsed authority is a loopback
# host: exactly 127.0.0.1, localhost or [::1], no userinfo, and a port, if any, of 1-65535.
base_ok() {
  case $1 in
    https://?*) return 0 ;;
    http://?*) ;;
    *) return 1 ;;
  esac
  rest=${1#http://}
  auth=${rest%%[/?#]*}
  case $auth in
    *[!A-Za-z0-9.:[\]]*) return 1 ;; # userinfo (@), backslash, space, control or other characters
  esac
  case $auth in
    '[::1]') return 0 ;;
    '[::1]':*) port=${auth#'[::1]':} ;;
    127.0.0.1) return 0 ;;
    127.0.0.1:*) port=${auth#127.0.0.1:} ;;
    localhost) return 0 ;;
    localhost:*) port=${auth#localhost:} ;;
    *) return 1 ;;
  esac
  case $port in
    '' | *[!0-9]*) return 1 ;;
  esac
  while :; do # drop leading zeros, so the length test below bounds the value
    case $port in
      0?*) port=${port#0} ;;
      *) break ;;
    esac
  done
  [ "${#port}" -le 5 ] && [ "$port" -ge 1 ] && [ "$port" -le 65535 ]
}
base_ok "$api_base" || die "VH_API_URL '$api_base' is refused: only https is allowed (http only for 127.0.0.1, localhost or [::1])"
base_ok "$dl_base" || die "VH_DOWNLOAD_URL '$dl_base' is refused: only https is allowed (http only for 127.0.0.1, localhost or [::1])"
api_base=${api_base%/}
dl_base=${dl_base%/}

printf '%s\n' "$repo" | grep -Eq '^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$' || die "VH_REPO '$repo' is not OWNER/NAME"
is_tag() { printf '%s\n' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; }
if [ "$version" != latest ]; then
  is_tag "$version" || die "VH_VERSION '$version' is not a release tag (vX.Y.Z)"
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in
  linux | darwin) ;;
  *) die "unsupported OS '$os': vh is built for linux and darwin" ;;
esac
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "unsupported architecture '$arch': vh is built for amd64 and arm64" ;;
esac
asset="vh_${os}_${arch}.tar.gz"

fetch() { # fetch <url> <outfile>; redirects may only lead to https
  case $1 in
    http://*) curl --proto =https,http --proto-redir =https --tlsv1.2 -fsSL --connect-timeout 15 --max-time 300 -o "$2" "$1" ;;
    *) curl --proto =https --proto-redir =https --tlsv1.2 -fsSL --connect-timeout 15 --max-time 300 -o "$2" "$1" ;;
  esac
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

tmp=$(mktemp -d)

if [ "${VH_REQUIRE_IMMUTABLE-}" = 0 ]; then
  require=0
  echo "vh install: note: VH_REQUIRE_IMMUTABLE=0, a release that is not confirmed immutable will be installed" >&2
else
  require=1
fi

# The release API says which tag is "latest" and whether the release is immutable.
if [ "$version" = latest ]; then
  release_url="$api_base/repos/$repo/releases/latest"
else
  release_url="$api_base/repos/$repo/releases/tags/$version"
fi
tag=$version
immutable=unknown
if fetch "$release_url" "$tmp/release.json" 2>/dev/null; then
  immutable=$(json_top immutable <"$tmp/release.json")
  if [ "$version" = latest ]; then
    tag=$(json_top tag_name <"$tmp/release.json" | sed -n 's/^"\(.*\)"$/\1/p')
    [ -n "$tag" ] || tag=latest
  fi
fi

if [ "$immutable" != true ]; then
  if [ "$require" = 1 ]; then
    if [ "$immutable" = false ]; then
      die "release $tag of $repo is not an immutable release, and an immutable release is required; nothing was installed (set VH_REQUIRE_IMMUTABLE=0 to install it anyway)"
    fi
    die "release of $repo could not be confirmed immutable: the release API gave no usable answer; nothing was installed (set VH_REQUIRE_IMMUTABLE=0 to install it anyway)"
  fi
  echo "vh install: note: the release is not confirmed immutable" >&2
fi

if [ "$require" = 1 ] && [ "$tag" = latest ]; then
  die "the release API named no tag for the latest release of $repo; nothing was installed"
fi
if [ "$tag" = latest ]; then
  base="$dl_base/$repo/releases/latest/download"
else
  is_tag "$tag" || die "the release API named '$tag', which is not a release tag; nothing was installed"
  base="$dl_base/$repo/releases/download/$tag"
fi

echo "vh install: fetching ${tag} $asset"
fetch "$base/$asset" "$tmp/$asset" || die "could not download $asset from $repo; nothing was installed"
fetch "$base/$checksums" "$tmp/$checksums" || die "could not download $checksums from $repo; nothing was installed"

want=$(awk -v a="$asset" '$2 == a || $2 == "*" a { print $1 }' "$tmp/$checksums")
case $want in
  '' | *[!0-9A-Fa-f]*) die "$checksums has no single checksum line for $asset; nothing was installed" ;;
esac
[ "${#want}" -eq 64 ] || die "$checksums has no SHA-256 for $asset; nothing was installed"
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$asset" | awk '{ print $1 }')
else
  got=$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')
fi
want=$(printf '%s' "$want" | tr 'A-F' 'a-f')
[ "$got" = "$want" ] || die "checksum mismatch for $asset (expected $want, got $got); nothing was installed"

mkdir "$tmp/x"
tar -xzf "$tmp/$asset" -C "$tmp/x" vh || die "$asset does not hold a vh binary; nothing was installed"
[ -f "$tmp/x/vh" ] || die "$asset does not hold a vh binary; nothing was installed"

mkdir -p "$dir"
# Copy then rename: replacing a running vh in place fails with "text file busy".
staged=$(mktemp "$dir/.vh.XXXXXX")
cp "$tmp/x/vh" "$staged"
chmod 0755 "$staged"
mv -f "$staged" "$dir/vh"
staged=

echo "vh install: installed to $dir/vh"
"$dir/vh" version
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "vh install: note: $dir is not on your PATH" >&2 ;;
esac

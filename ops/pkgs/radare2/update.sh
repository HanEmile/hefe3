#!/usr/bin/env bash
# Updates //ops/pkgs/radare2/package.nix to the newest upstream release:
# resolves the latest git tag, prefetches the source tarball (unpacked,
# matching fetchFromGitHub/fetchzip semantics), rewrites the `version` and
# `srcHash` fields in place and build-checks the result. If the build
# fails, the old package.nix is restored ("rollback on failure").
#
# Note: this only bumps radare2 itself. The pinned subproject sources
# (sdb/qjs) in package.nix follow subprojects/*.wrap of the release; if
# upstream bumps them the build check will fail - bump those pins too.
set -euo pipefail

cd "$(dirname "$0")"
root="$(cd ../../.. && pwd)"

REPO="https://github.com/radareorg/radare2.git"

latest="$(
  git ls-remote --tags --sort=-v:refname "$REPO" 'refs/tags/[0-9]*.[0-9]*.[0-9]*' \
    | sed -n 's|.*refs/tags/\([0-9][0-9.]*\)$|\1|p' \
    | head -n1
)"
if [ -z "$latest" ]; then
  echo "error: could not resolve latest release tag of $REPO" >&2
  exit 1
fi

current="$(sed -n 's|^  version = "\(.*\)";|\1|p' package.nix | head -n1)"
if [ "$current" = "$latest" ]; then
  echo "radare2 already up to date ($latest)"
  exit 0
fi

url="https://github.com/radareorg/radare2/archive/$latest.tar.gz"
base32="$(nix-prefetch-url --unpack --type sha256 "$url" | tail -n 1)"
hash="$(nix-hash --to-sri --type sha256 "$base32")"

cp package.nix package.nix.old
restore_old() {
  mv package.nix.old package.nix
  echo "radare2: update to $latest failed, package.nix restored to $current" >&2
}
trap restore_old ERR

sed -i '' \
  -e "s|^  version = \".*\";|  version = \"$latest\";|" \
  -e "s|^  srcHash = \".*\";|  srcHash = \"$hash\";|" \
  package.nix

echo "radare2: $current -> $latest (build-checking...)"
if nix-build "$root" -A ops.pkgs.radare2 --no-out-link >/dev/null 2>&1; then
  rm package.nix.old
  trap - ERR
  echo "radare2: updated to $latest"
else
  echo "radare2: build failed for $latest" >&2
  restore_old
  trap - ERR
  exit 1
fi

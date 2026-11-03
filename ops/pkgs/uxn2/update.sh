#!/usr/bin/env bash
# Updates //ops/pkgs/uxn2/package.nix to the newest upstream "main":
# resolves main to a commit, prefetches the source tarball (unpacked,
# matching fetchFromSourcehut/fetchzip semantics) and rewrites the
# `rev` and `hash` fields in place. The result is build-checked; if the
# build fails, the old package.nix is restored ("rollback on failure").
set -euo pipefail

cd "$(dirname "$0")"
root="$(cd ../../.. && pwd)"

REPO="https://git.sr.ht/~rabbits/uxn2"

latest="$(git ls-remote "$REPO" main | awk '{print $1}')"
if [ -z "$latest" ]; then
  echo "error: could not resolve $REPO main" >&2
  exit 1
fi

current_rev="$(sed -n 's/.*rev = "\(.*\)";/\1/p' package.nix)"
if [ "$current_rev" = "$latest" ]; then
  echo "uxn2 already up to date ($latest)"
  exit 0
fi

url="$REPO/archive/$latest.tar.gz"
base32="$(nix-prefetch-url --unpack --type sha256 "$url" | tail -n 1)"
hash="$(nix-hash --to-sri --type sha256 "$base32")"

cp package.nix package.nix.old
restore_old() {
  mv package.nix.old package.nix
  echo "uxn2: update to $latest failed, package.nix restored to $current_rev" >&2
}
trap restore_old ERR

sed -i '' \
  -e "s|rev = \".*\";|rev = \"$latest\";|" \
  -e "s|hash = \".*\";|hash = \"$hash\";|" \
  package.nix

echo "uxn2: $current_rev -> $latest (build-checking...)"
if nix-build "$root" -A ops.pkgs.uxn2 --no-out-link >/dev/null 2>&1; then
  rm package.nix.old
  trap - ERR
  echo "uxn2: updated to $latest"
else
  echo "uxn2: build failed for $latest" >&2
  restore_old
  trap - ERR
  exit 1
fi

#!/usr/bin/env bash
# Runs every update script in the tree. Any package dir containing an
# update.sh (by convention //ops/pkgs/*/update.sh) gets refreshed.
set -euo pipefail

root="$(cd "$(dirname "$0")/../../.." && pwd)"

found=0
for script in "$root"/ops/pkgs/*/update.sh; do
  [ -e "$script" ] || continue
  found=1
  echo "=== ${script#"$root"/} ==="
  bash "$script"
done

if [ "$found" -eq 0 ]; then
  echo "no update scripts found"
fi

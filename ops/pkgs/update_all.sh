#!/usr/bin/env bash
# Runs every ops/pkgs/*/update.sh and prints a summary.
#
# Each package's update.sh is expected to be self-guarding: skip when the
# package is already up to date ("... already up to date" in the output),
# build-check the result, and restore the old package.nix on failure
# (see //ops/pkgs/radare2/update.sh for the pattern).
set -uo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"

updated=()
current=()
failed=()

for script in "$root"/ops/pkgs/*/update.sh; do
  [ -e "$script" ] || continue
  name="$(basename "$(dirname "$script")")"

  echo "=== $name ==="
  if out="$(bash "$script" 2>&1)"; then
    echo "$out" | sed 's/^/  /'
    if echo "$out" | grep -q "up to date"; then
      current+=("$name")
    else
      updated+=("$name")
    fi
  else
    echo "$out" | sed 's/^/  /'
    failed+=("$name")
  fi
done

if [ "${#updated[@]}" -eq 0 ] && [ "${#current[@]}" -eq 0 ] && [ "${#failed[@]}" -eq 0 ]; then
  echo "no update scripts found"
  exit 0
fi

echo
echo "summary: ${#updated[@]} updated, ${#current[@]} up to date, ${#failed[@]} failed"
if [ "${#updated[@]}" -gt 0 ]; then
  printf '  updated:            %s\n' "${updated[*]}"
fi
if [ "${#failed[@]}" -gt 0 ]; then
  printf '  failed (rolled back): %s\n' "${failed[*]}"
  exit 1
fi

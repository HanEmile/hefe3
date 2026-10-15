#!/usr/bin/env bash
# Deploy every VM under ops/vms/x86, 4 at a time. Each VM gets its own
# result-<vm> symlink so the parallel builds don't clobber each other's
# ./result link (the old script raced on it).
set -u

cd "$(dirname "$0")"

deploy() {
  vm="$1"
  if nix-build -A "ops.nixos.$vm.deploy" -o "result-$vm" && "./result-$vm/bin/deploy"; then
    echo "ok: $vm"
  else
    echo "FAIL: $vm" >&2
    return 1
  fi
}
export -f deploy

find ops/vms/x86 -mindepth 1 -maxdepth 1 -type d -exec basename {} \; \
  | xargs -I{} -P 4 bash -c 'deploy "$@"' _ {}

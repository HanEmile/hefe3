#!/usr/bin/env bash
# fleet-self-upgrade - lampadas upgrades itself, 03:30 nightly.
#
# Own unit so the fleet-upgrade.service never kills itself mid-run when the
# switch restarts services. Never reboot lampadas here: its LUKS unlock is not
# reliable enough for unattended reboots, so kernel switches require a manual,
# attended reboot.

set -uo pipefail

STATE=/var/lib/fleet
LOG=$STATE/last-run.log

echo "===== fleet-self-upgrade $(date -Is) =====" | tee -a "$LOG"

cd "$FLEET_CHECKOUT" || exit 0

if ! nix-build . -A ops.nixos.lampadas.deploy -o "$STATE/result-lampadas" >>"$LOG.nix" 2>&1; then
  echo "nix-build lampadas FAILED" | tee -a "$LOG"
  exit 0
fi
if ! "$STATE/result-lampadas/bin/deploy" >>"$LOG" 2>&1; then
  echo "lampadas deploy FAILED" | tee -a "$LOG"
  exit 0
fi

# A changed kernel is activated only by the next manual reboot. Never call
# systemctl reboot from this unattended job (2026-11-17: another overnight
# outage ended at the interactive LUKS prompt).
if [ "$(readlink /run/booted-system/kernel)" != "$(readlink /run/current-system/kernel)" ]; then
  echo "kernel changed; manual reboot required" | tee -a "$LOG"
fi

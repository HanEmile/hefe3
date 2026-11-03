#!/usr/bin/env bash
# fleet-self-upgrade - lampadas upgrades and reboots itself, 03:30 nightly.
#
# Own unit so the fleet-upgrade.service never kills itself mid-run when the
# switch restarts services. Before rebooting we arm the unlock watcher on
# medano, which feeds the LUKS passphrase when lampadas' initrd sshd appears.

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

# kernel (or initrd) changed?
if [ "$(readlink /run/booted-system/kernel)" = "$(readlink /run/current-system/kernel)" ]; then
  echo "kernel unchanged; no reboot" | tee -a "$LOG"
  exit 0
fi

echo "kernel changed - arming medano unlock watcher, then rebooting" | tee -a "$LOG"

# arm the watcher on medano; it polls for lampadas' initrd sshd (port 22)
ssh -o ConnectTimeout=10 -o BatchMode=yes medano-root \
  'systemctl start fleet-unlock-lampadas' >>"$LOG" 2>&1 || \
  echo "WARNING: could not arm medano watcher - manual unlock will be needed" | tee -a "$LOG"

# record our own reboot intent for the 08:00 report
if [ -f "$STATE/results.json" ]; then
  tmp=$(mktemp)
  jq '. + {lampadas_self: {rebooted: "yes", at: (now|todate)}}' "$STATE/results.json" > "$tmp" && mv "$tmp" "$STATE/results.json"
fi

sleep 5
systemctl reboot

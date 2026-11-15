#!/usr/bin/env bash
# fleet-rescue - magic-rollback watchdog for lampadas, every 2 minutes.
#
# deploy-rs style "magic rollback": after any activation (manual or fleet-
# driven) the system must return to a healthy steady state on its own.
# The 2026-11-15 incident: fleet-self-upgrade's unit got SIGTERM'd by its
# own switch-to-configuration mid-teardown; every app service was stopped
# and nothing ever restarted them - 7h of "everything down" until noticed.
#
# This timer is the confirmation step a raw switch lacks:
#
#   state == degraded  ->  1. re-activate the current profile (completes an
#                             interrupted switch; restarts wanted-but-stopped
#                             units)
#                          2. still degraded?  ->  roll the system profile
#                             back one generation and activate THAT
#                             (once; a marker prevents rollback loops)
#                          3. healthy again      ->  clear the marker
#
# Only "degraded" is acted on. "starting"/"running" pass through, shutdowns
# and in-flight switches are explicitly left alone. Never reboots anything.

set -uo pipefail

STATE=/var/lib/fleet
ROLLBACK_MARKER=$STATE/rescue-rollback.done
LOG=$STATE/rescue.log

log() { echo "$(date -Is) $*" | tee -a "$LOG"; }

# ------------------------------------------------------- leave-alone gates --

state=$(systemctl is-system-running 2>/dev/null || true)
[ "$state" = running ] && exit 0
[ "$state" = degraded ] || { log "state='$state' - not our problem, leaving alone"; exit 0; }

# never fight an in-flight activation or an ongoing shutdown/reboot
if pgrep -f 'switch-to-configuration' >/dev/null 2>&1; then
  exit 0
fi
if systemctl is-active --quiet shutdown.target || systemctl is-active --quiet reboot.target; then
  exit 0
fi

# ----------------------------------------------------------- irc (only when
# acting - no tunnel churn on healthy ticks)

_IRC_TUNNEL=""
say() {
  [ -n "$_IRC_TUNNEL" ] || {
    ssh -o BatchMode=yes -o ConnectTimeout=10 -N -L 16667:192.168.75.23:6667 medano-root >/dev/null 2>&1 &
    _IRC_TUNNEL=$!
    sleep 3
  }
  "$FLEET_SYSWATCH" --server 127.0.0.1:16667 --channel '#emile' --say "$1" || true
}

# ----------------------------------------------------------------- heal -----

FAILED_UNITS=$(systemctl --failed --no-legend --plain 2>/dev/null | awk '{print $1}' | tr '\n' ' ')
log "DEGRADED - completing interrupted activation (failed: ${FAILED_UNITS:-none})"
say "fleet-rescue: lampadas DEGRADED (${FAILED_UNITS:-no failed units}) - re-activating current profile"

/run/current-system/bin/switch-to-configuration switch >>"$LOG" 2>&1 || true

state=$(systemctl is-system-running 2>/dev/null || true)
if [ "$state" = running ]; then
  log "healed after re-activation"
  say "fleet-rescue: lampadas healed by re-activation"
  rm -f "$ROLLBACK_MARKER"
  exit 0
fi

# ---------------------------------------------------------- magic rollback --

if [ -f "$ROLLBACK_MARKER" ]; then
  log "STILL DEGRADED after re-activation, but rollback already done once ($(cat "$ROLLBACK_MARKER" 2>/dev/null)) - not looping"
  exit 0
fi

log "still degraded after re-activation - MAGIC ROLLBACK to previous generation"
nix-env -p /nix/var/nix/profiles/system --rollback >>"$LOG" 2>&1 || { log "nix-env --rollback FAILED"; exit 0; }
/nix/var/nix/profiles/system/bin/switch-to-configuration switch >>"$LOG" 2>&1 || true

state=$(systemctl is-system-running 2>/dev/null || true)
echo "$(date -Is) state after rollback: $state" > "$ROLLBACK_MARKER"
log "rollback done, system now: $state"
say "fleet-rescue: lampadas MAGIC ROLLBACK done (still degraded -> rolled back a generation), now: $state"

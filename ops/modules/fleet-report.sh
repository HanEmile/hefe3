#!/usr/bin/env bash
# fleet-report - the 08:00 daily report into #emile, runs on lampadas.
#
# Composed from:
#   /var/lib/fleet/results.json        tonight's fleet-upgrade run
#   medano's status-board /api/summary.json (backups + kernels + reachability),
#   fetched through a temporary ssh tunnel (status-board is localhost-only).

set -uo pipefail

STATE=/var/lib/fleet
RESULTS=$STATE/results.json

# IRC via ssh tunnel to Ergo's localhost plaintext (medano hairpin/public
# ircs timing out for fresh lampadas connections since 2026-11-01 - same
# reason the status-board fetch below is tunneled).
_IRC_TUNNEL=""
_ipt_say() {
  [ -n "$_IRC_TUNNEL" ] || {
    ssh -o BatchMode=yes -o ConnectTimeout=10 -N -L 16667:192.168.75.23:6667 medano-root >/dev/null 2>&1 &
    _IRC_TUNNEL=$!
    sleep 3
  }
  local _try
  for _try in 1 2 3; do
    "$FLEET_SYSWATCH" --server 127.0.0.1:16667 --channel '#emile' --say "$@" && return 0
    sleep 10
  done
  return 0
}

say() { _ipt_say "$@"; }

# ------------------------------------------------ upgrade results (local) --

if [ -f "$RESULTS" ]; then
  PIN=$(jq -r '.pin // "unknown"' "$RESULTS")
  FIN=$(jq -r '.finished_at // "?"' "$RESULTS")
  TOTAL=$(jq '.hosts | length' "$RESULTS")
  DEP_OK=$(jq '[.hosts[] | select(.deployed=="ok")] | length' "$RESULTS")
  DEP_FAIL=$(jq -r '[.hosts[] | select(.deployed=="fail") | .name] | join(" ")' "$RESULTS")
  REB_FAIL=$(jq -r '[.hosts[] | select(.rebooted=="fail" or .back_up=="fail") | .name] | join(" ")' "$RESULTS")
  say "fleet report $(date +%F): nightly upgrade -> $DEP_OK/$TOTAL deployed ok (pin $PIN, finished $FIN)"
  [ -n "$DEP_FAIL" ] && say "  deploy FAILED: $DEP_FAIL"
  [ -n "$REB_FAIL" ] && say "  reboot/unlock FAILED: $REB_FAIL"
  if jq -e '.lampadas_self.rebooted == "yes"' "$RESULTS" >/dev/null 2>&1; then
    say "  lampadas rebooted itself at $(jq -r .lampadas_self.at "$RESULTS") (and is obviously back - you're reading this)"
  fi
else
  say "fleet report $(date +%F): no upgrade results found - did the 02:00 run fail to start?"
fi

# ------------------------------------------- status-board summary (remote) --

TUNNEL_PID=""

# status-board binds the bridge address only
if ssh -o ConnectTimeout=10 -o BatchMode=yes -N -L 18099:192.168.75.1:8090 medano-root 2>/dev/null &
then
  TUNNEL_PID=$!
  sleep 3
  if SUMMARY=$(curl -fs --max-time 20 http://127.0.0.1:18099/api/summary.json 2>/dev/null); then
    STALE=$(echo "$SUMMARY" | jq -r '(.stale_repos // []) | join(" ")')
    DOWN=$(echo "$SUMMARY" | jq -r '(.vms_down // []) | join(" ")')
    BOK=$(echo "$SUMMARY" | jq '.backups.OK')
    BBAD=$(echo "$SUMMARY" | jq '.backups.Bad + .backups.Warn')
    say "backups: $BOK repos fresh$( [ "$BBAD" != "0" ] && echo ", $BBAD stale/failing: $STALE" )"
    [ -n "$DOWN" ] && say "unreachable right now: $DOWN"

    # kernel freshness: only report the ones that matter
    BADK=$(echo "$SUMMARY" | jq -r '[.kernels[] | select(.level=="bad")] | length')
    WARNK=$(echo "$SUMMARY" | jq -r '[.kernels[] | select(.level=="warn")] | length')
    if [ "$BADK" != "0" ]; then
      LIST=$(echo "$SUMMARY" | jq -r '[.kernels[] | select(.level=="bad") | .name + "(" + .kernel + (if .eol then ",EOL" else "" end) + ")"] | join(" ")')
      say "kernels: $BADK stale/EOL: $LIST"
    elif [ "$WARNK" != "0" ]; then
      say "kernels: all healthy, $WARNK slightly behind (details: status.emile.space)"
    else
      say "kernels: all $(
        echo "$SUMMARY" | jq '.kernels | length') systems current"
    fi
  else
    say "status-board unreachable - no backups/kernel section today"
  fi
  kill "$TUNNEL_PID" 2>/dev/null
  wait "$TUNNEL_PID" 2>/dev/null
fi
[ -n "$_IRC_TUNNEL" ] && kill "$_IRC_TUNNEL" 2>/dev/null

exit 0

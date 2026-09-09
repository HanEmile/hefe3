# Tailscale connectivity watchdog for lampadas.
#
# Why this exists: lampadas sits in a flat behind a flaky consumer
# internet link (and previously a TX-timeout-storming r8168 NIC, see
# default.nix). tailscaled itself does NOT crash - it stays `Running` -
# but on a "major link change" / gateway+IP change it can drop its
# connection to the control plane and DERP and not always re-establish
# promptly. Symptoms seen in the journal:
#   health(warnable=not-in-map-poll): error: Unable to connect to the
#     Tailscale coordination server ...
#   health(warnable=no-derp-connection) / no-derp-home
#   health(warnable=login-state): error: You are logged out. ...
#     failed to resolve "controlplane.tailscale.com": no DNS fallback ...
# When that happens the node falls off the tailnet until the next
# favourable link event, which is exactly the "tailscale disconnecting
# all the time" the box exhibits.
#
# This is therefore a *reachability* watchdog, not a "restart the daemon"
# loop (the daemon is fine). Every RUN it:
#   1. Probes real tailnet reachability (backend state + ping a known
#      always-on peer over the tailnet, with a control-plane fallback).
#   2. If healthy: records a heartbeat, logs at most one terse OK line
#      per OK_LOG_INTERVAL so the journal does not fill with noise.
#   3. If unhealthy: dumps full diagnostics (backend state, health
#      warnings, netcheck, default route / link state) to the journal
#      and to a rolling debug log, then escalates recovery gently:
#        a. `tailscale up` with the configured flags (re-login / kick the
#           control connection without dropping the daemon),
#        b. only if STILL unreachable after a grace period, restart
#           tailscaled as a last resort.
#
# Pure bash + tailscale CLI: lampadas has neither jq nor python3, and we
# do not want to add a rebuild dependency just for a watchdog.
#
# Two-layer function to match the readTree + module-system calling
# convention used by homeassistant.nix (imported as
# `(import ./tailscale-watchdog.nix (args1 // args2))`).
{ hefe, pkgs, lib, ... }:

{ config, ... }:

let
  # Peer we expect to always be up on the tailnet. medano is the
  # hypervisor and is effectively never offline; pinging its tailnet IP
  # is a far stronger liveness signal than "tailscaled says Running".
  # medano is not in the "tailscale" IPAM namespace (only data/lampadas/rss
  # are), so look it up defensively and fall back to its known tailnet IP.
  peerIP = (hefe.ops.ipam."tailscale".medano or { v4 = "100.97.112.77"; }).v4;

  # Mirror the flags from services.tailscale.extraUpFlags so a recovery
  # `tailscale up` does not silently change prefs (advertise exit node,
  # ssh). Kept as one string on purpose - matches how the daemon was
  # brought up.
  upFlags = "--ssh --advertise-exit-node";

  # How often the watchdog runs.
  checkInterval = "2min";
  # Don't spam the journal: only emit an OK line this often when healthy.
  okLogInterval = 3600; # seconds
  # After a `tailscale up` kick, wait this long before deciding it failed
  # and escalating to a daemon restart.
  recoverGrace = 20; # seconds

  debugLog = "/var/log/tailscale-watchdog.log";
  heartbeat = "/run/tailscale-watchdog/last-ok";

  watchdog = pkgs.writeShellScript "tailscale-watchdog" ''
    set +e
    export PATH=${
      lib.makeBinPath [
        pkgs.tailscale
        pkgs.iproute2
        pkgs.coreutils
        pkgs.gnugrep
        pkgs.systemd
      ]
    }:$PATH

    ts=tailscale
    HEARTBEAT=${heartbeat}
    DBG=${debugLog}
    PEER=${peerIP}

    # log <msg>: to journal (stderr -> systemd) AND to the rolling debug log.
    log() {
      local line
      line="$(date -Is) $*"
      echo "$line"            # -> journal via systemd stdout
      echo "$line" >> "$DBG"  # -> persistent rolling log
    }

    # dump_diag: everything useful for figuring out *why* it's down.
    dump_diag() {
      log "DIAG ----------------------------------------------------------"
      log "DIAG backend-state: $($ts status --json 2>/dev/null | grep -m1 BackendState | tr -d ' ,"')"
      # Health warnings are the most direct signal (login-state,
      # not-in-map-poll, no-derp-*). One per line.
      $ts status 2>&1 | grep -iE 'health|warning|logged out|offline' | while read -r l; do
        log "DIAG health: $l"
      done
      log "DIAG default-route: $(ip route show default 2>&1 | tr '\n' ';')"
      log "DIAG tailscale0-addr: $(ip -brief addr show tailscale0 2>&1 | tr '\n' ';')"
      # netcheck is slow-ish (~5s) but invaluable: tells us if UDP / DERP
      # / the upstream link itself is the problem vs. tailscaled.
      $ts netcheck 2>&1 | grep -iE 'UDP|IPv4|IPv6|DERP latency|Nearest|MappingVaries|portmap' | while read -r l; do
        log "DIAG netcheck: $l"
      done
      log "DIAG ----------------------------------------------------------"
    }

    # reachable: 0 = tailnet is genuinely usable, 1 = not.
    # Backend must be Running AND we must be able to actually reach a peer.
    # A peer ping (DERP or direct) cuts through the "Running but silently
    # partitioned" state the link flaps produce.
    reachable() {
      local state
      state="$($ts status --json 2>/dev/null | grep -m1 BackendState | tr -d ' ,"')"
      case "$state" in
        *Running*) ;;
        *) return 1 ;;
      esac
      if $ts ping --timeout=5s --c 1 "$PEER" >/dev/null 2>&1; then
        return 0
      fi
      # The peer ping failed. Before declaring ourselves disconnected,
      # fall back to the control-plane signal: if tailscaled is still in
      # the map poll (no "not-in-map-poll" warning) and logged in, the
      # tailnet is up and the peer is simply down itself.
      if ! $ts status 2>&1 | grep -qiE "not-in-map-poll|logged out|NeedsLogin"; then
        return 0
      fi
      return 1
    }

    mkdir -p "$(dirname "$HEARTBEAT")" 2>/dev/null

    if reachable; then
      now=$(date +%s)
      last=0
      [ -r "$HEARTBEAT" ] && last=$(cat "$HEARTBEAT" 2>/dev/null || echo 0)
      echo "$now" > "$HEARTBEAT"
      # Throttle the OK chatter.
      if [ $(( now - last )) -ge ${toString okLogInterval} ]; then
        log "OK tailscale reachable (peer $PEER)"
      fi
      exit 0
    fi

    # --- Unhealthy from here on ---
    log "DOWN tailscale not reachable - diagnosing + attempting recovery"
    dump_diag

    # Step 1: gentle kick. `tailscale up` re-runs the login / control
    # handshake and re-applies prefs without tearing down the daemon or
    # the tun device. This alone fixes the common "logged out after DNS
    # blip / not-in-map-poll" case.
    log "RECOVER step1: tailscale up ${upFlags}"
    $ts up ${upFlags} >>"$DBG" 2>&1
    log "RECOVER step1: tailscale up exit=$?"

    sleep ${toString recoverGrace}

    if reachable; then
      log "RECOVER ok after 'tailscale up'"
      date +%s > "$HEARTBEAT"
      exit 0
    fi

    # Step 2: last resort. Bounce the daemon. Cheap, and on a stuck
    # magicsock / DERP state this is what finally clears it. Don't loop -
    # the timer will run us again in ${checkInterval}.
    log "RECOVER step2: still down, restarting tailscaled.service"
    systemctl restart tailscaled.service >>"$DBG" 2>&1
    log "RECOVER step2: restart exit=$?"

    # Give it a moment, then re-assert prefs (restart can come back
    # WantRunning but pre-login on a slow link).
    sleep ${toString recoverGrace}
    $ts up ${upFlags} >>"$DBG" 2>&1

    if reachable; then
      log "RECOVER ok after tailscaled restart"
      date +%s > "$HEARTBEAT"
    else
      log "RECOVER FAILED - still down after restart (likely upstream link down). Will retry in ${checkInterval}."
    fi
    exit 0
  '';
in
{
  systemd.services."tailscale-watchdog" = {
    description = "Probe tailscale reachability; diagnose + recover on failure (flaky-link watchdog)";
    after = [ "tailscaled.service" "network-online.target" ];
    wants = [ "tailscaled.service" ];
    serviceConfig = {
      Type = "oneshot";
      ExecStart = watchdog;
      # Keep the rolling debug log directory provisioned.
      LogsDirectory = "tailscale-watchdog";
    };
  };

  # Rotate the persistent debug log so it can't fill /var.
  services.logrotate.settings."tailscale-watchdog" = {
    files = debugLog;
    frequency = "weekly";
    rotate = 4;
    compress = true;
    missingok = true;
    notifempty = true;
    copytruncate = true;
  };

  systemd.timers."tailscale-watchdog" = {
    description = "Run the tailscale reachability watchdog every ${checkInterval}";
    wantedBy = [ "timers.target" ];
    timerConfig = {
      # First check shortly after boot, then on a steady cadence.
      OnBootSec = "90s";
      OnUnitActiveSec = checkInterval;
      # Jitter so a flaky link doesn't always get probed at the same
      # instant a backup / scrub fires (same rationale as the restic
      # RandomizedDelaySec in default.nix).
      RandomizedDelaySec = "20s";
      Persistent = true;
    };
  };
}

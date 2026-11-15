# Self-healing activation watchdog - deploy-rs style "magic rollback".
#
#   every 2min  -> systemctl is-system-running
#   running     -> clear marker, exit
#   degraded    -> 1. re-activate the current profile (completes an
#                     interrupted switch; restarts wanted-but-stopped units)
#                  2. still degraded -> roll the system profile back one
#                     generation and activate THAT (once; marker-guarded)
#   starting / shutdown / in-flight switch -> leave alone
#
# Born on lampadas (2026-11-15: fleet-self-upgrade SIGTERM'd by its own
# switch mid-teardown, 7h of everything-stopped). Generalized fleet-wide
# after the same review: any host can enter "degraded but reachable" and
# nothing would otherwise recover or notice.
#
# Import pattern: (import ../../../modules/magic-rollback.nix { inherit hefe pkgs; })
{ hefe, pkgs }:
{ config, lib, ... }:

let
  cfg = config.magicRollback;
  syswatch = pkgs.callPackage hefe.ops.pkgs.syswatch { };

  script = pkgs.writeShellApplication {
    name = "self-rescue";
    runtimeInputs = with pkgs; [ nix coreutils gnugrep gawk procps ];
    text = ''
      STATE=/var/lib/self-rescue
      ROLLBACK_MARKER=$STATE/rollback.done
      LOG=$STATE/rescue.log
      mkdir -p "$STATE"

      log() { echo "$(date -Is) $*" | tee -a "$LOG"; }
      say() { "${syswatch}/bin/syswatch --tls --server irc.emile.space:6697 --channel '#emile' --say "$(hostname -s): $1" || true; }

      # ---------------------------- leave-alone gates --------------------------
      state=$(systemctl is-system-running 2>/dev/null || true)
      [ "$state" = running ] && { rm -f "$ROLLBACK_MARKER"; exit 0; }
      [ "$state" = degraded ] || exit 0

      pgrep -f 'switch-to-configuration' >/dev/null 2>&1 && exit 0
      if systemctl is-active --quiet shutdown.target || systemctl is-active --quiet reboot.target; then
        exit 0
      fi

      # -------------------------------- heal -----------------------------------
      FAILED_UNITS=$(systemctl --failed --no-legend --plain 2>/dev/null | awk '{print $1}' | tr '\n' ' ')
      log "DEGRADED - completing interrupted activation (failed: ''${FAILED_UNITS:-none})"
      say "self-rescue: DEGRADED (''${FAILED_UNITS:-?}) - re-activating current profile"

      /run/current-system/bin/switch-to-configuration switch >>"$LOG" 2>&1 || true

      state=$(systemctl is-system-running 2>/dev/null || true)
      if [ "$state" = running ]; then
        log "healed after re-activation"
        say "self-rescue: healed by re-activation"
        rm -f "$ROLLBACK_MARKER"
        exit 0
      fi

      # ---------------------------- magic rollback -----------------------------
      if [ -f "$ROLLBACK_MARKER" ]; then
        log "STILL DEGRADED after re-activation, but rollback already done once ($(cat "$ROLLBACK_MARKER")) - not looping"
        exit 0
      fi

      log "still degraded after re-activation - MAGIC ROLLBACK to previous generation"
      nix-env -p /nix/var/nix/profiles/system --rollback >>"$LOG" 2>&1 || { log "nix-env --rollback FAILED"; exit 0; }
      /nix/var/nix/profiles/system/bin/switch-to-configuration switch >>"$LOG" 2>&1 || true

      state=$(systemctl is-system-running 2>/dev/null || true)
      echo "$(date -Is) state after rollback: $state" >"$ROLLBACK_MARKER"
      log "rollback done, system now: $state"
      say "self-rescue: MAGIC ROLLBACK done, now: $state"
    '';
  };
in
{
  options.magicRollback.enable = lib.mkEnableOption "self-healing activation watchdog (magic rollback)";

  config = lib.mkIf cfg.enable {
    systemd.services.self-rescue = {
      description = "heal a degraded system: re-activate, roll back a generation if needed";
      # Load-bearing (2026-11-15): the rescue can itself run
      # switch-to-configuration while its own unit file changed.
      stopIfChanged = false;
      restartIfChanged = false;
      serviceConfig = {
        Type = "oneshot";
        StateDirectory = "self-rescue";
        TimeoutStartSec = "15min";
      };
      script = "exec ${script}/bin/self-rescue";
    };

    systemd.timers.self-rescue = {
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnCalendar = "*:0/2:00";
        Persistent = true;
      };
    };
  };
}

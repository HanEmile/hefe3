# Boot/switch canary with automatic rollback.
#
#   every boot + 2min  -> run checks (sshd up + :22 accepting + no failed units)
#   checks pass        -> done until next boot
#   checks fail        -> switch-to-configuration the PREVIOUS generation and
#                         reboot; stamp /var/lib/generation-canary/done-* so we
#                         never boot-loop on the same failure pair twice
#
# Why in-guest: the failure we protect against is precisely the one where the
# machine becomes unreachable (fleet-upgrade 2026-11: initrd unlock worked,
# but a generation that wedges sshd would otherwise strand the box until
# manual rescue). No outside caller can roll back a machine it can't reach.
{ lib, pkgs, config, ... }:

let
  cfg = config.generationCanary;
in
{
  options.generationCanary = {
    enable = lib.mkEnableOption "generation canary with automatic rollback";

    checkFailedUnits = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Also roll back when any systemd unit is failed after boot.";
    };

    extraChecks = lib.mkOption {
      type = lib.types.lines;
      default = "";
      description = "Additional shell commands; non-zero exit counts as a canary failure.";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.generation-canary = {
      description = "verify the new generation stays reachable; roll back if not";
      wantedBy = [ "multi-user.target" ];
      after = [ "multi-user.target" ];
      path = with pkgs; [ netcat-gnu coreutils gnugrep gnused ];
      serviceConfig = {
        Type = "oneshot";
        TimeoutStartSec = "20min";
      };
      script = ''
        set -u
        STAMP=/var/lib/generation-canary/booted-flagged
        mkdir -p /var/lib/generation-canary

        # settle: boot storm before first real judgement call
        sleep 120

        fails=
        if ! systemctl is-active --quiet sshd.service; then
          fails="$fails sshd-inactive"
        fi
        if ! nc -z -w3 127.0.0.1 22; then
          fails="$fails port22-not-accepting"
        fi
        # Pretix 2026-11-01: unit active, port open, yet sshd children died
        # before emitting the banner - a port check alone misses this class.
        banner=$(timeout 5 bash -c 'exec 3<>/dev/tcp/127.0.0.1/22 && head -c 4 <&3' 2>/dev/null || true)
        if [ "$banner" != "SSH-" ]; then
          fails="$fails no-ssh-banner"
        fi
        ${lib.optionalString cfg.checkFailedUnits ''
        if [ -n "$(systemctl list-units --failed --no-legend --no-pager | head -1)" ]; then
          fails="$fails failed-units:$(systemctl list-units --failed --no-legend --no-pager | tr -s ' ' | cut -d' ' -f2 | tr '\n' ',')"
        fi
        ''}
        ${cfg.extraChecks}

        if [ -z "$fails" ]; then
          echo "generation-canary: OK"
          exit 0
        fi

        cur=$(readlink /run/current-system)
        curnum=$(basename "$cur" | sed -E 's/^system-([0-9]+)-link$/\1/')
        prev=""
        best=-1
        for gen in /nix/var/nix/profiles/system-*-link; do
          num=$(basename "$gen" | sed -E 's/^system-([0-9]+)-link$/\1/')
          if [ "$num" -lt "$curnum" ] && [ "$num" -gt "$best" ]; then
            best=$num; prev=$gen
          fi
        done

        if [ -z "$prev" ]; then
          echo "generation-canary: FAILED ($fails) but no previous generation exists - staying up"
          exit 0
        fi
        stamp="done-$(basename "$cur")-to-$(basename "$prev")"
        if [ -e "/var/lib/generation-canary/$stamp" ]; then
          echo "generation-canary: FAILED ($fails) and rollback $prev was already attempted once - refusing to loop"
          exit 0
        fi

        echo "generation-canary: FAILED ($fails) - rolling back: $cur -> $prev"
        touch "/var/lib/generation-canary/$stamp"
        "$prev/bin/switch-to-configuration" switch || {
          echo "rollback switch failed - staying up (manual review needed)"
          exit 0
        }
        # a reboot is the cleanest way out of any residual weirdness
        systemctl reboot
      '';
    };
  };
}

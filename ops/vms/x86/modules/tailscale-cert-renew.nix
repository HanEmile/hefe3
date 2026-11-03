# Auto-renewing tailscale-issued TLS certs.
#
# Usage:
#   imports = [ ../modules/tailscale-cert-renew.nix ];
#   tailscaleCertRenew.hostnames = [ "arr.pinto-pike.ts.net" ];
#
# Effect:
#   For each hostname, certs land at
#     /var/lib/tailscale-certs/<host>.crt
#     /var/lib/tailscale-certs/<host>.key
#   chmod 0640 root:nginx so the standard nginx user can read them.
#   A systemd timer runs once on boot and weekly afterwards, calling
#   `tailscale cert` and reloading nginx if reload-on-renew is set
#   (default true).
#
#   The first invocation is also chained as a `Wants` of nginx.service
#   so the cert exists by the time nginx starts (cold-boot case).
#
# Why timer-based: tailscale-issued certs use Let's Encrypt under the
# hood and last 90 days; refreshing weekly leaves plenty of headroom and
# tailscale's own logic short-circuits when the existing cert is still
# fresh, so the timer is essentially free.

{ config, lib, pkgs, ... }:

let
  cfg = config.tailscaleCertRenew;
  certDir = "/var/lib/tailscale-certs";
in
{
  options.tailscaleCertRenew = {
    hostnames = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [];
      description = "MagicDNS hostnames to obtain TLS certs for.";
    };
    reloadNginx = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Reload nginx after each successful renewal.";
    };
  };

  config = lib.mkIf (cfg.hostnames != []) {
    systemd.tmpfiles.rules = [
      "d ${certDir} 0750 root nginx -"
    ];

    systemd.services."tailscale-cert-renew" = {
      description = "Renew tailscale-issued TLS certs";
      after = [ "tailscaled.service" "network-online.target" ];
      wants = [ "tailscaled.service" "network-online.target" ];
      # Run before nginx so the cert files exist on first boot.
      before = lib.optional cfg.reloadNginx "nginx.service";
      wantedBy = lib.optional cfg.reloadNginx "nginx.service";

      path = [ pkgs.tailscale pkgs.coreutils ];
      script = ''
        set -eu
        failed=0
        mkdir -p ${certDir}
        chmod 0750 ${certDir}
        ${lib.concatMapStringsSep "\n" (host: ''
          echo "Renewing ${host}..."
          # tailscaled needs a moment after (re)start to fetch its netmap
          # (fails fast with "no netmap available"); retry before giving up.
          attempt=0
          rc=1
          while [ $attempt -lt 12 ]; do
            if ${pkgs.tailscale}/bin/tailscale cert \
                --cert-file ${certDir}/${host}.crt \
                --key-file  ${certDir}/${host}.key \
                ${host}; then
              rc=0
              break
            fi
            attempt=$((attempt + 1))
            sleep 10
          done
          if [ $rc -ne 0 ]; then
            echo "Giving up on ${host} after 12 attempts" >&2
            failed=1
          else
            chown root:nginx ${certDir}/${host}.crt ${certDir}/${host}.key
            chmod 0640      ${certDir}/${host}.crt ${certDir}/${host}.key
          fi
        '') cfg.hostnames}
        ${lib.optionalString cfg.reloadNginx ''
          # Best-effort reload (skipped on cold boot when nginx isn't up yet).
          # --no-block: when this oneshot is part of an nginx (re)start
          # transaction (Before=nginx.service), a blocking reload queues
          # behind that transaction and deadlocks with it.
          if systemctl is-active --quiet nginx.service; then
            systemctl reload --no-block nginx.service || true
          fi
        ''}
        exit "$failed"
      '';
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = false;
      };
    };

    systemd.timers."tailscale-cert-renew" = {
      description = "Weekly tailscale TLS cert renewal";
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnBootSec = "5min";
        OnUnitActiveSec = "7d";
        Persistent = true;
      };
    };
  };
}

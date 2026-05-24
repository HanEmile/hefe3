{
  hefe,
  pkgs,
  ...
}:
{ config, ... }:

let
  ipam = hefe.ops.ipam.tailscale.rss;
  baseUrl = "https://rss.pinto-pike.ts.net";
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
    (import ../modules/backups.nix { inherit hefe; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "rss";
  system.stateVersion = "25.05";

  # miniflux runs with DynamicUser, so the static "miniflux" user doesn't
  # exist; we make the secret files world-readable (they live in tmpfs
  # /run/agenix anyway).
  age.secrets = {
    miniflux_admin_creds = {
      file = hefe.ops.secrets."miniflux_admin_creds.age";
      mode = "0444";
    };
    miniflux_oidc_client_secret = {
      file = hefe.ops.secrets."miniflux_oidc_client_secret.age";
      mode = "0444";
    };
  };

  # miniflux binds on the tailscale IP, so it must wait for tailscaled to
  # bring the interface up and assign 100.x. Hooked via after/wants.
  systemd.services.miniflux = {
    after = [ "tailscaled.service" "network-online.target" ];
    wants = [ "tailscaled.service" "network-online.target" ];
  };

  services.miniflux = {
    enable = true;
    package = pkgs.miniflux;
    config = {
      LISTEN_ADDR = "127.0.0.1:${toString ipam.ports.miniflux}";
      BASE_URL = baseUrl;
      CLEANUP_FREQUENCY = 48;

      # OIDC via authelia
      OAUTH2_PROVIDER = "oidc";
      OAUTH2_CLIENT_ID = "miniflux";
      OAUTH2_CLIENT_SECRET_FILE = config.age.secrets.miniflux_oidc_client_secret.path;
      OAUTH2_REDIRECT_URL = "${baseUrl}/oauth2/oidc/callback";
      OAUTH2_OIDC_DISCOVERY_ENDPOINT = "https://sso.emile.space";
      OAUTH2_USER_CREATION = "1";
    };
    createDatabaseLocally = true;
    adminCredentialsFile = config.age.secrets.miniflux_admin_creds.path;
  };

  # Backups
  vmBackups = {
    paths = [ "/var/lib/postgresql" ];
    # miniflux state lives in postgres
    backupPrepareCommand = ''
      ${pkgs.sudo}/bin/sudo -u postgres ${pkgs.postgresql}/bin/pg_dump -Fc miniflux > /var/lib/postgresql/miniflux-restic.dump || true
    '';
  };

  # tailscale serve: have tailscaled terminate TLS on
  # https://rss.pinto-pike.ts.net and reverse-proxy to miniflux's local
  # listener. The daemon handles cert provisioning and rotation, so there
  # are no .age files to maintain.
  systemd.services.tailscale-serve-rss = {
    description = "Configure tailscale serve for rss.pinto-pike.ts.net";
    after = [ "tailscaled.service" "network-online.target" "miniflux.service" ];
    wants = [ "tailscaled.service" "network-online.target" ];
    wantedBy = [ "multi-user.target" ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
      ExecStart = pkgs.writeShellScript "tailscale-serve-rss" ''
        set -eu
        ${pkgs.tailscale}/bin/tailscale serve reset || true
        ${pkgs.tailscale}/bin/tailscale serve --bg --https=443 \
          http://127.0.0.1:${toString ipam.ports.miniflux}
      '';
      ExecStop = "${pkgs.tailscale}/bin/tailscale serve reset";
    };
  };

  # Healthcheck - miniflux's /healthcheck returns 200 when DB is reachable.
  services.healthProbes.probes = [
    { name = "self"; url = "http://127.0.0.1:${toString ipam.ports.miniflux}/healthcheck"; }
  ];
}

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
      LISTEN_ADDR = "${ipam.v4}:${toString ipam.ports.miniflux}";
      BASE_URL = baseUrl;
      CLEANUP_FREQUENCY = 48;

      # OIDC via authelia
      OAUTH2_PROVIDER = "oidc";
      OAUTH2_CLIENT_ID = "miniflux";
      OAUTH2_CLIENT_SECRET_FILE = config.age.secrets.miniflux_oidc_client_secret.path;
      OAUTH2_REDIRECT_URL = "${baseUrl}/oauth2/oidc/callback";
      OAUTH2_OIDC_DISCOVERY_ENDPOINT = "https://auth.medano.emile.space";
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

  # Healthcheck — miniflux's /healthcheck returns 200 when DB is reachable.
  services.healthProbes.probes = [
    { name = "self"; url = "http://${ipam.v4}:${toString ipam.ports.miniflux}/healthcheck"; }
  ];
}

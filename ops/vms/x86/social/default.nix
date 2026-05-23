{ hefe, ... }:
{ config, pkgs, ... }:

let
  ipam = hefe.ops.ipam.default.social;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
    (import ../modules/backups.nix { inherit hefe; })
  ];

  networking.hostName = "social";
  system.stateVersion = "25.05";

  networking.firewall.interfaces."enp1s0".allowedTCPPorts = [ 3004 ];

  age.secrets.gotosocial_environment_file = {
    file = hefe.ops.secrets."gotosocial_environment_file.age";
    owner = "gotosocial";
    group = "gotosocial";
  };

  # match corrino uid/gid mapping for the existing data dir
  users.users.gotosocial = {
    isSystemUser = true;
    group = "gotosocial";
    home = "/var/lib/gotosocial";
  };
  users.groups.gotosocial = {};

  services.gotosocial = {
    enable = true;
    setupPostgresqlDB = false;
    environmentFile = config.age.secrets.gotosocial_environment_file.path;
    settings = {
      application-name = "gotosocial";
      account-domain = "emile.space";
      host = "social.emile.space";
      protocol = "https";
      bind-address = ipam.v4;
      port = 3004;
      db-type = "sqlite";
      db-address = "/var/lib/gotosocial/database.sqlite";
      storage-local-base-path = "/var/lib/gotosocial/storage";

      accounts-allow-custom-css = true;
      advanced-rate-limit-requests = 0;

      oidc-enabled = true;
      oidc-idp-name = "authelia";
      oidc-issuer = "https://auth.medano.emile.space";
      oidc-client-id = "gotosocial";
      oidc-link-existing = true;
    };
  };

  vmBackups = {
    paths = [
      "/var/lib/gotosocial/database.sqlite"
      "/var/lib/gotosocial/storage"
    ];
    # the sqlite database needs to be quiesced before restic snapshots it.
    # Use sqlite3 .backup to write a consistent dump to a sibling file just
    # before restic runs.
    backupPrepareCommand = ''
      ${pkgs.sqlite}/bin/sqlite3 /var/lib/gotosocial/database.sqlite \
        ".backup '/var/lib/gotosocial/database.sqlite.restic-snapshot'" || true
    '';
  };

  services.healthProbes.probes = [
    { name = "self";   url = "http://${ipam.v4}:3004/"; expectedStatus = 200; }
    { name = "public"; url = "https://social.emile.space/api/v1/instance"; }
  ];
}

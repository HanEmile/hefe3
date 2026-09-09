{
  hefe,
  pkgs,
  ...
}:
{ config, ... }:

let
  ipam = hefe.ops.ipam.default.pretix;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    (import ../modules/backups.nix { inherit hefe; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "pretix";
  networking.firewall.allowedTCPPorts = [
    ipam.ports.pretix
  ];

  system.stateVersion = "25.05";

  age.secrets = {
    pretix_environment_file = {
      file = hefe.ops.secrets."pretix_environment_file.age";
      owner = "pretix";
      group = "pretix";
    };
  };

  services.pretix = {
    enable = true;

    nginx = {
      enable = true;
      domain = "tickets.emile.space";
    };

    environmentFile = config.age.secrets.pretix_environment_file.path;

    settings = {
      pretix = {
        instance_name = "tickets.emile.space";
        url = "https://tickets.emile.space";
        currency = "EUR";
        registration = false;
      };

      database = {
        backend = "postgresql";
      };

      mail = {
        from = "tickets@emile.space";
        host = "mail.emile.space";
        port = 587;
      };
    };
  };

  services.nginx = {
    virtualHosts."tickets.emile.space" = {
      listen = [
        { addr = ipam.v4; port = ipam.ports.pretix; }
      ];
    };
  };

  vmBackups.paths = [
    "/var/lib/pretix"
    "/var/lib/postgresql"
  ];

  services.healthProbes.probes = [
    { name = "self"; url = "https://tickets.emile.space/healthcheck/"; }
  ];
}

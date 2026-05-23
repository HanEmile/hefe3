{
  hefe,
  pkgs,
  ...
}:
{ config, ... }:

{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
  ];

  networking.hostName = "md";
  networking.firewall.allowedTCPPorts = [
    hefe.ops.ipam.default.md.ports.hedgedoc
  ];

  system.stateVersion = "25.05";

  age = {
    secrets = {
      hedgedoc_environment_variables = {
        file = hefe.ops.secrets."hedgedoc_environment_variables.age";
      };
    };
  };

  services.hedgedoc = {
    enable = true;
    package = pkgs.hedgedoc;

    environmentFile = config.age.secrets.hedgedoc_environment_variables.path;

    settings = {
      # host = "127.0.0.1";
      host = hefe.ops.ipam.default.md.v4;
      port = hefe.ops.ipam.default.md.ports.hedgedoc;

      domain = "md.medano.emile.space";

      urlPath = null; # we're hosting on the root of the subdomain and not a subpath
      allowGravatar = true;

      # we're terminating tls at the reverse proxy
      useSSL = false;

      # Use https:// for all links.
      # This is useful if you are trying to run hedgedoc behind a reverse proxy.
      # Only applied if domain is set.
      protocolUseSSL = true;

      # don't allow unauthenticated people to just write somewhere
      allowAnonymous = false;
      allowAnonymousEdits = true; # This allows us to set pads "freely"

      defaultPermission = "private";

      db = {
        dialect = "sqlite";
        storage = "/var/lib/hedgedoc/db.sqlite";
      };

      uploadsPath = "/var/lib/hedgedoc/uploads";

      path = null; # we want to use HTTP and not UNIX domain sockets...

      allowOrigin = with config.services.hedgedoc.settings; [
        host
        domain
      ];
    };
  };
}

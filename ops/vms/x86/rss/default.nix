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

  networking.hostName = "rss";
  system.stateVersion = "25.05";

  age.secrets = {
    miniflux_admin_creds = {
      file = hefe.ops.secrets."miniflux_admin_creds.age";
    };
  };

  services.miniflux = {
    enable = true;
    package = pkgs.miniflux;
    config = let
      tailscaleip = hefe.ops.ipam.tailscale.rss.v4;
      minifluxport = hefe.ops.ipam.tailscale.rss.ports.miniflux;
    in {
      LISTEN_ADDR = "${tailscaleip}:${minifluxport}";
      BASE_URL = "https://rss.pinto-pike.ts.net";

      # Cleanup job frequency to remove old sessions and archive entries
      CLEANUP_FREQUENCY = 48; # hours
    };
    createDatabaseLocally = true;
    adminCredentialsFile = config.age.secrets.miniflux_admin_creds.path;
  };
}

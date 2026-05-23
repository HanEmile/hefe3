{ hefe, pkgs, ... }:
{ config, ... }:

let
  ipam = hefe.ops.ipam.default.naraj;
in
{
  imports = [
   ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "naraj";
  system.stateVersion = "25.05";

  # Allow inbound HTTP from medano (host's nginx will eventually proxy to us).
  networking.firewall.interfaces."enp1s0".allowedTCPPorts = [ 80 ];

  systemd.tmpfiles.rules = [
    "d /var/www/emile.space 0755 nginx nginx - -"
  ];

  services.nginx = {
    enable = true;
    recommendedGzipSettings = true;
    recommendedOptimisation = true;
    recommendedProxySettings = true;
    recommendedTlsSettings = true;

    virtualHosts."naraj" = {
      default = true;
      listen = [ { addr = "${ipam.v4}"; port = 80; ssl = false; } ];
      locations."/" = {
        root = "/var/www/emile.space";
        index = "index.html";
        extraConfig = ''
          add_header Cache-Control "public, max-age=300";
        '';
      };
    };
  };

  services.healthProbes.probes = [
    { name = "self"; url = "http://${ipam.v4}/"; expectedStatus = 200; }
    { name = "node-exporter"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

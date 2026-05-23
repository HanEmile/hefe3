{ hefe, pkgs, ... }:
{ config, ... }:

let
  ipam = hefe.ops.ipam.default.tmp;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "tmp";
  system.stateVersion = "25.05";

  networking.firewall.interfaces."enp1s0".allowedTCPPorts = [ 80 ];

  systemd.tmpfiles.rules = [
    "d /var/www/tmp.emile.space 0755 nginx nginx - -"
  ];

  services.nginx = {
    enable = true;
    recommendedGzipSettings = true;
    recommendedOptimisation = true;
    recommendedTlsSettings = true;

    virtualHosts."tmp" = {
      default = true;
      listen = [ { addr = "${ipam.v4}"; port = 80; ssl = false; } ];
      locations."/" = {
        root = "/var/www/tmp.emile.space";
        extraConfig = ''
          autoindex on;
          add_header Cache-Control "public, max-age=60";
        '';
      };
    };
  };

  services.healthProbes.probes = [
    { name = "self"; url = "http://${ipam.v4}/"; expectedStatus = 200; }
    { name = "node-exporter"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

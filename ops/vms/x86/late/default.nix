{
  hefe,
  # pkgs,
  ...
}:
{ config, pkgs, ... }:

{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    (import ../modules/backups.nix { inherit hefe; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "late";
  networking.firewall.allowedTCPPorts = [ ];

  services.late-sh = {
    enable = true;
  };

  system.stateVersion = "25.05";

  vmBackups.paths = [
    "/var/lib/late-sh"
  ];

  services.healthProbes.probes = [
    { name = "self"; url = "http://127.0.0.1:8001/"; }
    { name = "node-exporter"; url = "http://${hefe.ops.ipam.default.late.v4}:9100/metrics"; }
  ];
}

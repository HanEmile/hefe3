{ hefe, pkgs, ... }:
{ config, ... }:

let
  ipam = hefe.ops.ipam.default.sb2;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "sb2";
  system.stateVersion = "25.05";

  # standby VM: idle, ready for ad-hoc use. node-exporter only.
  services.healthProbes.probes = [
    { name = "node-exporter"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

{ hefe, pkgs, ... }:
{ config, lib, ... }:

let
  ipam = hefe.ops.ipam.default.jules;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "jules";
  system.stateVersion = "25.05";

  services.healthProbes.probes = [
    { name = "node-exporter"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

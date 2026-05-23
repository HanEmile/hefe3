{ hefe, pkgs, ... }:
{ ... }:

{
  imports = [
   ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "naraj";
  system.stateVersion = "25.05";

  services.healthProbes.probes = [
    { name = "self"; url = "http://${hefe.ops.ipam.default.naraj.v4}:9100/metrics"; }
  ];
}

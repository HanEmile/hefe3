{
  hefe,
  pkgs,
  ...
}:
{ config, ... }:

{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "tmp";
  system.stateVersion = "25.05";

  services.healthProbes.probes = [
    { name = "self"; url = "http://${hefe.ops.ipam.default.tmp.v4}:9100/metrics"; }
  ];
}

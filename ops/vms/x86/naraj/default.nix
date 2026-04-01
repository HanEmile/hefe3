{ hefe, pkgs, ... }:
{ ... }:

{
  imports = [
   ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
  ];

  networking.hostName = "naraj";
  system.stateVersion = "25.05";
}

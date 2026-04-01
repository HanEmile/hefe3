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

  networking.hostName = "tmp";
  system.stateVersion = "25.05";
}

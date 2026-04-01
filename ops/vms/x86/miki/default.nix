{
  hefe,
  pkgs,
  ...
}:
{ ... }:

{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
  ];

  networking.hostName = "miki";

  system.stateVersion = "25.05";
}

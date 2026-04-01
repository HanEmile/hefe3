{
  hefe,
  pkgs,
  lib,
  ...
}@args1:

{ config, ... }@args2:

{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix {vmhost="medano";} { inherit hefe pkgs; })
    (import ./authelia.nix (args1 // args2))
  ];

  networking.hostName = "auth";

  system.stateVersion = "25.05";
}

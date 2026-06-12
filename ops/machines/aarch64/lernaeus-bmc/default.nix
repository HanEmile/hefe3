# lernaeus-bmc — Raspberry Pi 1 Model B+ v1.2 acting as BMC for lernaeus (GPU box).
{ hefe, pkgs, lib, ... }:
{ config, ... }:

{
  imports = [
    ../../../modules/bmc-base.nix
    ../../../modules/bmc-power.nix
    ./hardware-configuration.nix
  ];

  networking.hostName = "lernaeus-bmc";

  services.bmc-power = {
    enable = true;
    powerGpio = 17;
    resetGpio = 27;
  };

  system.stateVersion = "26.05";
}

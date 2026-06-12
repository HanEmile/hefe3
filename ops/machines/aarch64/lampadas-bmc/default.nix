# lampadas-bmc — Raspberry Pi 1 Model B+ v1.2 acting as BMC for lampadas (NAS).
#
# GPIO17 -> lampadas power button header
# GPIO27 -> lampadas reset button header
# Pi UART (GPIO14/15) -> lampadas COM1 serial header
{ hefe, pkgs, lib, ... }:
{ config, ... }:

{
  imports = [
    ../../../modules/bmc-base.nix
    ../../../modules/bmc-power.nix
    ./hardware-configuration.nix
  ];

  networking.hostName = "lampadas-bmc";

  services.bmc-power = {
    enable = true;
    powerGpio = 17;
    resetGpio = 27;
  };

  system.stateVersion = "26.05";
}

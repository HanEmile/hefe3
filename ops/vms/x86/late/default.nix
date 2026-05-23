{
  hefe,
  # pkgs,
  ...
}:
{ config, pkgs, ... }:

{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
  ];

  networking.hostName = "late";
  networking.firewall.allowedTCPPorts = [ ];

  services.late-sh = {
    enable = true;
  };

  system.stateVersion = "25.05";
}

{ hefe
, pkgs
, pkgs-unstable
, lib
, ...
}:
{ config, ... }:

{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost = "medano"; primaryBridge = "private"; } { inherit hefe pkgs; })

    (import ./private.nix { inherit hefe pkgs lib pkgs-unstable config; })
  ];

  # allow binding to tailscale ip
  boot.kernel.sysctl."net.ipv4.ip_nonlocal_bind" = 1;
  boot.kernelModules = [ "nfs" ];
  boot.supportedFilesystems = [ "nfs" ];

  networking.hostName = "arr";
  networking.nameservers = [ "192.168.33.2" ];

  networking.defaultGateway = {
    address = lib.mkForce hefe.ops.ipam.private.rou.v4;
    interface = "enp1s0";
  };

  system.stateVersion = "25.05";
}

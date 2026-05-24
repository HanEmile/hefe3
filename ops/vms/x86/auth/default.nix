{
  hefe,
  pkgs,
  lib,
  ...
}@args1:

{ config, ... }@args2:

let
  ipam = hefe.ops.ipam.default.auth;
in
{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix {vmhost="medano";} { inherit hefe pkgs; })
    (import ./authelia.nix (args1 // args2))
    (import ../modules/backups.nix { inherit hefe; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "auth";

  system.stateVersion = "25.05";

  vmBackups.paths = [
    "/var/lib/authelia-main"
  ];

  services.healthProbes.probes = [
    { name = "self"; url = "http://${ipam.v4}:${toString ipam.ports.authelia}/api/health"; }
    { name = "public"; url = "https://sso.emile.space/"; }
  ];
}

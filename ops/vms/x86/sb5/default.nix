{ hefe, pkgs, ... }:
{ config, ... }:

let
  ipam = hefe.ops.ipam.default.sb5;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "sb5";
  system.stateVersion = "25.05";

  # general-purpose sandbox: common build/runtime toolchain
  environment.systemPackages = with pkgs; [
    gcc
    gnumake
    go
    rustc
    cargo
    python3
    wget
    curl
    gdb
    strace
  ];

  security.sudo.extraRules = [{
    users = [ "emile" ];
    commands = [{ command = "ALL"; options = [ "NOPASSWD" ]; }];
  }];

  services.healthProbes.probes = [
    { name = "node-exporter"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

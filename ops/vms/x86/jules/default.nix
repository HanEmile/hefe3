{ hefe, pkgs, ... }:
{ config, lib, ... }:

let
  ipam = hefe.ops.ipam.default.jules;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "jules";
  system.stateVersion = "25.05";

  # Für Jules' Wohnungssuche-Dashboard
  environment.systemPackages = with pkgs; [ python3 uv ];
  programs.nix-ld.enable = true;            # damit der ImmoScout-Scraper (uv/Python-Pakete) läuft
  users.users.jules.linger = true;          # Jules' Dienste laufen dauerhaft, auch ohne Login
  services.tailscale.extraSetFlags = [ "--operator=jules" ];   # Jules darf "tailscale funnel" nutzen

  services.healthProbes.probes = [
    { name = "node-exporter"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

{ hefe, pkgs, ... }:
{ config, ... }:

let
  ipam = hefe.ops.ipam.default.r2wars;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
    (import ../modules/backups.nix { inherit hefe; })
  ];

  networking.hostName = "r2wars";
  system.stateVersion = "25.05";

  # r2wars is a radare2 project workspace. State lives in the VM's own qcow2
  # disk per design — not on /grave — so the workspace is self-contained.
  # Source + sqlite DBs land under /var/lib/r2wars after migration from corrino.
  systemd.tmpfiles.rules = [
    "d /var/lib/r2wars 0755 root root - -"
  ];

  # Useful tooling for actually doing radare2 work after ssh'ing in.
  environment.systemPackages = with pkgs; [
    radare2
    rizin
    go
    git
    tmux
  ];

  vmBackups.paths = [
    "/var/lib/r2wars"
  ];

  services.healthProbes.probes = [
    { name = "self"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

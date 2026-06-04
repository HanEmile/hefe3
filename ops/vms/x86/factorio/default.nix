{ hefe, ... }:
# Take pkgs from the module system (which sees nixpkgs.pkgs as the
# x86_64-linux instance from nixosFor) instead of from the outer
# readTree closure (which is the darwin pkgs).
{ config, pkgs, ... }:

let
  ipam = hefe.ops.ipam.default.factorio;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
    (import ../modules/backups.nix { inherit hefe; })
  ];

  networking.hostName = "factorio";
  system.stateVersion = "25.05";

  # Factorio state lives inside the VM's own disk under /var/lib/factorio
  # (via the upstream module's DynamicUser StateDirectory). No NFS - there
  # was no existing factorio data on corrino to migrate. Save files are
  # small; if you want them in /grave, periodically rsync /var/lib/factorio
  # to /grave/games/factorio from the host.

  networking.firewall = {
    allowedUDPPorts = [ ipam.ports.factorio ];
  };

  services.factorio = {
    enable = true;
    package = pkgs.factorio-headless;
    openFirewall = true;
    saveName = "default";
    autosave-interval = 10;
    requireUserVerification = false;
    public = false;
    lan = false;
    nonBlockingSaving = true;
    mods = [];
    description = "Emile's factorio server";
  };

  vmBackups.paths = [
    # Whole factorio state dir: saves, server-settings.json, mods, etc.
    # Earlier this was just the two json files; saves were assumed to be
    # rsync'd to /grave/games/factorio out-of-band, but that never
    # happened, so the saves had no backup at all.
    "/var/lib/factorio"
  ];

  services.healthProbes.probes = [
    { name = "self"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

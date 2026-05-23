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

  fileSystems."/var/lib/factorio" = {
    device = "192.168.75.1:/grave/games/factorio";
    fsType = "nfs";
    options = [
      "nolock"
      "_netdev"
      "nconnect=8"
    ];
  };

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
  };

  vmBackups.paths = [
    # Bookkeeping/config. Saves live on /grave/games/factorio (ZFS snapshotted).
    "/var/lib/factorio/server-settings.json"
    "/var/lib/factorio/server-adminlist.json"
  ];

  services.healthProbes.probes = [
    { name = "self"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

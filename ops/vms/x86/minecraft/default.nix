{ hefe, ... }:
# Take pkgs from the module system (which sees nixpkgs.pkgs as the
# x86_64-linux instance from nixosFor) instead of from the outer
# readTree closure (which is the darwin pkgs).
{ config, pkgs, ... }:

let
  ipam = hefe.ops.ipam.default.minecraft;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
    (import ../modules/backups.nix { inherit hefe; })
  ];

  networking.hostName = "minecraft";
  system.stateVersion = "25.05";

  # The minecraft world dir lives on the medano host (/grave/games/minecraft)
  # so it survives VM re-images and gets ZFS snapshotted alongside everything
  # else under /grave.
  fileSystems."/var/lib/minecraft" = {
    device = "192.168.75.1:/grave/games/minecraft";
    fsType = "nfs";
    options = [
      "nolock"
      "_netdev"
      "nconnect=8"
    ];
  };

  networking.firewall.allowedTCPPorts = [ ipam.ports.minecraft ];

  # Match the uid/gid from corrino (962:953) so the world files on
  # /grave/games/minecraft don't need a chown after migration.
  users.users.minecraft = {
    isSystemUser = true;
    uid = 962;
    group = "minecraft";
    home = "/var/lib/minecraft";
  };
  users.groups.minecraft.gid = 953;

  services.minecraft-server = {
    enable = true;
    eula = true;
    declarative = false;
    openFirewall = true;
    package = pkgs.minecraft-server;  # nixpkgs ships latest stable
    dataDir = "/var/lib/minecraft";
  };

  vmBackups.paths = [
    # Just bookkeeping config; the world itself is on /grave which is
    # snapshotted by medano's sanoid + restic.
    "/var/lib/minecraft/banned-ips.json"
    "/var/lib/minecraft/banned-players.json"
    "/var/lib/minecraft/ops.json"
    "/var/lib/minecraft/whitelist.json"
    "/var/lib/minecraft/server.properties"
  ];

  services.healthProbes.probes = [
    { name = "self"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

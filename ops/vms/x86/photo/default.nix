{
  hefe,
  pkgs,
  ...
}:
{ config, ... }:

{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    (import ../modules/backups.nix { inherit hefe; })
    ../modules/healthProbes.nix
  ];

  fileSystems."/data" = {
    device = "192.168.75.1:/grave/photos";
    fsType = "nfs";
    options = [
      "nolock"
      "_netdev"
      "nconnect=16"
      "noauto"            # Don't mount automatically at boot
      "x-systemd.automount" # Mount on first access instead
      "x-systemd.idle-timeout=600"
    ];
  };

  # pin the immich user uid so we can set it on the "outside" for access to the nfs share
  users.users.immich.isSystemUser = true;
  users.users.immich.uid = 994;
  users.groups.immich.gid = 992;

  age = {
    secrets = {
      photo_immich_secrets_file = {
        file = hefe.ops.secrets."photo_immich_secrets_file.age";
      };
    };
    identityPaths = [
      "/etc/ssh/ssh_host_ed25519_key"
      "/etc/ssh/ssh_host_rsa_key"
    ];
  };

  environment.systemPackages = with pkgs; [
    nfs-utils
    ripgrep
  ];

  # immich 2.7.5 (nixos-26.05) is marked insecure (CVE-2026-53662 Critical,
  # CVE-2026-59258, CVE-2026-105764 High - 2026-10-12 VEX scan), so take
  # 3.1.0 from the pinned nixos-unstable instead. Overrides services.immich
  # .package (default follows pkgs.immich); the module runs the package's
  # bundled machine-learning component, so server and ML stay in lockstep.
  nixpkgs.overlays = [
    (final: prev: {
      immich = (import hefe.third_party."nixos-unstable" {
        system = prev.stdenv.hostPlatform.system;
      }).immich;
    })
  ];

  services.immich = {
    enable = true;

    host = hefe.ops.ipam.default.photo.v4;
    port = hefe.ops.ipam.default.photo.ports.immich;

    secretsFile = config.age.secrets.photo_immich_secrets_file.path;
    accelerationDevices = null;
    mediaLocation = "/data/immich2";
  };

  networking = {
    hostName = "photo";
    firewall = {
      enable = true;
        interfaces."enp1s0" = {
        allowedTCPPorts = [ config.services.immich.port ];
      };
    };
  };

  system.stateVersion = "25.05";

  # Backup immich DB + config. NOT the photo library (that lives on the NFS
  # /grave/photos dataset which is backed up via medano's restic to storagebox).
  vmBackups = {
    paths = [
      "/var/lib/immich"
      "/var/lib/postgresql"
    ];
    excludePatterns = [
      "/var/lib/immich/thumbs"   # regenerable
      "/var/lib/immich/encoded-video"
    ];
  };

  services.healthProbes.probes = [
    { name = "self"; url = "http://${hefe.ops.ipam.default.photo.v4}:${toString hefe.ops.ipam.default.photo.ports.immich}/api/server/ping"; }
    { name = "public"; url = "https://photo.medano.emile.space/api/server/ping"; }
  ];
}

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
}

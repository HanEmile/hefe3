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
    device = "192.168.75.1:/grave/photo";
    fsType = "nfs";
    options = [
      "nolock"
      "netdev"
      "nconnect=16"
    ];
  };

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

  environment.systemPackages = with pkgs; [ nfs-utils ];

  services.immich = {
    enable = true;

    host = hefe.ops.ipam.default.photo.v4;
    port = hefe.ops.ipam.default.photo.ports.immich;

    secretsFile = config.age.secrets.photo_immich_secrets_file.path;
    accelerationDevices = null;
    mediaLocation = "/data/immich";
  };

  networking.hostName = "photo";
  system.stateVersion = "25.05";
}

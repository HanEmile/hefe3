{
  hefe,
  pkgs,
  lib,
  ...
}@args1:

{ config, ... }@args2:

{
  imports = [
    ./hardware-configuration.nix
    (import ../vm-base.nix {vmhost="medano";} { inherit hefe pkgs; })
    (import ./authelia.nix (args1 // args2))
  ];

  networking.hostName = "auth";

  system.stateVersion = "25.05";

  age.secrets = {
    storagebox_bx11_restic_password = {
      file = hefe.ops.secrets."storagebox_bx11_restic_password.age";
    };
    storagebox_bx11_connection_config = {
      file = hefe.ops.secrets."storagebox_bx11_connection_config.age";
    };
  };

  #fileSystems = {
  #  "/mnt/storagebox-bx11" = {
  #    device = "//u331921.your-storagebox.de/backup";
  #    fsType = "cifs";
  #    options =
  #      let
  #        conn_config = config.age.secrets."storagebox_bx11_connection_config".path;
  #      in
  #      [
  #        "_netdev,x-systemd.automount,noauto,x-systemd.idle-timeout=60s,x-systemd.device-timeout=5s,x-systemd.mount-timeout=5s,credentials=${conn_config}"
  #      ];
  #  };
  #};

  services.restic.backups."auth" = {
    timerConfig = {
      OnCalendar = "daily";
      Persistent = true;
    };
    repository = "/mnt/storagebox-bx11/backup/auth";
    initialize = true;
    passwordFile = config.age.secrets."storagebox_bx11_restic_password".path;
    paths = [
      "/var/lib/authelia-main"
    ];
    pruneOpts = [
      "--keep-daily 7"
      "--keep-weekly 5"
      "--keep-monthly 12"
      "--keep-yearly 15"
    ];
  };
}

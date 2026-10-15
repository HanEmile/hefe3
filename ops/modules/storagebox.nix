# CIFS mount of the Hetzner Storagebox (bx11), shared by medano, mail,
# lampadas and every VM importing //ops/vms/x86/modules/backups.nix.
#
# Lazy automount: the share is only mounted on first access under
# /mnt/storagebox-bx11 and unmounted after 60s idle. Credentials come
# from the agenix connection-config secret (keyed per host in
# ops/secrets/secrets.nix):
#
#   username=u331921
#   domain=u331921.your-storagebox.de
#   password=...

{ hefe }:

{ config, pkgs, ... }:

{
  age.secrets.storagebox_bx11_connection_config = {
    file = hefe.ops.secrets."storagebox_bx11_connection_config.age";
  };

  fileSystems."/mnt/storagebox-bx11" = {
    device = "//u331921.your-storagebox.de/backup";
    fsType = "cifs";
    options = [
      ("_netdev"
        + ",x-systemd.automount"
        + ",noauto"
        + ",x-systemd.idle-timeout=60s"
        + ",x-systemd.device-timeout=5s"
        + ",x-systemd.mount-timeout=5s"
        + ",credentials=${config.age.secrets.storagebox_bx11_connection_config.path}")
    ];
  };

  environment.systemPackages = [ pkgs.cifs-utils ];
}

{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "pretix";
  uuid = "95BAD2D5-42F1-480E-A712-F645B39C7098";
  # 2 GiB put pretix (jvm + postgres) into permanent memory pressure; with
  # systemd-oomd enabled every fresh ssh session scope was killed
  # pre-banner (2026-11-01). Bumped to 4.
  memory = 4;
  interfaces = [ "virbr0" ];

  # cd /keep/pools/vmpool && qemu-img create -f qcow2 pretix.qcow2 16G
  # (created 6.4G originally; grown live via virsh blockresize 2026-11-01)
  vmdisk = /keep/pools/vmpool/pretix.qcow2;
}

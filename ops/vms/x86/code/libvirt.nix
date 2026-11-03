{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "code";
  uuid = "5AE68F52-5BFF-455F-ACC0-7E79B59AFEA1";
  memory = 8; # GB - agentic builds
  vcpu_count = 8;

  # isolated bridge for the "code" network (see
  # machines/x86/medano/libvirt/networking/br_code.nix)
  interfaces = [ "virbr3" ];

  # the main vm disk
  # created 60G, grown to 120G on 2026-10-21 after /tmp (agent scratch)
  # filled it. Grow procedure (reboot REQUIRED: disk sits on ata1; qemu
  # can't notify an AHCI guest of capacity changes):
  #   host:  sudo virsh blockresize code sda 120G
  #   guest: echo ", +" | sudo sfdisk -N 1 --no-reread /dev/sda
  #          sudo partx -u /dev/sda && sudo resize2fs /dev/sda1
  vmdisk = /keep/pools/vmpool/code.qcow2;
}

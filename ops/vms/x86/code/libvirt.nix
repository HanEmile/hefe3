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
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 code.qcow2 80G
  vmdisk = /keep/pools/vmpool/code.qcow2;
}

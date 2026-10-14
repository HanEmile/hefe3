{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "jules";
  uuid = "1E85352D-469E-4580-9107-B85FC065C2E9";
  memory = 4; # GB
  vcpu_count = 4;
  interfaces = [ "virbr0" ];
  vmdisk = /keep/pools/vmpool/jules.qcow2;
}

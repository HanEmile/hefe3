{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "dev1";
  uuid = "45fd9544-1e55-47d1-9752-27fd4f33b5e9";
  memory = 4;
  interfaces = [ "virbr0" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/ubuntu-26.04-live-server-amd64.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 dev1.qcow2 80G
  vmdisk = /keep/pools/vmpool/dev1.qcow2;

  graphics = {
    type = "vnc";
    port = -1;
    autoport = true;
    listen.type = "address";
    listen.address = "127.0.0.1";
    passwd = "1234";
  };
}

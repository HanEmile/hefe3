{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
	name = "irc";
	uuid = "9066026B-B02B-43DB-A66D-2981BC86BF35";
	memory = 2; # GB
	interfaces = [ "virbr0" ];
	vmdisk = /keep/pools/vmpool/irc.qcow2;
	# NB: no install_vol - the disk image is bootable as-is.
}

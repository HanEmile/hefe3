# vms

This folder contains the definition for the VMs.

The VM folder is accessed through two kinds of "paths":

- `default.nix`
  - defines what's running on the vm
  - is called when building the vm
  - mainly imports `vm-base.nix` and expands on that
- `libvirt.nix`
  - defines the "external" vm config, as seen from the hypervisor 
  - called from the hypervisor
  - mainly imports `libvirt-base.nix` and expands on that
  
## default.nix
  
A VM config imports the vm base
* The first argument is an attrset defining the host this runs on (this is used to fetch the right gateway ip for the VM) and the primary bridge to use (per default `default`),
* The second arg provides the hefe module layer

A minmal yet complete `vm-name/default.nix` looks like this:

```nix
{
  { hefe, pkgs, lib, ... }:
  { config, ... }:
  
  {
    imports = [
      ./hardware-configuration.nix
      (import ../vm-base.nix { vmhost = "medano"; primaryBridge = "private"; } { inherit hefe pkgs; })

    ];
  
    networking.hostName = "vm-name";
  
    system.stateVersion = "25.05";
  }

}
```

## libvirt.nix

Kind of the same for `libvirt.nix`, it imports `../vm-base.nix`, passes the nixvirt import and provides just enough information to define a vm:

Creating the VMDisk is still a manual process, as is mounting the install_vol, doing the installation via `virsh console <name> console1`, then commenting out the install vol, deploying the host hypervisor in order to remove the install vol disk from the vm and rebooting. Not ideal, yet it's quite nice to just manage this all from within nix without having to fiddle around with libvirt xml.

```nix
{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "miki";
  uuid = "ECCDC38E-4801-4936-8943-C8171DC0E4F7";
  memory = 4; # GB
  interfaces = [ "virbr0" ];

  # comment out after first install
  # install_vol = "/keep/pools/isopool/latest-nixos-minimal-x86_64-linux.iso";

  # the main vm disk
  # cd /keep/pools/vmpool && qemu-img create -f qcow2 miki.qcow2 40G
  vmdisk = /keep/pools/vmpool/miki.qcow2;
}
```

## Installing nixos

```
sudo -i
parted /dev/sda -- mklabel msdos
parted /dev/sda -- mkpart primary 1MB 100%
parted /dev/sda -- set 1 boot on
mkfs.ext4 -L nixos /dev/sda1
mount /dev/disk/by-label/nixos /mnt
nixos-generate-config --root /mnt
vim /mnt/etc/nixos/configuration.nix
```

users.users.root.openssh.authorizedKeys.keys = [ "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPZi43zHEsoWaQomLGaftPE5k0RqVrZyiTtGqZlpWsew emile@caladan" ];

```
nixos-install
reboot
```

* comment out install_vol after first install
* deploy medano to apply 

* copy hardware-configuration.nix to hefe 
  * `scp root@photo:/etc/nixos/hardware-configuration.nix ops/vms/x86/photo/`

* add vm to //ops/acl/default.nix
* add vm to //ops/ipam/default.nix

* `virsh net-dhcp-leases default`
* adjust `~/.ssh/config` adding the vm with a ProxyJump: 

```
Host photo
  ProxyJump medano
  Hostname 192.168.75.5
```

* deploy vm (will break in the end, intended)
  * `nix-build -A ops.nixos.photo.deploy && ./result/bin/deploy`
  
* adjust the ssh config with the ip configured in `//ops/ipam/default.nix`
  
```
Host photo
  ProxyJump medano
  Hostname 192.168.75.9
```


### TODO

- [ ] install
  - [x] git
  - [x] md
  - [x] photo
  - [ ] social 
  - [x] auth 
  - [ ] tmp 
  - [ ] rss 
- [ ] configure
  - [ ] git
  - [x] md 
  - [ ] photo 
  - [ ] social 
  - [x] auth 
  - [ ] tmp
  - [ ] rss

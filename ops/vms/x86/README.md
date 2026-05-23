# Creating a new VM on medano

The recommended path is **image-based bootstrap**: a complete bootable qcow2
disk image is built from the VM's NixOS expression, scp'd to medano, and
booted by libvirt. No manual `nixos-install` ceremony.

The legacy ISO-based flow (`install_vol` + `nixos-install`) still works as a
fallback for VMs that need a custom partition layout, secure boot, or
similar.

## Image-based flow (recommended)

1. **Allocate IP + MAC in IPAM** (`ops/ipam/default.nix`) under
   `"default"` (or `"private"` for VPN-routed VMs):

   ```nix
   newvm = {
     v4 = "192.168.75.NN";
     mac = "02:..";  # echo "newvm" | md5sum | sed 's/^\(..\)\(..\)\(..\)\(..\)\(..\).*/02:\1:\2:\3:\4:\5/'
   };
   ```

2. **Register an ACL entry** (`ops/acl/default.nix`):

   ```nix
   newvm = withDefault { };
   ```

3. **Allow the VM to access //users** (only needed if your VM imports anything
   from `//users`, which `vm-base.nix` does). Add to the `exceptions` list in
   the top-level `default.nix`:

   ```nix
   [ "ops" "vms" "x86" "newvm" ]
   ```

4. **Create the VM directory** with two files:

   `ops/vms/x86/newvm/libvirt.nix`:

   ```nix
   { nixvirt, ... }:

   import ../libvirt-base.nix { inherit nixvirt; } {
     name = "newvm";
     uuid = "$(uuidgen)";
     memory = 2; # GB
     interfaces = [ "virbr0" ];
     vmdisk = /keep/pools/vmpool/newvm.qcow2;
     # NB: no install_vol — the disk image is bootable as-is.
   }
   ```

   `ops/vms/x86/newvm/default.nix`:

   ```nix
   { hefe, pkgs, ... }:
   { config, ... }:

   {
     imports = [
       ../hardware-image.nix
       (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
     ];

     networking.hostName = "newvm";
     system.stateVersion = "25.05";

     # service config follows
   }
   ```

   Note: no `hardware-configuration.nix`. The generic
   `../hardware-image.nix` describes the disk layout produced by
   `ops/lib/mkVmImage.nix` (legacy MBR, ext4 root labeled "nixos", grub on
   `/dev/sda`).

5. **Build and push the image** (from caladan, in `hefe3/`):

   ```sh
   nix-build -A ops.nixos.newvm.deploy_image && ./result/bin/deploy-image
   ```

   The script (a) builds the qcow2 (delegating to medano as the
   remote builder for x86_64-linux), (b) refuses to overwrite a running
   VM's disk, (c) scp's the image as `/keep/pools/vmpool/newvm.qcow2.new`,
   (d) does an atomic `mv` into place.

6. **Wire the VM into medano** by adding `(vm "newvm")` to
   `ops/machines/x86/medano/default.nix`, then deploy medano:

   ```sh
   nix-build -A ops.nixos.medano.deploy && ./result/bin/deploy
   ```

   libvirt will pick up the new domain on activation. The static DHCP
   reservation is generated automatically from the IPAM entry, so no edit
   to `ops/machines/x86/medano/libvirt/networking/br_default.nix` is
   needed.

7. **Verify**:

   ```sh
   ssh -J medano root@192.168.75.NN hostname
   ssh medano 'virsh list --all | grep newvm; virsh dommemstat newvm'
   ```

## Re-imaging an existing VM

To replace an existing VM's disk (e.g. after a major rewrite or to recover
from drift), stop the domain first, then re-run `deploy-image`:

```sh
ssh medano 'virsh shutdown newvm'
# wait for `virsh list --all` to show shut off
nix-build -A ops.nixos.newvm.deploy_image && ./result/bin/deploy-image
ssh medano 'virsh start newvm'
```

The `deploy-image` script refuses to push to a running VM (libvirt holds
the file open, and silently swapping the disk under a live qemu corrupts
state).

## Updating in place (most common case)

Once a VM is bootstrapped, you almost never need to re-image. Edit
`ops/vms/x86/newvm/default.nix` and run:

```sh
nix-build -A ops.nixos.newvm.deploy && ./result/bin/deploy
```

This does a `nixos-rebuild switch`-style update over SSH without
restarting the VM.

## Legacy ISO-based flow

If you need a manual install (uncommon — only for non-standard partition
layouts, encryption, or pre-existing data disks), uncomment
`install_vol` in `libvirt.nix`, deploy medano, attach to the VM with
`virsh console newvm console1`, and follow `install.sh` in the repo
root. Then comment out `install_vol`, redeploy, and copy the generated
`/etc/nixos/hardware-configuration.nix` back into the repo as
`ops/vms/x86/newvm/hardware-configuration.nix` (instead of importing
`../hardware-image.nix`).

## Files referenced

- `ops/lib/mkVmImage.nix` — wraps `<nixpkgs>/nixos/lib/make-disk-image.nix`
  with our partition/format conventions.
- `ops/vms/x86/hardware-image.nix` — generic hardware-config for
  image-built VMs.
- `ops/vms/x86/libvirt-base.nix` — shared libvirt domain template.
- `ops/vms/x86/vm-base.nix` — shared NixOS config (networking from
  IPAM, ssh, tailscale, prometheus exporter).
- `ops/vms/x86/demo01/` — minimal reference VM that bootstraps via the
  image flow.

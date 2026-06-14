# parella

Headless bare-metal box. Encrypted ZFS, systemd-boot/UEFI, remote
unlock over SSH.

## Hardware

| Component     | Model                              |
|---------------|------------------------------------|
| Motherboard   | ASRock B450 Gaming-ITX/ac          |
| CPU           | AMD Ryzen 5 2600 (6C/12T, 65W)    |
| RAM           | 8 GB DDR4-2133                     |
| SSD           | 250 GB WD Blue NVMe (WDS250G2B0C) |
| NIC           | Intel I211 GbE (igb driver)        |
| Coprocessor   | Intel Xeon Phi 3120P (see below)   |
| GPU           | **None** — fully headless          |
| PSU           | 600W                               |

NVMe by-id: `nvme-WDC_WDS250G2B0C_21302W802962`

## Disk layout

```
nvme0n1
├─ nvme0n1p1   1G   vfat (EFI)    -> /boot
└─ nvme0n1p2 231G   ZFS  (rpool)  -> encrypted, aes-256-gcm
```

ZFS datasets:

```
rpool/root -> /       (neededForBoot)
rpool/nix  -> /nix
rpool/home -> /home
rpool/var  -> /var
```

No swap device — uses `zramSwap` (50% of RAM).

## Key values

```
hostId:                ebac5691
ESP UUID:              B045-D514
NIC driver:            igb (Intel I211)
NIC interface name:    enp9s0
SSH host key:          ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOSJkHV7Owm3tTXzlHAajcHgoS35bTWFQ+OZ9iHwHNBg root@parella
Initrd SSH host key:   /etc/secrets/initrd/ssh_host_ed25519_key (NOT in git)
```

## Boot & unlock flow

Parella uses a **systemd-based initrd** (not the scripted initrd) with
SSH on port 2222 for headless ZFS encryption unlock.

```sh
# After reboot, SSH into the initrd:
ssh -p 2222 root@parella

# systemd-tty-ask-password-agent runs automatically via .profile
# Type the ZFS passphrase when prompted.
# Boot continues to stage 2, SSH on port 22 becomes available.

ssh root@parella
```

### SSH config (caladan ~/.ssh/config)

Must be placed ABOVE the `Host !*.* *` wildcard rule:

```
Host parella
    User root
    Hostname 192.168.1.30

Host parella-unlock
    User root
    Port 2222
    Hostname 192.168.1.30
```

> Note: the DHCP IP may change across reboots. Update as needed, or
> use `parella.pinto-pike.ts.net` once tailscale is up (only after
> stage 2, not during initrd unlock).

## Deploy

```sh
# From caladan, in ~/hefe3:
nix-build -A ops.nixos.parella.deploy && ./result/bin/deploy
```

The deploy builds on parella itself (`buildOnTarget = true` in
`ops/nixos.nix`). It ships the derivation from caladan, then
`nix-store --realise` on parella.

## Install runbook

### Prerequisites

- NixOS installer ISO: `nix-build -A ops.nixos.parella.iso`
- USB stick flashed with the ISO
- Ethernet cable connected to the B450's rear I/O GbE port

### Procedure

1. Boot from USB (NVMe should be empty or wiped). The ISO boots
   headless with DHCP + SSH. Find the IP from your router's DHCP
   lease table (hostname: `parella-installer`).

2. SSH in: `ssh root@<installer-ip>`

3. Partition:
   ```sh
   DISK=/dev/disk/by-id/nvme-WDC_WDS250G2B0C_21302W802962
   sgdisk --zap-all $DISK && wipefs -a $DISK
   partprobe $DISK; sleep 2
   sgdisk -og $DISK
   sgdisk -n1:0:+1G -t1:EF00 -c1:EFI $DISK
   sgdisk -n2:0:0 -t2:BF00 -c2:rpool $DISK
   partprobe $DISK; sleep 2
   mkfs.vfat -F32 -n EFI ${DISK}-part1
   ```

4. Create encrypted ZFS pool (interactive — prompts for passphrase):
   ```sh
   zpool create -f -o ashift=12 -o autotrim=on \
     -O compression=zstd -O acltype=posixacl -O xattr=sa -O atime=off \
     -O encryption=aes-256-gcm -O keylocation=prompt -O keyformat=passphrase \
     -O mountpoint=none -O canmount=off \
     rpool ${DISK}-part2
   ```

5. Create datasets with `mountpoint=legacy` (**critical** — using real
   mountpoints on the live ISO will mount over `/nix` and kill all
   binaries):
   ```sh
   zfs create -o mountpoint=legacy rpool/root
   zfs create -o mountpoint=legacy rpool/nix
   zfs create -o mountpoint=legacy rpool/home
   zfs create -o mountpoint=legacy rpool/var
   ```

6. Mount:
   ```sh
   mkdir -p /mnt
   mount -t zfs rpool/root /mnt
   mkdir -p /mnt/{boot,nix,home,var}
   mount -t zfs rpool/nix  /mnt/nix
   mount -t zfs rpool/home /mnt/home
   mount -t zfs rpool/var  /mnt/var
   mount ${DISK}-part1 /mnt/boot
   ```

7. Generate hostId and initrd SSH key:
   ```sh
   HOSTID=$(head -c4 /dev/urandom | od -A none -t x4 | sed 's/ //g')
   ESP_UUID=$(blkid -s UUID -o value ${DISK}-part1)
   echo "hostId: $HOSTID  ESP_UUID: $ESP_UUID"

   mkdir -p /mnt/etc/secrets/initrd
   ssh-keygen -t ed25519 -N "" -f /mnt/etc/secrets/initrd/ssh_host_ed25519_key
   ```

8. Write NixOS configs to `/mnt/etc/nixos/` (scp from caladan or
   write manually — see `configuration.nix` and
   `hardware-configuration.nix` templates). Update `hostId` and
   `ESP_UUID` in the configs.

9. Install:
   ```sh
   nix-channel --add https://nixos.org/channels/nixos-26.05 nixos
   nix-channel --update
   nixos-install --root /mnt --no-root-passwd
   ```

10. Set real mountpoints (using `canmount=noauto` to prevent
    auto-mounting over the live ISO):
    ```sh
    umount /mnt/boot /mnt/nix /mnt/home /mnt/var /mnt

    zfs set canmount=noauto rpool/root rpool/nix rpool/home rpool/var
    zfs set mountpoint=/     rpool/root
    zfs set mountpoint=/nix  rpool/nix
    zfs set mountpoint=/home rpool/home
    zfs set mountpoint=/var  rpool/var
    zfs set canmount=on rpool/root rpool/nix rpool/home rpool/var

    zpool export rpool
    ```

11. Remove USB, reboot. Unlock via `ssh -p 2222 root@<ip>`.

12. After first boot, from caladan:
    ```sh
    # Grab the host key
    ssh root@parella cat /etc/ssh/ssh_host_ed25519_key.pub
    # Add to ops/secrets/secrets.nix, then: cd ops/secrets && ragenix -r

    # Deploy the hefe3 config
    nix-build -A ops.nixos.parella.deploy && ./result/bin/deploy

    # Set up tailscale
    ssh root@parella 'tailscale up --ssh'
    ```

## Key learnings from the install

### ZFS dataset mountpoints on a live ISO

**DO NOT** create ZFS datasets with real mountpoints (e.g.
`mountpoint=/nix`) while running from a live ISO. ZFS immediately
auto-mounts the dataset, which overlays the ISO's `/nix/store` and
kills every binary on the system (including `zfs` itself, `sshd`,
everything). SSH drops and the system is unrecoverable without a
reboot.

**Fix**: always create datasets with `mountpoint=legacy` during
install. After `nixos-install`, unmount everything, then use
`canmount=noauto` + `zfs set mountpoint=<real>` +
`canmount=on` + `zpool export`. The `canmount=noauto` prevents
ZFS from auto-mounting when the mountpoint is changed.

### Scripted initrd does NOT work for headless encrypted ZFS

The NixOS scripted initrd (`boot.initrd.systemd.enable = false`)
prompts for the ZFS passphrase on `/dev/console` via
`zfs load-key -- rpool`. On a headless machine with no GPU, this
read blocks forever — there is no console input. The
`postCommands` SSH login approach (`echo "zfs load-key -a; killall
zfs" >> /root/.profile`) cannot work because:

1. The init's `zfs load-key` reads from `/dev/console`, not from
   the pipe that SSH can reach
2. The init's mount retries (10 seconds) time out before SSH is
   even reachable
3. After timeout, the init drops to an emergency prompt that also
   reads from `/dev/console` — unreachable on headless

**Fix**: use the **systemd-based initrd** (`boot.initrd.systemd.enable
= true`). Key differences:

- Uses `systemd-ask-password` socket protocol instead of
  `/dev/console` reads
- `systemd-tty-ask-password-agent --watch` connects to the socket
  from any terminal (including SSH)
- The initrd waits indefinitely for the key — no 10-second timeout
- Networking uses `systemd-networkd` (configure via
  `boot.initrd.systemd.network`), NOT the kernel `ip=dhcp`
  parameter

### systemd initrd networking

The kernel `ip=dhcp` parameter does NOT work with the systemd initrd
(it relies on `udhcpc` which is disabled). Configure DHCP via:

```nix
boot.initrd.network.enable = true;          # loads af_packet, etc.
boot.initrd.systemd.network = {
  enable = true;
  networks."10-dhcp" = {
    matchConfig.Name = [ "en*" "eth*" ];
    networkConfig.DHCP = "ipv4";
  };
};
```

### systemd initrd SSH unlock profile

Root's home in the systemd initrd is `/var/empty`, NOT `/root`.
Writing to `/root/.profile` is silently ignored. Use a systemd
service to write the profile:

```nix
boot.initrd.systemd.services.zfs-unlock-profile = {
  description = "Write ZFS unlock profile for SSH login";
  wantedBy = [ "initrd.target" ];
  before = [ "initrd-root-fs.target" ];
  unitConfig.DefaultDependencies = false;
  serviceConfig.Type = "oneshot";
  script = ''
    mkdir -p /var/empty
    echo 'systemd-tty-ask-password-agent --watch' > /var/empty/.profile
  '';
};
```

### Remote builder SSH on caladan (nix-darwin)

The nix daemon on caladan runs as root. For remote builders
(medano, lernaeus) to work:

1. **`sshKey`** must be set in `nix.buildMachines` — root doesn't
   have its own SSH key, point to `/Users/emile/.ssh/id_ed25519`
2. **`programs.ssh.knownHosts`** must include the builder host keys
   — without this, root's SSH hangs forever on host key
   verification (no TTY for interactive confirmation)

Both are configured in
`ops/machines/aarch64/caladan/default.nix`.

### nixos-install needs a channel

The live ISO doesn't have `<nixpkgs>` in the Nix search path by
default. Before `nixos-install`:

```sh
nix-channel --add https://nixos.org/channels/nixos-26.05 nixos
nix-channel --update
```

## Xeon Phi 3120P — status

**Not yet operational.** The Phi (Knights Corner, PCI ID `8086:225e`)
causes the B450 UEFI to hang during POST when physically installed.
The UEFI likely tries to initialize it as a display adapter. Without
a monitor/keyboard to change BIOS settings ("Initial Display Output"),
this cannot be resolved headlessly.

The Phi has two power connectors (8-pin + 6-pin PCIe) — both must
be connected. It draws up to 300W and is passively cooled (needs
directed airflow).

The `mic` kernel driver (Intel MIC/Xeon Phi) was removed from
mainline Linux. Knights Corner support requires the Intel MPSS
(Manycore Platform Software Stack) which is ancient and unlikely to
work on modern kernels.

### Planned approach

Once BIOS access is available (borrow a GPU temporarily):

1. Set BIOS "Initial Display Output" to onboard/disabled
2. Boot with Phi installed
3. Use VFIO (`vfio-pci.ids=8086:225e`) to claim the Phi at boot
4. Pass through to a QEMU VM for experimentation

Alternatively, run the Phi experiments on a different machine with
display access.

## Files

```
ops/machines/x86/parella/
├── default.nix                  # main NixOS config
├── boot.nix                     # boot/initrd/ZFS/SSH unlock config
├── hardware-configuration.nix   # disk layout, filesystems, CPU
└── README.md                    # this file
```

## Other changes made during parella bring-up

- `default.nix` (repo root): added readTree exception for parella
- `ops/acl/default.nix`: added `parella = withDefault { };`
- `ops/nixos.nix`: added `isoFor` function + `.iso` attribute on
  all machines (builds bootable installer ISO with SSH + ZFS tools)
- `ops/machines/aarch64/caladan/default.nix`: added `sshKey` to
  both builders, added `programs.ssh.knownHosts` for medano +
  lernaeus, added lernaeus as a build machine

# lernaeus

GPU box. Single 512GB NVMe SSD, 32GB RAM, NVIDIA RTX A2000 6GB.
Encrypted ZFS (`rpool`, native encryption) + systemd-boot/UEFI +
initrd-ssh remote unlock. Home LAN, DHCP, reachable over Tailscale.

## unlock

```
; ssh lernaeus-unlock        # Port 2222 (see ~/.ssh/config)
# zpool import -a            # usually already done by initrd postCommands
# zfs load-key -a
Enter passphrase for 'rpool':
# killall zfs                # releases askpass, boot continues
```

The local console also prompts for the passphrase if a monitor/keyboard
is attached.

---

## Install runbook

Do this from the box itself, booted off a **NixOS minimal ISO**
(`nixos-minimal-*-x86_64-linux.iso`). All commands as root (`sudo -i`).
The finished config lives in this repo and is deployed from caladan, but
the *first* install writes the pool and a bootstrap config to the disk.

### 0. Identify the disk

```sh
lsblk -o NAME,SIZE,MODEL,TYPE
ls -l /dev/disk/by-id/        # pick the stable nvme-... id, NOT nvme0n1
DISK=/dev/disk/by-id/nvme-<your-ssd-id>
```

Everything below uses `$DISK`. Using the `by-id` path keeps the pool
stable across reboots (matches `boot.zfs.devNodes = "/dev/disk/by-id"`).

### 1. Partition (GPT, UEFI)

```sh
# wipe any old signatures
wipefs -a "$DISK"
sgdisk --zap-all "$DISK"

# p1: 1GB EFI System Partition
sgdisk -n1:0:+1G   -t1:EF00 -c1:EFI   "$DISK"
# p2: rest -> ZFS
sgdisk -n2:0:0     -t2:BF00 -c2:rpool "$DISK"

partprobe "$DISK"; sleep 2
ls -l "$DISK"*           # confirm -part1 / -part2 exist
```

`$DISK-part1` is the ESP, `$DISK-part2` is the ZFS partition.

### 2. ESP filesystem

```sh
mkfs.vfat -F32 -n EFI "${DISK}-part1"
```

### 3. Create the encrypted pool

Single-disk pool, native encryption at the pool root, passphrase prompt.
`ashift=12` for 4K-sector NVMe; `-O` sets inheritable dataset props.

```sh
zpool create -f \
  -o ashift=12 \
  -o autotrim=on \
  -O compression=zstd \
  -O acltype=posixacl \
  -O xattr=sa \
  -O atime=off \
  -O encryption=aes-256-gcm \
  -O keylocation=prompt \
  -O keyformat=passphrase \
  -O mountpoint=none \
  -O canmount=off \
  rpool "${DISK}-part2"
# (prompts for the passphrase you will type at every unlock)
```

### 4. Datasets

```sh
zfs create -o mountpoint=legacy rpool/root
zfs create -o mountpoint=legacy -o atime=off rpool/nix
zfs create -o mountpoint=legacy rpool/home
zfs create -o mountpoint=legacy rpool/var

# 8GB swap zvol (32GB RAM, modest swap for hibernation-free safety)
zfs create -V 8G -b $(getconf PAGESIZE) \
  -o logbias=throughput -o sync=always \
  -o primarycache=metadata -o secondarycache=none \
  -o com.sun:auto-snapshot=false \
  rpool/swap
mkswap -L swap /dev/zvol/rpool/swap
```

### 5. Mount the target

```sh
mount -t zfs rpool/root /mnt
mkdir -p /mnt/{boot,nix,home,var}
mount -t zfs rpool/nix  /mnt/nix
mount -t zfs rpool/home /mnt/home
mount -t zfs rpool/var  /mnt/var
mount "${DISK}-part1"   /mnt/boot
swapon /dev/zvol/rpool/swap
```

### 6. Generate hardware config + hostId

```sh
nixos-generate-config --root /mnt

# unique stable ZFS hostId
head -c4 /dev/urandom | od -A none -t x4 | tr -d ' '
# -> e.g. 4f3a1b9c ; put it in default.nix networking.hostId
```

Look at `/mnt/etc/nixos/hardware-configuration.nix` and copy its
`boot.initrd.availableKernelModules` / `boot.kernelModules` /
`hardware.cpu.*` lines into this repo's
`ops/machines/x86/lernaeus/hardware-configuration.nix` (the ZFS
`fileSystems` and swap stanza are already correct there). **Confirm the
NIC driver** the generator picked and make sure it is also listed in
`boot.nix`'s `initrd.availableKernelModules`, otherwise remote unlock
won't have a network.

### 7. initrd ssh host key (for remote unlock)

`boot.nix` expects `/etc/secrets/initrd/ssh_host_ed25519_key`. Generate
it on the installed system (NOT committed to git):

```sh
mkdir -p /mnt/etc/secrets/initrd
ssh-keygen -t ed25519 -N "" \
  -f /mnt/etc/secrets/initrd/ssh_host_ed25519_key
```

### 8. First install

Two options:

**A. Bootstrap with the generated config, then switch to the repo**
(simplest for first boot):

```sh
# minimal: ensure /mnt/etc/nixos/configuration.nix enables zfs,
# systemd-boot, sshd, and your authorized key, then:
nixos-install --root /mnt
reboot
```

**B. Install the repo config directly** (needs the hefe checkout +
npins reachable on the box). Usually easier to do A, get on the network,
then `deploy` from caladan (step 9).

### 9. Bring it onto the fleet (from caladan)

Once it boots and you can unlock it:

1. Put the real `hostId` and hardware modules into
   `ops/machines/x86/lernaeus/{default.nix,hardware-configuration.nix}`
   (steps 6).
2. Grab the running system's ssh host key and add to secrets:
   ```sh
   ssh root@lernaeus cat /etc/ssh/ssh_host_ed25519_key.pub
   ```
   Add it as `lernaeus = "ssh-ed25519 ... root@lernaeus";` in
   `ops/secrets/secrets.nix` and to the `for [ ... ]` lists of any
   secrets lernaeus needs, then `cd ops/secrets && ragenix -r`.
3. Deploy:
   ```sh
   nix-build -A ops.nixos.lernaeus.deploy && ./result/bin/deploy
   ```
   (`deploy` for bare-metal hosts targets the bare hostname / ssh
   config alias — see `ops/nixos.nix`.)
4. Tailscale up (manual, first time):
   ```sh
   ssh root@lernaeus 'tailscale up --ssh'
   ```

### 10. SSH config aliases (on caladan, ~/.ssh/config)

```
Host lernaeus
    Hostname lernaeus.pinto-pike.ts.net

Host lernaeus-unlock
    Port 2222
    Hostname lernaeus.home.arpa   # or the DHCP/LAN IP; tailscale is down at unlock time
```

> Note: at unlock time tailscale is NOT up (it starts after the root fs
> mounts), so `lernaeus-unlock` must point at a LAN-reachable
> address/hostname, not the tailnet name.

## verify the GPU

```sh
nvidia-smi                 # driver bound, A2000 visible
nvtop                      # live utilisation
# container GPU smoke test (after enabling docker/podman):
# docker run --rm --gpus all nvidia/cuda:12.4.0-base-ubuntu22.04 nvidia-smi
```

---

## As-installed (2026-06-10)

The actual sequence used to install this box, with the corrections we had to
make afterward. This is the authoritative record for rebuilding a similar
machine. (The runbook above is the idealized version; this is what really
happened, including the gotchas.)

Hardware as built: **AMD Ryzen 5 5600G**, 32GB RAM, **Samsung 970 EVO Plus
500GB** NVMe, NVIDIA RTX A2000 6GB. Installed from a **NixOS 26.05 minimal
ISO** (a 21.05 stick was rejected — too old for the modern ZFS userland).

### Disk + pool (as run, root shell)

```sh
DISK=/dev/disk/by-id/nvme-Samsung_SSD_970_EVO_Plus_500GB_S4EVNZ0R207907X_1

# The disk had a stale GPT that made sgdisk miscompute free space
# ("Could not create partition 1 from 4294967296"). Full wipe fixed it:
zpool export -a; swapoff -a; umount -R /mnt 2>/dev/null
sgdisk --zap-all "$DISK"; wipefs -a "$DISK"; blkdiscard -f "$DISK"
partprobe "$DISK"; sleep 2
sgdisk -og "$DISK"                              # fresh empty GPT
sgdisk -n1:0:+1G -t1:EF00 -c1:EFI   "$DISK"     # 1G ESP
sgdisk -n2:0:0   -t2:BF00 -c2:rpool "$DISK"     # rest = ZFS
partprobe "$DISK"; sleep 2
mkfs.vfat -F32 -n EFI "${DISK}-part1"

# Encrypted pool (native encryption, passphrase prompt). NOTE: created the
# datasets with mountpoint=legacy at first - that was WRONG (see below).
zpool create -f -o ashift=12 -o autotrim=on \
  -O compression=zstd -O acltype=posixacl -O xattr=sa -O atime=off \
  -O encryption=aes-256-gcm -O keylocation=prompt -O keyformat=passphrase \
  -O mountpoint=none -O canmount=off \
  rpool "${DISK}-part2"

zfs create -o mountpoint=legacy rpool/root
zfs create -o mountpoint=legacy -o atime=off rpool/nix
zfs create -o mountpoint=legacy rpool/home
zfs create -o mountpoint=legacy rpool/var
# swap zvol was created here too, but later REMOVED in favour of zramSwap.

mount -t zfs rpool/root /mnt
mkdir -p /mnt/boot /mnt/nix /mnt/home /mnt/var
mount -t zfs rpool/nix  /mnt/nix
mount -t zfs rpool/home /mnt/home
mount -t zfs rpool/var  /mnt/var
mount "${DISK}-part1"   /mnt/boot

nixos-generate-config --root /mnt
head -c4 /dev/urandom | od -A none -t x4 | tr -d ' '   # hostId = 51392d58

mkdir -p /mnt/etc/secrets/initrd
ssh-keygen -t ed25519 -N "" -f /mnt/etc/secrets/initrd/ssh_host_ed25519_key
# (we initially mis-named this ssh_host_key_ed25519_key and renamed it;
#  boot.nix expects ssh_host_ed25519_key)
```

Then a minimal bootstrap `configuration.nix` (zfs + systemd-boot + sshd +
root key + hostId), `nixos-install --no-root-passwd`, reboot, and from then on
everything was deployed from caladan with
`nix-build -A ops.nixos.lernaeus.deploy && ./result/bin/deploy`.

### Three things that bit us (fix these the FIRST time)

1. **Datasets must NOT be `mountpoint=legacy`.** The repo config mounts with
   `zfsutil`, which needs real mountpoints. With `legacy`, stage-1 boot failed
   to mount root (`'rpool/root' cannot be mounted using 'zfs mount'`) and
   dropped to the emergency prompt every boot. Fix applied live from the
   initrd (persists on the pool):
   ```sh
   zfs set mountpoint=/     rpool/root
   zfs set mountpoint=/nix  rpool/nix
   zfs set mountpoint=/var  rpool/var
   zfs set mountpoint=/home rpool/home
   ```
   For a fresh build, create them with these mountpoints from the start.

2. **No zvol swap — use zramSwap.** The `rpool/swap` zvol's `/dev/zvol/...`
   udev symlink raced the scripted-initrd device wait ("waiting for device
   /dev/zvol/rpool/swap to appear") and wedged boot. Config now uses
   `zramSwap` (default.nix) and `swapDevices = [ ]`. The leftover zvol is
   harmless; `zfs destroy rpool/swap` to reclaim it.

3. **CPU SMU temp sensor wedged at 95.875C.** On this ASRock A520M-ITX/ac
   (BIOS L3.44), the on-die SMU thermal telemetry can saturate at its max
   code; k10temp Tctl + the nct6792 TSI0/SMBUSMASTER all read a frozen ~96C
   while the chip is actually cool. A **full power-drain** (PSU unplugged +
   hold power button) clears it; a BIOS update (L3.44 -> L3.90) is the
   permanent fix. The status bar prefers k10temp but falls back to the
   CPUTIN thermistor if Tctl reads >=95C.

### Display note

Monitor is 2560x1440 (Lenovo S24q-10) but the GPU reaches it through a
Mini-DP -> HDMI adapter (-> KVM -> monitor) that only negotiates an
HDMI-1.4-class link, capping at 1920x1080@60. Verified it's the adapter, not
the cable/KVM/monitor (a MacBook does 1440p75 through the same KVM). For
1440p: swap to a 4K60 Mini-DP->HDMI 2.0 adapter or Mini-DP->DP, then set
`output "DP-3" { mode 2560x1440@75Hz }` in home_emile.nix.

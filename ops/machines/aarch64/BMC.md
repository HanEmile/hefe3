# BMC (Baseboard Management Controller) Raspberry Pis

Four Raspberry Pi 1 Model B+ v1.2 boards act as always-on out-of-band
managers for the fleet's bare-metal PCs. Each Pi is connected to the main
switch and wired to its paired PC's power/reset headers via GPIO.

## Hardware

| BMC            | Managed PC  | Description       |
|----------------|-------------|-------------------|
| lampadas-bmc   | lampadas    | NAS               |
| lernaeus-bmc   | lernaeus    | GPU box           |
| lankiveil-bmc  | lankiveil   | Router PC         |
| parella-bmc    | parella     | Xeon Phi PC       |

**Pi Model**: Raspberry Pi 1 Model B+ v1.2 (BCM2835, ARMv6, 512 MB RAM)

**GPIO wiring** (active low, accent trigger):
- GPIO17 → PC power button header
- GPIO27 → PC reset button header
- Pi UART (GPIO14/15) → PC COM1 serial header (optional)

## File layout

```
ops/
  nixos.nix                          # nixosForBmc: cross-compilation setup
  modules/
    bmc-base.nix                     # shared NixOS module (SSH, GPIO, networking)
    bmc-power.nix                    # services.bmc-power option module
  machines/aarch64/
    lampadas-bmc/default.nix         # per-Pi config
    lernaeus-bmc/default.nix
    lankiveil-bmc/default.nix
    parella-bmc/default.nix
```

All BMC machines are under `ops.nixos.*` alongside the x86 machines.

## Building

Images are cross-compiled on medano (x86_64-linux → armv6l-linux). The
first build takes ~1 hour (GCC cross-compiler + kernel); subsequent
rebuilds take seconds since all store paths are cached on medano.

**Build a single image:**
```bash
ssh root@medano.emile.space \
  "cd /root/hefe3 && nix-build -A ops.nixos.lampadas-bmc.sdImage -j 12 --cores 12"
```

**Build all four:**
```bash
ssh root@medano.emile.space "cd /root/hefe3 && \
  for bmc in lampadas-bmc lernaeus-bmc lankiveil-bmc parella-bmc; do
    echo \$bmc: \$(nix-build -A ops.nixos.\$bmc.sdImage -j 12 --cores 12 --no-out-link)
  done"
```

**After changing configs**, sync to medano first:
```bash
rsync -az ~/hefe3/ops/ root@medano.emile.space:/root/hefe3/ops/
```

Then rebuild. Only changed derivations recompile.

## Flashing

From caladan, with an SD card inserted:

```bash
# Find your SD card
diskutil list

# Flash (streams image from medano, writes to SD card)
./flash-bmc.sh /dev/disk5 lampadas-bmc
```

The script builds the image on medano (instant if cached), streams it
over SSH, and writes it to the raw disk device. Requires sudo for dd.

**Manual flash:**
```bash
diskutil unmountDisk /dev/disk5
ssh root@medano.emile.space "cat /nix/store/<hash>-.../sd-image/*.img" \
  | sudo dd of=/dev/rdisk5 bs=64k
diskutil eject /dev/disk5
```

## Login

- **Console**: user `root` or `emile`, password `bmc`
- **SSH**: key-based only (caladan keys are pre-configured)

```bash
ssh root@lampadas-bmc    # or by IP
```

Password auth is disabled over SSH. Console password is set via
`initialPassword` in bmc-base.nix — change it on first login with
`passwd`.

## GPIO power control

Once logged into a BMC Pi:

```bash
systemctl start bmc-power-on     # short press power button (500ms pulse)
systemctl start bmc-power-off    # hold power button 5s (force off)
systemctl start bmc-reset        # pulse reset button (500ms)
```

GPIO pin assignments are configured per-host in each machine's
`default.nix` (default: GPIO17=power, GPIO27=reset). Change via:

```nix
services.bmc-power = {
  enable = true;
  powerGpio = 17;  # adjust as needed
  resetGpio = 27;
};
```

## Cross-compilation details

The build uses `nixpkgs.buildPlatform = x86_64-linux` and
`nixpkgs.hostPlatform = armv6l-linux`. Medano and lernaeus are
configured as armv6l-capable builders via:

1. `boot.binfmt.emulatedSystems = [ "armv6l-linux" ]` — QEMU user-mode
   emulation for derivations that must run armv6l code
2. `nix.settings.system-features` includes `gccarch-armv6kz` — required
   by the glibc bootstrap
3. Caladan's `/etc/nix/machines` lists both builders with
   `armv6l-linux` in their supported systems

**Known workarounds in the build:**

- **efivar/efibootmgr** — replaced with stubs (format string bugs on
  32-bit armv6l). The Pi has no EFI.
- **`KBUILD_MODPOST_WARN=1`** — RPi 5 RP1 drivers reference
  `__aeabi_uldivmod` (absent on armv6l). Made non-fatal so the kernel
  builds; those modules simply won't load on a Pi 1.
- **`SCHED_CLASS_EXT=n`** — sched_ext has a link error on armv6l
  cross-compilation.
- **git removed** from system packages — Rust `gitcore` can't
  cross-compile for armv6l.
- **Minimal initrd modules** — stripped the default NixOS module list
  (RAID, virtio, SATA) down to Pi 1 essentials (mmc, ext4, usb).

## Updating a running BMC

After the initial SD card flash, updates can be deployed over SSH
without re-flashing:

```bash
# Build on medano
ssh root@medano.emile.space \
  "cd /root/hefe3 && nix-build -A ops.nixos.lampadas-bmc.toplevel -j 12 --cores 12"

# Copy closure to the Pi and switch
# (TODO: implement once the Pis are on the network permanently)
```

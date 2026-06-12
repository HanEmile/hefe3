# Boot configuration for lernaeus.
#
# Single 512GB NVMe SSD, UEFI, systemd-boot. The root pool `rpool` uses
# ZFS native encryption (aes-256-gcm, passphrase keylocation=prompt) at
# the pool root, so a single passphrase unlocks every dataset.
#
# Remote unlock: an sshd runs in the initrd on port 2222 (mirrors medano
# and lampadas). To unlock:
#
#   ; ssh lernaeus-unlock      # Port 2222, see README.md
#   # zpool import -a          # usually already imported
#   # zfs load-key -a
#   Enter passphrase for 'rpool':
#   # killall zfs              # releases the askpass and lets boot proceed
#
# The local console will also prompt for the passphrase, so a keyboard +
# monitor works too.
{ hefe, ... }:

{
  config,
  lib,
  pkgs,
  ...
}:

{
  boot = {
    supportedFilesystems = [ "zfs" ];
    # Pin an explicit LTS kernel. `zfs.latestCompatibleLinuxPackages` is
    # deprecated (it now just points at the default kernel, which can race
    # ahead of ZFS support). 6.12 is the current LTS and is ZFS-supported.
    kernelPackages = pkgs.linuxPackages_6_12;

    # Single-disk pool: do NOT force-import. forceImportRoot=true (the
    # pre-26.11 default) can mask a pool that failed to export cleanly and
    # risks importing a pool another system still holds; false is the safe
    # new default. There's no second host contending for this disk.
    zfs.forceImportRoot = false;
    # Stable device nodes for ZFS vdevs (set by the install via -o ashift
    # and partlabels). by-id is the ZFS-recommended default for a single
    # consumer disk.
    zfs.devNodes = "/dev/disk/by-id";

    loader = {
      systemd-boot = {
        enable = true;
        # Keep a bounded number of generations on the 1GB ESP so it does
        # not fill up.
        configurationLimit = 20;
      };
      efi.canTouchEfiVariables = true;
    };

    # Bring the network up in the initrd via DHCP so the initrd sshd is
    # reachable for remote unlock.
    kernelParams = [
      "ip=dhcp"
      # Silence continuous "pcieport 0000:00:08.1: PME: Spurious native
      # interrupt!" log spam (~1 every 13s). This is a benign AMD Renoir/
      # Cezanne APU PME quirk on an internal USB/audio bridge - NOT an AER
      # or hardware error (all AER status registers are clean, ASPM already
      # off on that port). Disabling PCIe-port power management stops the
      # misfiring PME interrupt path; negligible cost on an always-on box.
      "pcie_port_pm=off"
    ];

    initrd = {
      # Use the scripted (pre-systemd) initrd. The remote-unlock flow
      # below relies on `network.postCommands` to drop the operator into
      # the `zfs load-key` prompt, which systemd-stage-1 does not support.
      # This matches the medano/lampadas unlock model.
      systemd.enable = false;

      # Modules needed to see the NVMe disk and the wired NIC early.
      # availableKernelModules is refined by the generated
      # hardware-configuration.nix after `nixos-generate-config`; the
      # network driver MUST be present here (and the in-tree name may
      # differ - adjust after the first `lspci -k`).
      availableKernelModules = [
        "nvme"
        "xhci_pci"
        "ahci"
        "usbhid"
        "usb_storage"
        "sd_mod"
        # Common Intel/Realtek 1GbE NIC drivers; the generator usually
        # adds the right one, but we keep the likely candidates so remote
        # unlock keeps working across kernel bumps.
        "e1000e"
        "r8169"
        "igb"
        # AES acceleration for ZFS native encryption.
        "aesni_intel"
        "cryptd"
      ];
      kernelModules = [ "aesni_intel" ];

      network = {
        enable = true;
        ssh = {
          enable = true;
          # Different port from the running system's sshd so clients don't
          # complain about a changing host key (initrd uses its own key).
          port = 2222;
          # Unquoted path: the key is copied into the initrd from this
          # location on the installed system. Generate it during install
          # (see README.md) - DO NOT commit the private key.
          hostKeys = [ /etc/secrets/initrd/ssh_host_ed25519_key ];
          authorizedKeys = hefe.users.hanemile.keys.all;
        };
        # Drop the unlocker straight into the zfs load-key flow.
        postCommands = ''
          zpool import -a || true
          echo "zfs load-key -a; killall zfs" >> /root/.profile
        '';
      };
    };
  };
}

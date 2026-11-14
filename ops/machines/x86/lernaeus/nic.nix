# RTL8111/8168 NIC stability fixes for lernaeus (Realtek, PCI 10ec:8168 rev 15).
#
# Symptom (~60+ watchdog events per boot, bursts that kill all
# connectivity for minutes - originally diagnosed as "tailscale going
# offline", twice confirmed correlating with Sunshine streaming load):
#
#   r8169 enp4s0: NETDEV WATCHDOG: transmit queue 0 timed out 5001 ms
#   dhcpcd: fe80::... is unreachable  (router flapping, ~every 10s)
#
# Escalation history (same day, in order):
#  1. runtime mitigations failed to stop the storms: EEE off,
#     PCI function pinned to D0 (power/control=on), then ALL offloads off.
#  2. FINAL FIX - copied from lampadas, which has the SAME NIC family
#     (RTL8168h/8111h = 10ec:8168) and the same bug (see its default.nix
#     for the long write-up; ~6-11k watchdogs/day observed there):
#     swap the in-tree r8169 driver for the out-of-tree Realtek vendor
#     driver r8168, which does not hit this at all on the same hardware.
#
# Remaining notes:
#  - `options r8168 aspm=0 eee_enable=0 s0_magic_packet=0` mirrors
#    lampadas: even the vendor driver wedged under load with PCIe ASPM
#    and/or EEE on (one watchdog every ~12s until both were off).
#  - pcie_aspm=off kernel param stays as a (redundant but free) second
#    line of defense now that a reboot is required anyway.
#  - The ethtool offload disabling + EEE-off fixup service stays: it is
#    driver-agnostic insurance and costs performance nothing on a 1G link.
#  - initrd gets r8168 loaded explicitly so remote LUKS unlock over ssh
#    keeps working (r8169 is blacklisted system-wide).
{ hefe, ... }:

{
  config,
  lib,
  pkgs,
  ...
}:

{
  boot = {
    extraModulePackages = [ config.boot.kernelPackages.r8168 ];
    blacklistedKernelModules = [ "r8169" ];
    kernelParams = [ "pcie_aspm=off" ];
    extraModprobeConfig = ''
      options r8168 aspm=0 eee_enable=0 s0_magic_packet=0
    '';

    initrd = {
      availableKernelModules = [ "r8168" ];
      kernelModules = [ "r8168" ];
    };
  };

  # Driver-agnostic runtime mitigations, applied every boot and whenever
  # the interface appears (udev). With r8168 they are pure insurance.
  services.udev.extraRules = ''
    ACTION=="add", SUBSYSTEM=="pci", KERNEL=="0000:04:00.0", \
      RUN+="${pkgs.ethtool}/bin/ethtool --set-eee enp4s0 eee off", \
      RUN+="${pkgs.bash}/bin/sh -c 'echo on > /sys/bus/pci/devices/0000:04:00.0/power/control'"
  '';

  systemd.services.nic-realtek-fixup = {
    description = "Apply RTL8168 power-management/offload mitigation (r816x wedge)";
    wantedBy = [ "multi-user.target" ];
    after = [ "network-pre.target" ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };
    script = ''
      ${pkgs.ethtool}/bin/ethtool --set-eee enp4s0 eee off || true
      ${pkgs.ethtool}/bin/ethtool -K enp4s0 tx off rx off sg off tso off ufo off gso off gro off || true
      echo on > /sys/bus/pci/devices/0000:04:00.0/power/control || true
    '';
  };
}

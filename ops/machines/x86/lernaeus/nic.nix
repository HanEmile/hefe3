# RTL8111/8168 NIC stability fixes for lernaeus (Realtek r8169 driver).
#
# Symptom (~60+ times per boot, bursts that kill all connectivity for
# minutes - originally diagnosed as "tailscale going offline"):
#
#   r8169 enp4s0: NETDEV WATCHDOG: transmit queue 0 timed out 5001 ms
#   dhcpcd: fe80::... is unreachable  (router flapping, ~every 10s)
#
# This is the textbook r8169 (RTL8168 rev 15) failure mode: the chip's
# power management (PCIe ASPM link states / D3hot entry, and EEE LPI)
# wedges the TX queue and the kernel watchdog has to reset it.
#
# Countermeasures:
#  1. pcie_aspm=off on the kernel line - disables ASLink power management
#     system-wide. Desktop box, power impact is negligible. Needs a reboot
#     to take effect.
#  2. Runtime `power/control = on` on the PCI function - keeps the device
#     in D0, never power-gated. Applied immediately by nic-realtek-fixup
#     (and re-applied on every boot/activation via udev).
#  3. EEE off - cheap insurance; link-level "green ethernet" LPI is
#     another well-known r8169 TX-hang trigger.
{ hefe, ... }:

{
  config,
  lib,
  pkgs,
  ...
}:

{
  # (1) permanent, kernel-wide ASPM off. Takes effect on next reboot.
  boot.kernelParams = [ "pcie_aspm=off" ];

  # (2)+(3) applied right now (no reboot needed) and re-applied whenever
  # the interface appears, via a udev rule.
  services.udev.extraRules = ''
    ACTION=="add", SUBSYSTEM=="pci", KERNEL=="0000:04:00.0", \
      RUN+="${pkgs.ethtool}/bin/ethtool --set-eee enp4s0 eee off", \
      RUN+="${pkgs.bash}/bin/sh -c 'echo on > /sys/bus/pci/devices/0000:04:00.0/power/control'"
  '';

  systemd.services.nic-realtek-fixup = {
    description = "Apply RTL8168 power-management workarounds (r8169 TX wedge)";
    wantedBy = [ "multi-user.target" ];
    after = [ "network-pre.target" ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };
    script = ''
      ${pkgs.ethtool}/bin/ethtool --set-eee enp4s0 eee off || true
      echo on > /sys/bus/pci/devices/0000:04:00.0/power/control || true
    '';
  };
}

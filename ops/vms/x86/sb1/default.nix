{ hefe, pkgs, ... }:
{ config, lib, ... }:

let
  ipam = hefe.ops.ipam.default.sb1;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "sb1";
  system.stateVersion = "25.05";

  boot.kernelModules = [ "kvm-intel" ];

  # Use latest 7.1 stable kernel (all BPF fixes included)
  boot.kernelPackages = pkgs.linuxPackages_7_1;

  boot.kernelPatches = [{
    name = "ebpf-fuzzing-config";
    patch = null;
    structuredExtraConfig = with lib.kernel; {
      KCOV = yes;
      KCOV_INSTRUMENT_ALL = yes;
      KCOV_ENABLE_COMPARISONS = yes;
    };
  }];

  boot.kernel.sysctl = {
    "kernel.unprivileged_bpf_disabled" = 0;
    "net.core.bpf_jit_enable" = 1;
  };

  security.sudo.extraRules = [{
    users = [ "emile" ];
    commands = [{ command = "ALL"; options = [ "NOPASSWD" ]; }];
  }];

  environment.systemPackages = with pkgs; [
    gcc gnumake go rustc cargo python3 wget curl strace pciutils
    ncurses elfutils pkg-config clang llvmPackages.llvm bpftools
    linuxHeaders gdb qemu flex bison bc perl openssl pahole elfutils zlib
  ];

  services.healthProbes.probes = [
    { name = "node-exporter"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

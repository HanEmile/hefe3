{ hefe, pkgs, ... }:
{ config, ... }:

let
  ipam = hefe.ops.ipam.default.sb3;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "sb3";
  system.stateVersion = "25.05";

  # kernelCTF development environment
  environment.systemPackages = with pkgs; [
    qemu
    gcc
    gnumake
    binutils
    python3
    wget
    curl
    gdb
    strace
    bpftools
    clang
    llvm
    flex
    bison
    bc
    elfutils
    openssl
    pkg-config
    ncurses
  ];

  # Enable KVM for nested virtualization (kernelCTF QEMU inside this VM)
  virtualisation.libvirtd.enable = false;
  boot.kernelModules = [ "kvm-intel" "kvm-amd" ];

  # Allow larger disk for kernel builds
  services.healthProbes.probes = [
    { name = "node-exporter"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}

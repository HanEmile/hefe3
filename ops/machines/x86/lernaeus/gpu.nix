# GPU configuration for lernaeus: NVIDIA RTX A2000 (6GB, Ampere GA106).
#
# Proprietary driver + CUDA + container toolkit (Docker/Podman GPU via CDI)
# AND the Wayland/sway + Steam gaming stack prerequisites. The A2000 is on
# the production driver branch (595+), which has GBM support (needed for
# wlroots/sway on NVIDIA) and 32-bit GL/Vulkan (needed for Steam/Proton).
{ hefe, ... }:

{
  config,
  lib,
  pkgs,
  ...
}:

{
  # NOTE: the proprietary NVIDIA driver + CUDA are unfree. The fleet
  # builder (ops/nixos.nix) creates the nixpkgs instance with
  # `config.allowUnfree = true` already, so we must NOT set
  # `nixpkgs.config` here (it errors: "system configures nixpkgs with an
  # externally created instance"). If you ever `nixos-rebuild` directly
  # on the box during bring-up, pass `--impure` / set NIXPKGS_ALLOW_UNFREE=1
  # instead.

  # OpenGL / Vulkan / compute userland, including 32-bit. enable32Bit
  # pulls in i686-linux NVIDIA libs (nvidia-egl-*-x32 etc.) required by
  # Steam/Proton/Wine. This previously broke deploys when the closure was
  # built on the x86_64-only medano remote builder ("Failed to find a
  # machine for remote build! required: i686-linux"). It works now because
  # lernaeus builds its OWN closure on-target (ops/nixos.nix buildOnTarget)
  # and an x86_64-linux host builds i686-linux derivations natively.
  hardware.graphics = {
    enable = true;
    enable32Bit = true;
  };

  services.xserver.videoDrivers = [ "nvidia" ];

  hardware.nvidia = {
    # Open kernel modules (Ampere supports them; now NVIDIA's recommended
    # default for Turing+). Switched ON to fix the monitor being capped at
    # 1920x1080 instead of its native 2560x1440: NVIDIA driver bug #960 /
    # nvbug 5651624 (a mode-validation regression that drops the EDID's
    # preferred 1440p DTD on DP, exposing only the lower CEA modes) was
    # fixed in the -open modules first. Confirmed by users with identical
    # hardware (RTX A2000 + Lenovo QHD over DisplayPort).
    open = true;
    modesetting.enable = true;
    nvidiaSettings = true;
    # Pin the production branch for stability on a headless box.
    package = config.boot.kernelPackages.nvidiaPackages.production;
    # Lets the card power down when idle (single consumer GPU, no
    # persistence daemon needed for compute bursts).
    powerManagement.enable = false;
  };

  # GPU containers: nvidia-container-toolkit wires CDI into Docker/Podman.
  hardware.nvidia-container-toolkit.enable = true;

  environment.systemPackages = with pkgs; [
    # `nvidia-smi`, `nvidia-settings` come with the driver. Add CUDA
    # tooling + monitoring.
    cudaPackages.cuda_nvcc
    cudaPackages.cudatoolkit
    nvtopPackages.nvidia
    pciutils # lspci -k to confirm the driver bound
  ];
}

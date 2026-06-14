# Shared NixOS base configuration for BMC Raspberry Pis.
#
# This is a standard NixOS module (single-function). It receives `hefe`
# via specialArgs (set in ops/bmc.nix's nixosForBmc).
{ config, pkgs, lib, hefe, ... }:

{
  boot = {
    loader = {
      grub.enable = false;
      generic-extlinux-compatible.enable = true;
    };

    # Disable RP1 (Pi 5) and other modules that fail cross-compilation
    # for armv6l due to missing __aeabi_uldivmod in modpost.
    kernelPatches = [{
      name = "disable-rp1-for-armv6l";
      patch = null;
      structuredExtraConfig = with lib.kernel; {
        PWM_RP1 = lib.mkForce no;
        I2C_DESIGNWARE_PLATFORM = lib.mkForce no;
        VIDEO_RP1_CFE = lib.mkForce no;
        SND_SOC_RP1_HEADPHONES = lib.mkForce no;
        DRM_RP1_DSI = lib.mkForce no;
        DRM_RP1_DPI = lib.mkForce no;
        DRM_RP1_VEC = lib.mkForce no;
      };
    }];

    initrd.availableKernelModules = lib.mkForce [
      "usbhid"
      "usb_storage"
      "mmc_block"
      "sdhci"
      "bcm2835_dma"
      "i2c_bcm2835"
      "ext4"
      "sd_mod"
    ];
    initrd.kernelModules = lib.mkForce [ ];
  };

  hardware.enableRedistributableFirmware = true;

  documentation.enable = false;
  documentation.man.enable = false;
  documentation.nixos.enable = false;
  fonts.fontconfig.enable = false;

  zramSwap = {
    enable = true;
    memoryPercent = 50;
  };

  networking = {
    useDHCP = true;
    domain = "home.arpa.";
    search = [ "home.arpa" ];
    nameservers = [ "8.8.8.8" "8.8.4.4" "1.1.1.1" ];
    firewall = {
      enable = true;
      allowedTCPPorts = [ ];
    };
  };

  time.timeZone = "Europe/Berlin";

  users = {
    mutableUsers = false;
    users.root = {
      initialPassword = "bmc";
      openssh.authorizedKeys.keys = hefe.users.hanemile.keys.all;
    };
    users.emile = {
      isNormalUser = true;
      initialPassword = "bmc";
      extraGroups = [ "wheel" "gpio" "dialout" ];
      openssh.authorizedKeys.keys = hefe.users.hanemile.keys.all;
    };
  };

  users.groups.gpio = { };

  services.udev.extraRules = ''
    SUBSYSTEM=="bcm2835-gpiomem", KERNEL=="gpiomem", GROUP="gpio", MODE="0660"
    SUBSYSTEM=="gpio", KERNEL=="gpiochip*", ACTION=="add", PROGRAM="${pkgs.bash}/bin/bash -c 'chgrp gpio /sys/class/gpio/export /sys/class/gpio/unexport; chmod 220 /sys/class/gpio/export /sys/class/gpio/unexport'"
  '';

  environment.systemPackages = with pkgs; [
    vim
    # git — disabled: cross-compilation of gitcore (Rust) fails for armv6l
    tmux
    htop
    libgpiod
    picocom
  ];

  programs.mosh.enable = true;

  services = {
    openssh = {
      enable = true;
      settings = {
        PasswordAuthentication = false;
        KbdInteractiveAuthentication = false;
      };
    };

    tailscale = {
      enable = true;
      extraUpFlags = [ "--ssh" ];
    };
  };

  nix = {
    gc = {
      automatic = true;
      dates = "weekly";
      options = "--delete-older-than 7d";
    };
    settings.auto-optimise-store = true;
  };
}

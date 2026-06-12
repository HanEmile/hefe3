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

    initrd.availableKernelModules = [
      "usbhid"
      "usb_storage"
      "vc4"
      "bcm2835_dma"
      "i2c_bcm2835"
    ];
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
      openssh.authorizedKeys.keys = hefe.users.hanemile.keys.all;
    };
    users.emile = {
      isNormalUser = true;
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
    git
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

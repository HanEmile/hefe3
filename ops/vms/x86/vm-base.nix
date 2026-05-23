# usage:
# imports = [
#   (import ../vm-base.nix { vmhost="medano"; } { inherit hefe pkgs; })
# ]

# the host to run on (this is used to set the gateway ip), and the default bridge to use.
{ vmhost, primaryBridge ? "default" }:

# the hefe module layer
{ hefe, pkgs, ... }:

# the "normal" module layer passed by imports
{ config, ... }:

let
  acl = hefe.ops.acl;
  hostname = config.networking.hostName;
in
{
  boot.loader.grub.enable = true;
  boot.loader.grub.device = "/dev/sda";

  time.timeZone = "Europe/Helsinki";

  documentation.nixos.enable = false;

  users = let
    aclconf = with acl; (usersForHost host."${hostname}");
  in {
    users = aclconf.users;
    groups = aclconf.groups;
  };

  environment.systemPackages = with pkgs; [
    vim
    htop
    git
    tailscale
    vnstat
  ];

  networking = {
    useDHCP = false;

    interfaces = {
      "enp1s0" = {
        useDHCP = false;
        macAddress = hefe.ops.ipam."${primaryBridge}"."${hostname}".mac;
        ipv4.addresses = [
          {
            address = hefe.ops.ipam."${primaryBridge}"."${hostname}".v4;
            prefixLength = 24;
          }
        ];
      };
    };

    defaultGateway = {
      address = hefe.ops.ipam."${primaryBridge}"."${vmhost}".v4;
      interface = "enp1s0";
    };

    nameservers = [ "8.8.8.8" ];

    firewall = {
      enable = true;
      # Tailscale: HTTP for vhosts on the overlay, plus node-exporter for the
      # dashboard/prometheus scrape.
      interfaces."tailscale0".allowedTCPPorts = [ 80 443 9100 ];
      allowedTCPPorts = [ ];
      allowedUDPPorts = [ ];
    };
  };

  services = {
    openssh = {
      enable = true;
      authorizedKeysInHomedir = true; # enables ~/.ssh/authorized_keys
      settings = {
        PasswordAuthentication = false;
        KbdInteractiveAuthentication = false;
      };
    };

    tailscale = {
      enable = true;
      extraUpFlags = [ "--ssh" ];
    };

    vnstat.enable = true;

    # Prometheus node-exporter — base metrics for every VM, scraped from
    # medano over tailscale.
    prometheus.exporters.node = {
      enable = true;
      listenAddress = "0.0.0.0";
      port = 9100;
      enabledCollectors = [ "systemd" "logind" "processes" ];
    };
  };

  nix = {
    gc = {
      automatic = true;
      dates = "weekly";
      options = "--delete-older-than 14d";
    };
    settings = {
      auto-optimise-store = true;
    };
  };
}

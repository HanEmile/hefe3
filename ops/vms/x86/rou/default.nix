{ hefe, pkgs, ... }:
{ config, ... }:

{
  imports = [
    ./hardware-configuration.nix

    # not using the vm-base here, as this config differs so hard from what it'd assume
  ];

  boot = {
    loader.grub.enable = true;
    loader.grub.device = "/dev/sda";
    kernel.sysctl = {
      "net.ipv4.conf.all.forwarding" = true;
      "net.ipv4.ip_forward" = 1;
      "net.ipv6.conf.all.forwarding" = 1;
    };
  };

  time.timeZone = "Europe/Helsinki";

  users.users.root.openssh.authorizedKeys.keys = hefe.users.hanemile.keys.all;

  environment.systemPackages = with pkgs; [
    vim
    htop
    vnstat
  ];

  age.secrets = {
    mullvad_famous_hog_priv.file = hefe.ops.secrets."mullvad_famous_hog_priv.age";
  };

  networking = {
    hostName = "rou";
    useDHCP = false;

    interfaces = {
      "enp1s0" = {
        useDHCP = false;
        macAddress = "52:54:00:90:70:2a";
        ipv4.addresses = [
          {
            address = "192.168.33.2";
            prefixLength = 24;
          }
        ];
      };
      "enp2s0" = {
        useDHCP = false;
        macAddress = "52:54:00:f6:f8:94";
        ipv4.addresses = [
          {
            address = "192.168.34.2";
            prefixLength = 24;
          }
        ];
      };
    };

    defaultGateway = {
      address = "192.168.34.1";
      interface = "enp2s0";
    };

    nameservers = [
      "8.8.8.8"
      "10.64.0.1" # mullvad wireguard
    ];

    nat = {
      enable = true;
      internalInterfaces = [ "enp1s0" ];
      internalIPs = [ "192.168.33.0/24" ];
      externalInterface = "wg0"; # route everything through the wireguard interface
      forwardPorts = [
        {
          sourcePort = 53;
          proto = "tcp";
          destination = "10.70.237.140:53";
        }
        {
          sourcePort = 53;
          proto = "udp";
          destination = "10.70.237.140:53";
        }
      ];
    };

    firewall = {
      enable = true;
      allowedTCPPorts = [ 53 ];
      allowedUDPPorts = [ 53 ];
    };

    wireguard = {
      enable = true;
      interfaces."wg0" = let
          wg-pub-ip = "185.204.1.203";
          default-gw = config.networking.defaultGateway.address;
        in {
        postSetup = "${pkgs.iproute2}/bin/ip route add ${wg-pub-ip} via ${default-gw}";
        postShutdown = "${pkgs.iproute2}/bin/ip route del ${wg-pub-ip} via ${default-gw}";

        privateKeyFile = config.age.secrets."mullvad_famous_hog_priv".path;
        ips = [
          "10.67.139.185/32"
          "fc00:bbbb:bbbb:bb01::4:8bb8/128"
        ];
        peers = [
          {
            publicKey = "veLqpZazR9j/Ol2G8TfrO32yEhc1i543MCN8rpy1FBA=";
            allowedIPs = [ "0.0.0.0/0" "::/0" ];
            endpoint = "${wg-pub-ip}:51820";
            persistentKeepalive = 25;
          }
        ];
      };
    };
  };

  services = {
    openssh.enable = true;
    vnstat.enable = true;
  };

  system.stateVersion = "25.05";
}

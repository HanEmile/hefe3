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

      # NixOS 26.05 (iptables-nft backend) does not reliably apply the
      # auto-generated MASQUERADE to *forwarded* traffic egressing the
      # point-to-point wg0 (/32) interface: arr (192.168.33.0/24) packets
      # left wg0 with their private source intact and Mullvad dropped them
      # (conntrack showed [UNREPLIED] with reply tuple still 192.168.33.x).
      # Force an explicit SNAT to rou's wg0 client IP for the bridge subnet.
      extraCommands = ''
        iptables -t nat -I nixos-nat-post 1 -s 192.168.33.0/24 -o wg0 -j SNAT --to-source 10.67.139.185
      '';

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
          wg-pub-ip = "193.138.7.137"; # fi-hel-wg-101 (fi-hel-wg-001 retired)
          default-gw = config.networking.defaultGateway.address;
        in {
        # Pin the Mullvad endpoint to the real uplink (enp2s0); then force wg0
        # to OWN the default route. NixOS 26.05 installs the peer allowedIPs
        # 0.0.0.0/0 route at metric 1024, which LOSES to the static
        # networking.defaultGateway (metric 0) via enp2s0 - so all forwarded
        # VM traffic (192.168.33.0/24) egressed enp2s0 with a private source
        # and was dropped (medano only NATs .34/.75, not .33). Re-add the wg0
        # default at metric 0 so it wins. This regressed on the 26.05 upgrade.
        postSetup = ''
          ${pkgs.iproute2}/bin/ip route replace ${wg-pub-ip} via ${default-gw}
          ${pkgs.iproute2}/bin/ip route replace default dev wg0
        '';
        postShutdown = ''
          ${pkgs.iproute2}/bin/ip route del ${wg-pub-ip} via ${default-gw} || true
          ${pkgs.iproute2}/bin/ip route del default dev wg0 || true
        '';

        privateKeyFile = config.age.secrets."mullvad_famous_hog_priv".path;
        ips = [
          "10.67.139.185/32"
          "fc00:bbbb:bbbb:bb01::4:8bb8/128"
        ];
        peers = [
          {
            publicKey = "2S3G7Sm9DVG6+uJtlDu4N6ed5V97sTbA5dCSkUelWyk=";
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

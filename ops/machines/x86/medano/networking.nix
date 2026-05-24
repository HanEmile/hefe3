{ config, ... }:

let
  pubv4 = "95.217.35.60";
  ircVMip = "192.168.75.5";

  httpPort = "80";
  httpsPort = "80";
  narajVMip = "192.168.75.2";

  ircBouncerPort = 6697;
in
{

  boot.kernel.sysctl = {
    "net.ipv4.conf.all.forwarding" = true;
    "net.ipv4.ip_forward" = 1;
    "net.ipv6.conf.all.forwarding" = 1;

    # “If DNAT rewrites a packet to an address on a local interface, don’t consume it locally — treat it as routable and forward it.”
    "net.ipv4.conf.eno1.route_localnet" = 1;
    "net.ipv4.conf.virbr0.route_localnet" = 1;
  };

  networking = {
    hostName = "medano";
    hostId = "e1ce6466";

    useDHCP = false;

    interfaces = {
      "eno1" = {
        useDHCP = false;
        ipv4.addresses = [
          {
            address = pubv4;
            prefixLength = 26;
          }
        ];
        ipv6.addresses = [
          {
            address = "2a01:4f9:2b:2d45::1";
            prefixLength = 64;
          }
        ];
      };
    };

    defaultGateway = "95.217.35.1";
    defaultGateway6 = {
      address = "fe80::1";
      interface = "enp0s31f6";
    };

    nameservers = [ "8.8.8.8" ];

    nat = {
      enable = true;

      externalInterface = "eno1";
      internalInterfaces = [
        # TODO(emile): use ops.ipam names
        "virbr0"
      ];

      # The IP address ranges for which to perform NAT. Packets coming from these addresses (on any interface) and destined for the external interface will be rewritten.
      internalIPs = [
        # TODO(emile): get this ip via ops.ipam
        # TODO(emile): pass `hefe` to this file
        # irc
        "${ircVMip}/32"
      ];

      forwardPorts = [
        # Forward external 80/443 to naraj (TLS-terminating ingress VM).
        { destination = "${narajVMip}:80";  proto = "tcp"; sourcePort = 80; }
        { destination = "${narajVMip}:443"; proto = "tcp"; sourcePort = 443; }
        # forward irc traffic directed at the host to the `irc` VM
        # {
        #   destination = "${ircVMip}:${toString ircBouncerPort}";
        #   proto = "tcp";
        #   sourcePort = ircBouncerPort;
        # }
      ];

      extraCommands = ''
        # Clean hairpin NAT: internal -> external IP -> internal service
        iptables -t nat -I PREROUTING 1 -s 192.168.75.0/24 -d ${pubv4} -p tcp --dport ${toString ircBouncerPort} -j DNAT --to-destination ${ircVMip}:${toString ircBouncerPort}

        iptables -t nat -I PREROUTING 1 -s 192.168.75.0/24 -d ${pubv4} -p udp --dport ${toString ircBouncerPort} -j DNAT --to-destination ${ircVMip}:${toString ircBouncerPort}

        # Allow web traffic from VMs to host
        iptables -I INPUT 1 -i virbr0 -p tcp --dport 80 -j ACCEPT
        iptables -I INPUT 1 -i virbr0 -p tcp --dport 443 -j ACCEPT

        # Forward DNAT'd web traffic to naraj (NixOS nat module only adds
        # the DNAT rule; FORWARD has to be opened explicitly).
        iptables -I FORWARD 1 -i eno1 -o virbr0 -d ${narajVMip} -p tcp --dport 80  -j ACCEPT
        iptables -I FORWARD 1 -i eno1 -o virbr0 -d ${narajVMip} -p tcp --dport 443 -j ACCEPT
        iptables -I FORWARD 1 -i virbr0 -o eno1 -s ${narajVMip} -p tcp --sport 80  -j ACCEPT
        iptables -I FORWARD 1 -i virbr0 -o eno1 -s ${narajVMip} -p tcp --sport 443 -j ACCEPT

        # SNAT the return packets so external clients see medano's public IP,
        # not naraj's bridge IP. Without this the reply from naraj has src=
        # 192.168.75.2 which is unroutable on the public internet.
        iptables -t nat -I POSTROUTING 1 -o eno1 -s ${narajVMip} -j SNAT --to-source ${pubv4}

        # Hairpin NAT: connections from other VMs (192.168.75.0/24) to
        # medano's public IP on 80/443 should also DNAT to naraj. Without
        # this, gotosocial on social VM resolving auth.medano.emile.space
        # to medano's public IP fails because the regular DNAT only matches
        # packets coming in on eno1.
        iptables -t nat -I PREROUTING 1 -s 192.168.75.0/24 -d ${pubv4} -p tcp --dport 80  -j DNAT --to-destination ${narajVMip}:80
        iptables -t nat -I PREROUTING 1 -s 192.168.75.0/24 -d ${pubv4} -p tcp --dport 443 -j DNAT --to-destination ${narajVMip}:443

        # Allow virbr0->virbr0 forward for hairpin (when source and dest are
        # both on virbr0 — needs explicit accept on most kernels).
        iptables -I FORWARD 1 -i virbr0 -o virbr0 -d ${narajVMip} -p tcp --dport 80  -j ACCEPT
        iptables -I FORWARD 1 -i virbr0 -o virbr0 -d ${narajVMip} -p tcp --dport 443 -j ACCEPT

        # And SNAT so naraj sees the request as coming from medano, not the
        # originating VM (avoids reply going via wrong path).
        iptables -t nat -I POSTROUTING 1 -o virbr0 -d ${narajVMip} -p tcp --dport 80  -j MASQUERADE
        iptables -t nat -I POSTROUTING 1 -o virbr0 -d ${narajVMip} -p tcp --dport 443 -j MASQUERADE
      '';
    };

    firewall = {
      enable = true;
      interfaces = {
        "eno1" = {
          allowedTCPPorts = [
            22 2222 # ssh
            80 # http
            443 # https
            ircBouncerPort # irc
          ];
          allowedUDPPorts = [
            ircBouncerPort # irc
          ];
        };

        # Allow nfs ports for the virbr1 interface
        # Get ports using:
        #     `rpcinfo -p`
        # From the client:
        #     `nmap -sV --script=nfs-showmount 192.168.33.1`
        #
        # This is done so that the `data` VM can access the `grave/data` zfs
        # pool via NFS
        "virbr0" =
          let
            ports =
              with config.services.nfs.server;
              [
                lockdPort
                mountdPort
                statdPort
              ]
              ++ [
                111 # portmapper
                2049 # nfs
                53 # dns
              ];
          in
          {
            allowedTCPPorts = ports ++ [
              80
              443
            ];
            allowedUDPPorts = ports;
          };

        # Allow nfs ports for the virbr1 interface
        # Get ports using:
        #     `rpcinfo -p`
        # From the client:
        #     `nmap -sV --script=nfs-showmount 192.168.33.1`
        #
        # This is done so that the `arr` VM can access the `grave/media` zfs
        # pool via NFS
        "virbr1" =
          let
            ports =
              with config.services.nfs.server;
              [
                lockdPort
                mountdPort
                statdPort
              ]
              ++ [
                111 # portmapper
                2049 # nfs
              ]
              ++ [
                ircBouncerPort # irc
              ];
          in
          {
            allowedTCPPorts = ports ++ [
              80
              443
            ];
            allowedUDPPorts = ports;
          };

        "tailscale0" = {
          allowedTCPPorts = [
            80 # http
            443 # https
            8080 # Guacamole
          ];
          allowedUDPPorts = [ ];
        };
      };

      extraCommands = ''
        # Remove all existing custom FORWARD rules for virbr0
        iptables -D FORWARD -i eno1 -o virbr0 -j ACCEPT 2>/dev/null || true
        iptables -D FORWARD -i virbr0 -o eno1 -j ACCEPT 2>/dev/null || true

        # Add clean, specific forwarding rules
        iptables -I FORWARD 1 -i eno1 -o virbr0 -p tcp --dport ${toString ircBouncerPort} -j ACCEPT
        iptables -I FORWARD 1 -i virbr0 -o eno1 -p tcp --sport ${toString ircBouncerPort} -j ACCEPT
        iptables -I FORWARD 1 -i eno1 -o virbr0 -p udp --dport ${toString ircBouncerPort} -j ACCEPT
        iptables -I FORWARD 1 -i virbr0 -o eno1 -p udp --sport ${toString ircBouncerPort} -j ACCEPT

        # General forwarding between interfaces (placed after specific rules)
        iptables -A FORWARD -i eno1 -o virbr0 -j ACCEPT
        iptables -A FORWARD -i virbr0 -o eno1 -j ACCEPT
      '';

    };
  };
}

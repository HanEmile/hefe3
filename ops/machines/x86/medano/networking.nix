{ config, lib, ... }:

let
  pubv4 = "95.217.35.60";
  ircVMip = "192.168.75.5";

  httpPort = "80";
  httpsPort = "80";
  narajVMip = "192.168.75.2";
  vmSubnet = "192.168.75.0/24";

  ircBouncerPort = 6697;

  # ---------- declarative NAT/forward topology ----------
  # Single source of truth for both the iptables rules and the dashboard
  # NAT visualization. Each entry describes one logical ingress flow into a
  # backend VM. dport is the public-facing port (= naraj-facing port for
  # forwards), dest is "<ip>:<port>" of the backend.
  natFlows = [
    {
      name = "https-ingress";
      desc = "External HTTPS to naraj reverse proxy";
      proto = "tcp";
      dport = 443;
      dest  = "${narajVMip}:443";
      hairpin = true; # also DNAT from vmSubnet -> pubv4:443
      snatReturn = true; # SNAT replies on eno1 so external clients see pubv4
    }
    {
      name = "http-ingress";
      desc = "External HTTP to naraj (ACME http-01 + redirect)";
      proto = "tcp";
      dport = 80;
      dest  = "${narajVMip}:80";
      hairpin = true;
      snatReturn = true;
    }
    # IRC bouncer kept as historical example, currently disabled in
    # forwardPorts but the hairpin rule is still installed (harmless).
    {
      name = "ircs";
      desc = "IRC bouncer (hairpin only; primary DNAT is via nixos nat.forwardPorts when re-enabled)";
      proto = "tcp";
      dport = ircBouncerPort;
      dest  = "${ircVMip}:${toString ircBouncerPort}";
      hairpin = true;
      snatReturn = false;
    }
  ];

  # natFlows -> iptables shell snippet.
  mkFlowRules = f:
    let
      destIp = builtins.head (lib.splitString ":" f.dest);
      destPort = builtins.elemAt (lib.splitString ":" f.dest) 1;
    in
    (lib.optionalString f.hairpin ''
      # ${f.name}: hairpin DNAT (VM-originated traffic to public IP)
      iptables -t nat -I PREROUTING 1 -s ${vmSubnet} -d ${pubv4} -p ${f.proto} --dport ${toString f.dport} -j DNAT --to-destination ${f.dest}
    '')
    + (lib.optionalString (destIp == narajVMip) ''
      # ${f.name}: FORWARD chain accept (external -> bridge)
      iptables -I FORWARD 1 -i eno1 -o virbr0 -d ${destIp} -p ${f.proto} --dport ${toString f.dport} -j ACCEPT
      iptables -I FORWARD 1 -i virbr0 -o eno1 -s ${destIp} -p ${f.proto} --sport ${toString destPort} -j ACCEPT
      # ${f.name}: hairpin FORWARD + MASQUERADE
      iptables -I FORWARD 1 -i virbr0 -o virbr0 -d ${destIp} -p ${f.proto} --dport ${toString f.dport} -j ACCEPT
      iptables -t nat -I POSTROUTING 1 -o virbr0 -d ${destIp} -p ${f.proto} --dport ${toString f.dport} -j MASQUERADE
    '')
    + (lib.optionalString f.snatReturn ''
      # ${f.name}: SNAT replies on eno1 to pubv4 so clients see medano's address
      iptables -t nat -I POSTROUTING 1 -o eno1 -s ${destIp} -j SNAT --to-source ${pubv4}
    '');

  natFlowsJSON = builtins.toJSON {
    publicIp = pubv4;
    vmSubnet = vmSubnet;
    externalIface = "eno1";
    bridge = "virbr0";
    flows = natFlows;
  };
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
        # Allow web traffic from VMs to host (used by status-board upstream)
        iptables -I INPUT 1 -i virbr0 -p tcp --dport 80 -j ACCEPT
        iptables -I INPUT 1 -i virbr0 -p tcp --dport 443 -j ACCEPT

        # Generated from natFlows above (see let-binding). Editing this
        # source produces the iptables rules below and the JSON file at
        # /run/nat-flows.json consumed by the status-board NAT visualizer.
      '' + lib.concatMapStrings mkFlowRules natFlows;
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

  environment.etc."nat-flows.json".text = natFlowsJSON;
}

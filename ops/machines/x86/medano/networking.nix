{ config, lib, pkgs, ... }:

let
  pubv4 = "95.217.35.60";
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
    # IRCS: public IRC (Ergo). naraj terminates TLS for irc.emile.space and
    # stream-proxies plaintext IRC onward to the irc VM's Ergo listener. This
    # follows the same ingress path as 80/443 so the destIp==naraj branch in
    # mkFlowRules installs the FORWARD/hairpin/MASQUERADE rules automatically.
    {
      name = "ircs";
      desc = "Public IRC (Ergo) via naraj TLS-terminating stream proxy";
      proto = "tcp";
      dport = ircBouncerPort;
      dest  = "${narajVMip}:${toString ircBouncerPort}";
      hairpin = true;
      snatReturn = true;
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

    # “If DNAT rewrites a packet to an address on a local interface, don’t consume it locally - treat it as routable and forward it.”
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
    # "enp0s31f6" was stale from an older interface naming; on this box the
    # NIC answers as eno1 and the gateway life-cycle silently dropped any
    # IPv6 default route at boot because of the mismatch (2026-11-09).
    defaultGateway6 = {
      address = "fe80::1";
      interface = "eno1";
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
      # (IRC now routes via naraj like 80/443, so no per-irc-VM SNAT entry is needed.)
      internalIPs = [
      ];

      forwardPorts = [
        # Forward external 80/443 to naraj (TLS-terminating ingress VM).
        { destination = "${narajVMip}:80";  proto = "tcp"; sourcePort = 80; }
        { destination = "${narajVMip}:443"; proto = "tcp"; sourcePort = 443; }
        # Forward external IRC (6697) to naraj, which terminates TLS for
        # irc.emile.space and stream-proxies plaintext to the irc VM's Ergo.
        {
          destination = "${narajVMip}:${toString ircBouncerPort}";
          proto = "tcp";
          sourcePort = ircBouncerPort;
        }
        # Kubo swarm on the nomos VM (192.168.75.17) — tcp + quic. The VM
        # announces these public addrs via services.nomos.ipfs.announce so
        # published Trust Anchor state is dialable from the public DHT.
        {
          destination = "192.168.75.17:4001";
          proto = "tcp";
          sourcePort = 4001;
        }
        {
          destination = "192.168.75.17:4001";
          proto = "udp";
          sourcePort = 4001;
        }
      ];

      extraCommands = ''
        # Allow web traffic from VMs to host (used by status-board upstream)
        iptables -I INPUT 1 -i virbr0 -p tcp --dport 80 -j ACCEPT
        iptables -I INPUT 1 -i virbr0 -p tcp --dport 443 -j ACCEPT

        # Kubo swarm on the nomos VM (192.168.75.17). The DNAT half comes from
        # networking.nat.forwardPorts above; mkFlowRules only generates FORWARD
        # accepts for naraj-destined flows, so this one needs them explicitly.
        iptables -I FORWARD 1 -i eno1 -o virbr0 -d 192.168.75.17 -p tcp --dport 4001 -j ACCEPT
        iptables -I FORWARD 1 -i eno1 -o virbr0 -d 192.168.75.17 -p udp --dport 4001 -j ACCEPT
        iptables -I FORWARD 1 -i virbr0 -o eno1 -s 192.168.75.17 -p tcp --sport 4001 -j ACCEPT
        iptables -I FORWARD 1 -i virbr0 -o eno1 -s 192.168.75.17 -p udp --sport 4001 -j ACCEPT

        # Generated from natFlows above (see let-binding). Editing this
        # source produces the iptables rules below and the JSON file at
        # /etc/nat-flows.json consumed by the status-board NAT visualizer.
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
              8090 # status-board listens on 192.168.75.1:8090 for naraj
              9100 # node-exporter, scraped by irc's uptimebot/diskwatch over the bridge
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
          ];
          allowedUDPPorts = [ ];
        };
      };

      extraCommands = ''
        # Private ULA over the bridge for v6-stable gatewaying of naraj
        # (naraj uses 2a01:4f9:2b:2d45::2 as its public v6 face; medano
        # still owns ::1 itself - 2026-11-09).
        ${pkgs.iproute2}/bin/ip -6 addr replace fdc6:7a0f:c0ff::1/64 dev virbr0
        ${pkgs.iproute2}/bin/ip -6 route replace 2a01:4f9:2b:2d45::2/128 dev virbr0
        ${pkgs.iproute2}/bin/ip -6 neigh replace proxy 2a01:4f9:2b:2d45::2 dev eno1
        # and bridge the public 80/443 hit through to naraj's v6, mirroring
        # the v4 NAT arrangement
        ${pkgs.iptables}/bin/ip6tables -C INPUT -i virbr0 -p tcp --dport 80 -j ACCEPT 2>/dev/null \
          || ${pkgs.iptables}/bin/ip6tables -I INPUT 1 -i virbr0 -p tcp --dport 80 -j ACCEPT
        ${pkgs.iptables}/bin/ip6tables -C INPUT -i virbr0 -p tcp --dport 443 -j ACCEPT 2>/dev/null \
          || ${pkgs.iptables}/bin/ip6tables -I INPUT 1 -i virbr0 -p tcp --dport 443 -j ACCEPT
        ${pkgs.iptables}/bin/ip6tables -C INPUT -i virbr0 -p tcp --dport 6697 -j ACCEPT 2>/dev/null \
          || ${pkgs.iptables}/bin/ip6tables -I INPUT 1 -i virbr0 -p tcp --dport 6697 -j ACCEPT
        ${pkgs.iptables}/bin/ip6tables -t nat -C PREROUTING -i eno1 -p tcp --dport 80 -j DNAT \
          --to-destination [2a01:4f9:2b:2d45::2]:80 2>/dev/null \
          || ${pkgs.iptables}/bin/ip6tables -t nat -I PREROUTING -i eno1 -p tcp --dport 80 -j DNAT \
            --to-destination [2a01:4f9:2b:2d45::2]:80
        ${pkgs.iptables}/bin/ip6tables -t nat -C PREROUTING -i eno1 -p tcp --dport 443 -j DNAT \
          --to-destination [2a01:4f9:2b:2d45::2]:443 2>/dev/null \
          || ${pkgs.iptables}/bin/ip6tables -t nat -I PREROUTING -i eno1 -p tcp --dport 443 -j DNAT \
            --to-destination [2a01:4f9:2b:2d45::2]:443
        ${pkgs.iptables}/bin/ip6tables -t nat -C PREROUTING -i eno1 -p tcp --dport 6697 -j DNAT \
          --to-destination [2a01:4f9:2b:2d45::2]:6697 2>/dev/null \
          || ${pkgs.iptables}/bin/ip6tables -t nat -I PREROUTING -i eno1 -p tcp --dport 6697 -j DNAT \
            --to-destination [2a01:4f9:2b:2d45::2]:6697
        # (no v4-style SNAT needed - replies route back through it default)
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

  # Durable workaround for the LIBVIRT_FWI gotcha (see CLAUDE.md and the
  # firewall.extraCommands comment below). libvirt installs its LIBVIRT_FWI
  # chain into FORWARD with `-I FORWARD 1` *after* nixos's firewall.service
  # has run, which pushes our public-ingress ACCEPT rules below libvirt's
  # blanket REJECT. The 2026-05-29 incident hit this when a fan-out of VM
  # redeploys re-triggered libvirtd and broke public emile.space until
  # firewall.service was restarted.
  #
  # This unit re-asserts the same `-I FORWARD 1` inserts via the libvirtd
  # lifecycle: PartOf=libvirtd.service makes it stop+start whenever libvirtd
  # does, so the rules end back on top of the chain without manual
  # intervention. It is idempotent (each insert is preceded by a tolerant
  # `-D ... || true`).
  systemd.services.medano-forward-fixup = {
    description = "Re-assert FORWARD/NAT inserts above libvirt's LIBVIRT_FWI chain";
    wantedBy = [ "multi-user.target" "libvirtd.service" ];
    after = [ "firewall.service" "libvirtd.service" ];
    partOf = [ "libvirtd.service" ];
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
      ExecStart = pkgs.writeShellScript "medano-forward-fixup" (''
        set -eu
        # Bare `iptables ...` calls (from mkFlowRules) need iptables on PATH.
        export PATH=${pkgs.iptables}/bin:$PATH
        IPT=${pkgs.iptables}/bin/iptables

        # General virbr0 <-> eno1 forwarding (mirrors firewall.extraCommands).
        "$IPT" -D FORWARD -i eno1 -o virbr0 -j ACCEPT 2>/dev/null || true
        "$IPT" -D FORWARD -i virbr0 -o eno1 -j ACCEPT 2>/dev/null || true
        "$IPT" -I FORWARD 1 -i eno1 -o virbr0 -j ACCEPT
        "$IPT" -I FORWARD 1 -i virbr0 -o eno1 -j ACCEPT

        # IRC bouncer (matches firewall.extraCommands).
        "$IPT" -I FORWARD 1 -i eno1 -o virbr0 -p tcp --dport ${toString ircBouncerPort} -j ACCEPT
        "$IPT" -I FORWARD 1 -i virbr0 -o eno1 -p tcp --sport ${toString ircBouncerPort} -j ACCEPT
        "$IPT" -I FORWARD 1 -i eno1 -o virbr0 -p udp --dport ${toString ircBouncerPort} -j ACCEPT
        "$IPT" -I FORWARD 1 -i virbr0 -o eno1 -p udp --sport ${toString ircBouncerPort} -j ACCEPT
      '' + lib.concatMapStrings mkFlowRules natFlows);
    };
  };

  environment.etc."nat-flows.json".text = natFlowsJSON;
}

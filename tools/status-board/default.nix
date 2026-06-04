# status-board - internal dashboard for the medano fleet.
{ pkgs, lib ? pkgs.lib, ... }:

let
  package = pkgs.buildGoModule {
    pname = "status-board";
    version = "0.1.0";
    src = lib.cleanSource ./.;
    vendorHash = null;
    nativeBuildInputs = [ pkgs.bash pkgs.python3 ];
    # WCAG AA contrast check runs at build time so palette regressions fail
    # nix-build rather than ship to the fleet. Runs against both dark and
    # light @media branches. See tools/status-board/check-contrast.sh.
    postPatch = ''
      patchShebangs check-contrast.sh
      bash check-contrast.sh
      bash check-contrast.sh --mode=light
    '';
  };

  module = { hefe }:
    { config, lib, pkgs, ... }:
    let
      ipam = hefe.ops.ipam;
      defaultVms = lib.attrNames (lib.filterAttrs (n: _: n != "medano") ipam.default);
      privateVms = lib.attrNames (lib.filterAttrs (n: _: n != "medano") ipam.private);
      allVms = defaultVms ++ privateVms;
      ipamLookup = name:
        if ipam.default ? "${name}" then ipam.default."${name}"
        else if ipam.private ? "${name}" then ipam.private."${name}"
        else throw "status-board: no IPAM for ${name}";

      vmInventory = pkgs.writeText "vm-inventory.json" (builtins.toJSON (
        map (n: {
          name = n;
          ip = (ipamLookup n).v4;
          bridge = if ipam.default ? "${n}" then "default" else "private";
        }) allVms
      ));

      # Per-VM open ports: union of generic + per-interface lists.
      # Some VMs aren't in ops.nixos (sb1..3 are bare bootstraps). For those,
      # we fall back to an empty list and rely on the vm-base default
      # (node-exporter 9100 on enp1s0).
      portsForVM = name:
        let
          n = hefe.ops.nixos."${name}" or null;
          fw = if n != null then n.config.networking.firewall else null;
          generic = if fw != null then fw.allowedTCPPorts else [];
          enp1 = if fw != null && fw.interfaces ? enp1s0
                 then fw.interfaces.enp1s0.allowedTCPPorts else [];
          tail = if fw != null && fw.interfaces ? tailscale0
                 then fw.interfaces.tailscale0.allowedTCPPorts else [];
        in lib.unique (generic ++ enp1 ++ tail);

      backupsEnabledForVM = name:
        let n = hefe.ops.nixos."${name}" or null;
        in if n == null then false
           else (n.config.vmBackups.enable or false);

      backupPathsForVM = name:
        let n = hefe.ops.nixos."${name}" or null;
        in if n == null then []
           else (n.config.vmBackups.paths or []);

      # Backup targets per VM. Modeled as a list so adding a second target
      # (e.g. a cold archive) becomes a config-only change here. Today every
      # configured VM has exactly one target: the storagebox restic repo.
      backupTargetsForVM = name:
        if backupsEnabledForVM name then [
          {
            kind = "restic";
            label = "storagebox-bx11";
            repo = "/mnt/storagebox-bx11/backup/${name}";
          }
        ] else [];

      # Hand-edited cross-VM relationships. We can't reliably derive these
      # from per-VM evaluation because they cross OIDC, NFS, restic, etc.
      # Keep the list small and meaningful. Edges show up as labeled
      # connections in the graph and are click-focusable.
      relationships = [
        # OIDC: consumer -> auth
        { from = "rss";      to = "auth"; kind = "oidc"; via = "https://sso.emile.space"; }
        { from = "md";       to = "auth"; kind = "oidc"; via = "https://sso.emile.space"; }
        { from = "social";   to = "auth"; kind = "oidc"; via = "https://sso.emile.space"; }
        { from = "photo";    to = "auth"; kind = "oidc"; via = "https://sso.emile.space"; }
        { from = "data";     to = "auth"; kind = "oidc"; via = "https://sso.emile.space"; }

        # Status board (on medano host) -> auth (via naraj forward-auth).
        { from = "medano"; to = "auth"; kind = "forward-auth"; via = "naraj nginx"; }

        # NFS exports off medano /grave.
        { from = "photo"; to = "medano"; kind = "nfs"; via = "/grave/photos"; }
        { from = "data";  to = "medano"; kind = "nfs"; via = "/grave/data";   }

        # All VMs and medano back up to the storagebox.
        { from = "medano"; to = "storagebox"; kind = "restic"; via = "cifs"; }
        { from = "naraj";  to = "storagebox"; kind = "restic"; via = "cifs"; }
        { from = "auth";   to = "storagebox"; kind = "restic"; via = "cifs"; }
        { from = "md";     to = "storagebox"; kind = "restic"; via = "cifs"; }
        { from = "photo";  to = "storagebox"; kind = "restic"; via = "cifs"; }
        { from = "social"; to = "storagebox"; kind = "restic"; via = "cifs"; }
        { from = "rss";    to = "storagebox"; kind = "restic"; via = "cifs"; }
        { from = "data";   to = "storagebox"; kind = "restic"; via = "cifs"; }
        { from = "tmp";    to = "storagebox"; kind = "restic"; via = "cifs"; }
      ];

      # Public-facing ingress mappings - hostnames terminated at naraj.
      # Kept lean: name + upstream VM + upstream service:port + tls.
      ingressList = [
        { host = "emile.space";       vm = "naraj"; service = "static";       port = 80;   tls = true; }
        { host = "tmp.emile.space";   vm = "tmp";   service = "nginx";        port = 80;   tls = true; }
        { host = "md.emile.space";    vm = "md";    service = "hedgedoc";     port = 9091; tls = true; }
        { host = "sso.emile.space";   vm = "auth";  service = "authelia";     port = 9091; tls = true; }
        { host = "photo.emile.space"; vm = "photo"; service = "immich";       port = 9091; tls = true; }
        { host = "social.emile.space";vm = "social";service = "gotosocial";   port = 3004; tls = true; }
        { host = "amaltheea.emile.space"; vm = "amalthea"; service = "backend"; port = 8080; tls = true; }
        { host = "status.emile.space";vm = "medano";service = "status-board"; port = 8090; tls = true; }
        # tailscale-only paths bypass naraj.
        { host = "rss.pinto-pike.ts.net";  vm = "rss";  service = "miniflux"; port = 8080; tls = false; }
        { host = "data.pinto-pike.ts.net"; vm = "data"; service = "sftpgo";   port = 8080; tls = false; }
      ];

      vmGraph = builtins.toJSON {
        vms = map (n: {
          name = n;
          ip = (ipamLookup n).v4;
          bridge = if ipam.default ? "${n}" then "virbr0" else "virbr1";
          ports = portsForVM n;
          backupsEnabled = backupsEnabledForVM n;
          backupPaths = backupPathsForVM n;
          backupTargets = backupTargetsForVM n;
        }) allVms;
        # Static list of ZFS pools to scrape via `zpool list -Hp` at runtime.
        zpools = [ "bpool" "rpool" "grave" ];
        inherit relationships;
        ingress = ingressList;
        externalIp = "95.217.35.60";
      };

      vmGraphFile = pkgs.writeText "vm-graph.json" vmGraph;
    in
    {
      environment.etc."status-board-graph.json".source = vmGraphFile;

      systemd.services.status-board = {
        description = "Internal medano fleet dashboard";
        wantedBy = [ "multi-user.target" ];
        after = [ "network.target" "libvirtd.service" ];
        path = [ pkgs.libvirt pkgs.coreutils pkgs.zfs ];
        # RequiresMountsFor pulls in mnt-storagebox\x2dbx11.automount as
        # a dependency AND - crucially - tells systemd to propagate the
        # autofs trigger plus any subsequent CIFS mount underneath it
        # into the unit's mount namespace. Without this the service can
        # end up with a private namespace snapshot taken before the
        # CIFS share is ever mounted (observed 2026-06-01: service last
        # restarted May 29, /mnt/storagebox-bx11 idle-unmounted shortly
        # after, the service's namespace then stayed pinned to an empty
        # autofs view - every os.Stat on /mnt/storagebox-bx11/backup/<vm>
        # returned ENOENT and the dashboard showed "no repo yet" for
        # every configured VM, while the host's root namespace had the
        # share mounted normally).
        unitConfig.RequiresMountsFor = "/mnt/storagebox-bx11";
        environment = {
          STATUS_BOARD_INVENTORY = "${vmInventory}";
          STATUS_BOARD_GRAPH = "/etc/status-board-graph.json";
          STATUS_BOARD_LISTEN = "192.168.75.1:8090";
        };
        serviceConfig = {
          ExecStart = "${package}/bin/status-board";
          Restart = "on-failure";
          RestartSec = 5;
          User = "root";
          StateDirectory = "status-board";
          StateDirectoryMode = "0750";
        };
      };
    };
in
{
  inherit package module;
}

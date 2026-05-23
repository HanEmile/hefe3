{
  hefe,
  pkgs,
  system,
  lib,
  ...
}@args:

let
  nixosFor =
    machineName:
    let
      sources = hefe.third_party;

      nixos = sources."nixos-25.11";
      pkgs = import nixos {
        system = "x86_64-linux";
        config.allowUnfree = true;
        overlays = [
          (final: prev: {
            makhor = final.callPackage hefe.ops.pkgs.makhor {
              inherit pkgs;
            };
          })
          (
            final: prev:
            let
              lateShSrc = sources."late-sh".outPath + "/default.nix";
              rustMinimalPlatform =
                let
                  platform = pkgs.rust-bin.stable.latest.minimal;
                in
                pkgs.makeRustPlatform {
                  rustc = platform;
                  cargo = platform;
                };
            in
            {
              late-sh = pkgs.callPackage lateShSrc {
                rustPlatform = rustMinimalPlatform;
                gitRev = "main";
              };
            }
          )
          (import (sources."rust-overlay"))
        ];
      };

      lib = import (nixos + "/lib");

      nixVirtSrc = sources."NixVirt".outPath + "/flake.nix";
      nixVirtFlake = import nixVirtSrc;
      nixVirtOutput = (
        nixVirtFlake.outputs {
          self = null;
          nixpkgs = nixos.outPath;
        }
      );
      nixVirtModule = nixVirtOutput.nixosModules.default;
      nixVirtLib.lib = nixVirtOutput.lib;

      agenix = (sources."agenix".outPath + "/modules/age.nix");
    in
    import (nixos + "/nixos/lib/eval-config.nix") {
      inherit lib system;
      specialArgs = {
        inherit args sources;
        nixvirt = nixVirtLib;
      };
      modules = [
        machineName # //ops/machines/x86/<name>/default.nix
        nixVirtModule # virtualisation
        agenix # secrets
        { nixpkgs.pkgs = pkgs; }
        (import ./modules/late-sh.nix)
      ];
    };

  build = hostname: ''
    ${pkgs.nix}/bin/nix-build \
      -A ops.nixos.${hostname}.toplevel \
      --system "x86_64-linux" \
      -j 0 \
      --cores 0 \
      --show-trace \
      -v
  '';

  buildScriptFor =
    hostname:
    pkgs.writeShellScriptBin "build" ''
      set -ue

      echo "[STEP 1/4]: Building host"
      ${build hostname}
    '';

  # If sshTarget is set, deploy talks to that address directly. Otherwise
  # uses the bare hostname (relies on ~/.ssh/config aliases).
  # sshJump (optional): a host alias to ProxyJump through (e.g. "medano" for
  # VMs that are only reachable via the hypervisor).
  deployScriptForOpts =
    { hostname,
      host_suffix ? "",
      sshTarget ? null,
      sshJump ? null,
    }:
    let
      target = if sshTarget != null then sshTarget else "${hostname}${host_suffix}";
      sshOpts = if sshJump != null
                then "-o ProxyJump=root@${sshJump} -o StrictHostKeyChecking=accept-new"
                else "";
    in
    pkgs.writeShellScriptBin "deploy" ''
      set -ue

      echo "[STEP 1/4]: Building host ${hostname}"
      ${build hostname}

      echo "[STEP 2/4]: Copying build result to host (target=${target})"
      NIX_SSHOPTS="${sshOpts}" nix-copy-closure \
        --to root@${target} \
        --use-substitutes \
        --verbose \
        --gzip \
        ./result

      echo "[STEP 3/4]: Setting the profile"
      ssh ${sshOpts} root@${target} \
        nix-env \
          -p /nix/var/nix/profiles/system \
          --set $(readlink ./result)

      echo "[STEP 4/4]: Switching to configuration"
      ssh ${sshOpts} root@${target} \
        /nix/var/nix/profiles/system/bin/switch-to-configuration switch
    '';

  deployScriptFor =
    hostname:
    host_suffix:
    deployScriptForOpts { inherit hostname host_suffix; };

  # VMs: pull primary IP from IPAM (default bridge first, then private bridge),
  # and use medano as the ProxyJump.
  deployVmScriptFor =
    name:
    let
      ipam =
        hefe.ops.ipam.default."${name}" or
        hefe.ops.ipam.private."${name}" or
        null;
      target =
        if ipam != null then ipam.v4
        else throw "deployVmScriptFor: no IPAM entry for ${name}";
    in
    deployScriptForOpts {
      hostname = name;
      sshTarget = target;
      sshJump = "medano";
    };

  # Build a bootable qcow2 image from a VM's NixOS config.
  imageFor =
    name:
    let
      sources = hefe.third_party;
      nixos = sources."nixos-25.11";
      linuxPkgs = import nixos {
        system = "x86_64-linux";
        config.allowUnfree = true;
      };
      linuxLib = import (nixos + "/lib");
      nixosCfg = (nixosFor hefe.ops.vms.x86."${name}").config;
    in
    (import ./lib/mkVmImage.nix) {
      pkgs = linuxPkgs;
      lib = linuxLib;
      inherit nixos;
      nixosConfig = nixosCfg;
      inherit name;
    };

  # Script that builds the qcow2 image for a VM and uploads it to medano
  # at /keep/pools/vmpool/<name>.qcow2. Refuses to overwrite a running VM's
  # disk (libvirt holds the file open; operator must shut down first).
  deployImageScriptFor =
    name:
    pkgs.writeShellScriptBin "deploy-image" ''
      set -ue

      echo "[STEP 1/4]: Building qcow2 image for ${name}"
      export NIXPKGS_ALLOW_UNSUPPORTED_SYSTEM=1
      ${pkgs.nix}/bin/nix-build \
        -A ops.nixos.${name}.image \
        --system "x86_64-linux" \
        -j 0 \
        --cores 0 \
        --show-trace

      IMG=$(readlink ./result)/${name}.qcow2
      if [ ! -f "$IMG" ]; then
        IMG=$(readlink ./result)/nixos.qcow2
      fi
      echo "Image at: $IMG"
      ls -lh "$IMG"

      echo "[STEP 2/4]: Confirming the target VM is not running on medano"
      if ssh root@medano "virsh domstate ${name} 2>/dev/null" | grep -q running; then
        echo "ERROR: VM '${name}' is running on medano. Shut it down first." >&2
        exit 1
      fi

      echo "[STEP 3/4]: Uploading image as /keep/pools/vmpool/${name}.qcow2.new"
      ${pkgs.openssh}/bin/scp "$IMG" root@medano:/keep/pools/vmpool/${name}.qcow2.new

      echo "[STEP 4/4]: Atomically replacing /keep/pools/vmpool/${name}.qcow2"
      ssh root@medano \
        "mv -f /keep/pools/vmpool/${name}.qcow2.new /keep/pools/vmpool/${name}.qcow2"

      echo
      echo "Image deployed. Start with: ssh root@medano 'virsh start ${name}'"
    '';

  # type being "machines" or "vms"
  conf = name: type: {
    "${name}" =
      {
        config = (nixosFor hefe.ops."${type}".x86."${name}").config;
        toplevel = (nixosFor hefe.ops."${type}".x86."${name}").config.system.build.toplevel;
        build = buildScriptFor "${name}";
        # machines (bare-metal hosts): just hostname or hostname + .pinto-pike.ts.net
        # vms: route through medano with the IPAM IP
        deploy =
          if type == "vms"
          then deployVmScriptFor name
          else deployScriptFor name "";
        deploy_ts = deployScriptFor name ".pinto-pike.ts.net";
      }
      // (lib.optionalAttrs (type == "vms") {
        image = imageFor name;
        deploy_image = deployImageScriptFor name;
      });
  };

  machine = name: conf name "machines";
  vm = name: conf name "vms";

  abc =
    func: dir:
    lib.attrsets.mergeAttrsList (
      lib.attrsets.attrValues (
        lib.attrsets.mergeAttrsList (
          builtins.map (x: { "${x}" = (func "${x}"); }) (
            builtins.attrNames (lib.attrsets.filterAttrs (k: v: v == "directory") (builtins.readDir dir))
          )
        )
      )
    );

  machines = abc machine ./machines/x86;
  vms = abc vm ./vms/x86;

  withAll = keys: keys // { all = builtins.attrValues keys; };
in
withAll (machines // vms)

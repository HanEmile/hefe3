{
  hefe,
  pkgs,
  system,
  lib,
  ...
}@args:

let
  sources = hefe.third_party;
  nixos = sources."nixos-26.05";

  nixosFor =
    machineName: extraModules:
    let
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
      inherit lib;
      system = "x86_64-linux";
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
      ] ++ extraModules;
    };

  # ── armv6l-linux (BMC Raspberry Pi) NixOS evaluation ──────────────────
  #
  # Cross-compilation: the NixOS config targets armv6l-linux (RPi 1) but
  # all compilation happens on x86_64-linux (medano, the remote builder).
  # This avoids the need for an armv6l builder or binfmt emulation.

  nixosLib = import (nixos + "/lib");

  nixosForBmc =
    machineDef:
    let
      agenix = (sources."agenix".outPath + "/modules/age.nix");
    in
    import (nixos + "/nixos/lib/eval-config.nix") {
      lib = nixosLib;
      system = null;
      specialArgs = {
        inherit hefe args sources;
      };
      modules = [
        "${nixos}/nixos/modules/installer/sd-card/sd-image-raspberrypi.nix"
        machineDef
        agenix
        {
          # Cross-compilation: build on x86_64-linux, target armv6l-linux.
          nixpkgs.buildPlatform.system = "x86_64-linux";
          nixpkgs.hostPlatform.system = "armv6l-linux";
          nixpkgs.config = {
            allowUnfree = true;
            allowUnsupportedSystem = true;
            allowBroken = true;
          };
          sdImage.compressImage = false;
        }
      ];
    };


  build = hostname: ''
    ${pkgs.nix}/bin/nix-build \
      -A ops.nixos.${hostname}.toplevel \
      --system "x86_64-linux" \
      -j 0 \
      --cores 0 \
      --keep-going \
      --builders-use-substitutes \
      --show-trace \
      -v
  '';

  instantiate = hostname: ''
    ${pkgs.nix}/bin/nix-instantiate \
      -A ops.nixos.${hostname}.toplevel \
      --system "x86_64-linux" \
      --show-trace
  '';

  buildScriptFor =
    hostname:
    pkgs.writeShellScriptBin "build" ''
      set -ue

      echo "[STEP 1/4]: Building host"
      ${build hostname}
    '';

  deployScriptForOpts =
    { hostname,
      host_suffix ? "",
      sshTarget ? null,
      sshJump ? null,
      buildOnTarget ? false,
    }:
    let
      target = if sshTarget != null then sshTarget else "${hostname}${host_suffix}";
      sshOpts = if sshJump != null
                then "-o ProxyJump=root@${sshJump} -o StrictHostKeyChecking=accept-new"
                else "";
    in
    if buildOnTarget then
    pkgs.writeShellScriptBin "deploy" ''
      set -ue

      echo "[STEP 1/4]: Evaluating ${hostname} on caladan (no local build)"
      DRV=$(${instantiate hostname})
      echo "drv: $DRV"

      echo "[STEP 2/4]: Shipping the derivation to ${target} (jump=${toString sshJump})"
      NIX_SSHOPTS="${sshOpts}" ${pkgs.nix}/bin/nix copy \
        --derivation \
        --to "ssh-ng://root@${target}?compress=true" \
        --no-check-sigs \
        --verbose \
        "$DRV"

      echo "[STEP 3/4]: Building on ${target} + setting the profile"
      OUT=$(ssh ${sshOpts} root@${target} \
        "nix-store --realise \"$DRV\"")
      echo "built: $OUT"
      ssh ${sshOpts} root@${target} \
        nix-env -p /nix/var/nix/profiles/system --set "$OUT"

      echo "[STEP 4/4]: Switching to configuration"
      ssh ${sshOpts} root@${target} \
        /nix/var/nix/profiles/system/bin/switch-to-configuration switch
    ''
    else
    pkgs.writeShellScriptBin "deploy" ''
      set -ue

      echo "[STEP 1/4]: Building host ${hostname}"
      ${build hostname}

      echo "[STEP 2/4]: Copying build result to host (target=${target} jump=${toString sshJump})"
      NIX_SSHOPTS="${sshOpts}" ${pkgs.nix}/bin/nix copy \
        --to "ssh-ng://root@${target}?compress=true" \
        --substitute-on-destination \
        --no-check-sigs \
        --verbose \
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
    deployScriptForOpts { inherit hostname host_suffix; buildOnTarget = true; };

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

  imageFor =
    name:
    let
      linuxPkgs = import nixos {
        system = "x86_64-linux";
        config.allowUnfree = true;
      };
      linuxLib = import (nixos + "/lib");
      nixosCfg = (nixosFor hefe.ops.vms.x86."${name}" (homeModulesFor name "vms")).config;
    in
    (import ./lib/mkVmImage.nix) {
      pkgs = linuxPkgs;
      lib = linuxLib;
      inherit nixos;
      nixosConfig = nixosCfg;
      inherit name;
    };

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

  homeModulesFor =
    name: type:
    let
      homeNix =
        hefe.path.origSrc + "/ops/${type}/x86/${name}/home.nix";
    in
    if builtins.pathExists homeNix then
      [
        (hefe.third_party."home-manager".outPath + "/nixos")
        homeNix
      ]
    else
      [ ];

  # ── BMC build / deploy / flash / qemu scripts ─────────────────────────

  bmcBuildScript =
    name:
    pkgs.writeShellScriptBin "build" ''
      set -ue
      echo "Building SD card image for ${name} (armv6l-linux)..."
      echo ""
      echo "Prerequisites:"
      echo "  - A Linux builder that can handle armv6l-linux"
      echo "  - Either: binfmt emulation on the nix-darwin linux-builder VM"
      echo "    or: a remote armv6l/aarch64 builder with binfmt"
      echo ""
      ${pkgs.nix}/bin/nix-build \
        -A ops.nixos.${name}.sdImage \
        -j 0 \
        --cores 0 \
        --keep-going \
        --builders-use-substitutes \
        --show-trace \
        -v
      echo ""
      echo "SD image built:"
      ls -lh ./result/sd-image/
    '';

  bmcFlashScript =
    name:
    pkgs.writeShellScriptBin "flash" ''
      set -ue
      if [ -z "''${1:-}" ]; then
        echo "Usage: $(basename "$0") /dev/diskN"
        echo ""
        echo "On macOS: diskutil list   (use /dev/rdiskN for speed)"
        exit 1
      fi
      DISK="$1"

      echo "Building SD card image for ${name}..."
      ${pkgs.nix}/bin/nix-build \
        -A ops.nixos.${name}.sdImage \
        -j 0 --cores 0 --keep-going --builders-use-substitutes --show-trace

      IMG=$(find ./result/sd-image/ -name '*.img' -o -name '*.img.zst' | head -1)
      [ -n "$IMG" ] || { echo "ERROR: no image found" >&2; exit 1; }

      echo ""
      echo "Image: $IMG"
      echo "Target: $DISK"
      read -p "This will ERASE $DISK. Continue? [y/N] " confirm
      [ "$confirm" = "y" ] || exit 1

      if [[ "$IMG" == *.zst ]]; then
        ${pkgs.zstd}/bin/zstd -d "$IMG" --stdout | sudo dd of="$DISK" bs=64K status=progress
      else
        sudo dd if="$IMG" of="$DISK" bs=64K status=progress
      fi
      echo "Done. Eject the SD card and boot your Pi."
    '';

  bmcQemuScript =
    name:
    pkgs.writeShellScriptBin "qemu-bmc" ''
      set -ue

      echo "Building SD card image for ${name}..."
      ${pkgs.nix}/bin/nix-build \
        -A ops.nixos.${name}.sdImage \
        -j 0 --cores 0 --keep-going --builders-use-substitutes --show-trace

      IMG=$(find ./result/sd-image/ -name '*.img' | head -1)
      [ -n "$IMG" ] || { echo "ERROR: no .img found (compression enabled?)" >&2; exit 1; }

      WORKDIR=$(mktemp -d)
      trap "rm -rf $WORKDIR" EXIT

      echo "Copying image to $WORKDIR..."
      cp "$IMG" "$WORKDIR/sd.img"
      ${pkgs.qemu}/bin/qemu-img resize "$WORKDIR/sd.img" 2G

      echo ""
      echo "Starting QEMU (versatilepb, arm1176, 256MB RAM)..."
      echo "  SSH forwarded: localhost:2222 -> guest:22"
      echo "  Press Ctrl-A X to exit QEMU."
      echo ""

      exec ${pkgs.qemu}/bin/qemu-system-arm \
        -M versatilepb \
        -cpu arm1176 \
        -m 256 \
        -drive file="$WORKDIR/sd.img",format=raw,if=sd \
        -nographic \
        -serial stdio \
        -no-reboot \
        -net nic \
        -net user,hostfwd=tcp::2222-:22
    '';

  bmcDeployScript =
    name:
    pkgs.writeShellScriptBin "deploy" ''
      set -ue

      echo "[STEP 1/4]: Building ${name} toplevel"
      ${pkgs.nix}/bin/nix-build \
        -A ops.nixos.${name}.toplevel \
        -j 0 --cores 0 --keep-going --builders-use-substitutes --show-trace -v

      echo "[STEP 2/4]: Copying closure to ${name}"
      NIX_SSHOPTS="" ${pkgs.nix}/bin/nix copy \
        --to "ssh-ng://root@${name}?compress=true" \
        --no-check-sigs \
        --verbose \
        ./result

      echo "[STEP 3/4]: Setting the profile"
      ssh root@${name} \
        nix-env -p /nix/var/nix/profiles/system --set $(readlink ./result)

      echo "[STEP 4/4]: Switching to configuration"
      ssh root@${name} \
        /nix/var/nix/profiles/system/bin/switch-to-configuration switch
    '';

  # ── Per-type conf builders ─────────────────────────────────────────────

  # type being "machines" or "vms"
  conf = name: type:
    let
      homeModules = homeModulesFor name type;
    in
    {
    "${name}" =
      {
        config = (nixosFor hefe.ops."${type}".x86."${name}" homeModules).config;
        toplevel = (nixosFor hefe.ops."${type}".x86."${name}" homeModules).config.system.build.toplevel;
        build = buildScriptFor "${name}";
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

  bmcConf =
    name:
    {
      "${name}" = {
        config = (nixosForBmc hefe.ops.machines.aarch64."${name}").config;
        toplevel = (nixosForBmc hefe.ops.machines.aarch64."${name}").config.system.build.toplevel;
        sdImage = (nixosForBmc hefe.ops.machines.aarch64."${name}").config.system.build.sdImage;
        build = bmcBuildScript name;
        flash = bmcFlashScript name;
        qemu = bmcQemuScript name;
        deploy = bmcDeployScript name;
      };
    };

  machine = name: conf name "machines";
  vm = name: conf name "vms";

  # ── Directory scanners ─────────────────────────────────────────────────

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

  abcFiltered =
    func: dir: filter:
    lib.attrsets.mergeAttrsList (
      lib.attrsets.attrValues (
        lib.attrsets.mergeAttrsList (
          builtins.map (x: { "${x}" = (func "${x}"); }) (
            builtins.filter filter (
              builtins.attrNames (lib.attrsets.filterAttrs (k: v: v == "directory") (builtins.readDir dir))
            )
          )
        )
      )
    );

  machines = abc machine ./machines/x86;
  vms = abc vm ./vms/x86;
  bmcs = abcFiltered bmcConf ./machines/aarch64 (n: lib.hasSuffix "-bmc" n);

  withAll = keys: keys // { all = builtins.attrValues keys; };
in
withAll (machines // vms // bmcs)

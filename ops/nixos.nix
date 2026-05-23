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

  deployScriptFor =
    hostname: # e.g. "medano"
    host_suffix: # e.g. ".pinto-pike.ts.net" (mind the leading dot!)
    pkgs.writeShellScriptBin "deploy" ''
      set -ue

      echo "[STEP 1/4]: Building host ${hostname}"
      ${build hostname}

      echo "[STEP 2/4]: Copying build result to host"
      nix-copy-closure \
        --to root@${hostname}${host_suffix} \
        --use-substitutes \
        --verbose \
        --gzip \
        ./result

      echo "[STEP 3/4]: Setting the profile"
      ssh root@${hostname}${host_suffix} \
        nix-env \
          -p /nix/var/nix/profiles/system \
          --set $(readlink ./result)

      echo "[STEP 4/4]: Switching to configuration"
      ssh root@${hostname}${host_suffix} \
        /nix/var/nix/profiles/system/bin/switch-to-configuration switch
    '';

  # type being "machines" or "vms"
  conf = name: type: {
    "${name}" = {
      config = (nixosFor hefe.ops."${type}".x86."${name}").config;
      toplevel = (nixosFor hefe.ops."${type}".x86."${name}").config.system.build.toplevel;
      build = buildScriptFor "${name}";
      deploy = deployScriptFor "${name}" "";
      deploy_ts = deployScriptFor "${name}" ".pinto-pike.ts.net";
    };
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

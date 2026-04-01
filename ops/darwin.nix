# This file is responsible for building nix-darwin configurations.
{
  hefe,
  pkgs,
  system,
  lib,
  ...
}@args:

let
  sources = hefe.third_party;
  home-manager = sources."home-manager";
  nixpkgs-lib = (import sources."nixos-unstable" { }).lib;

  evalConfig = import (sources."nix-darwin" + "/eval-config.nix");

  darwinSystem = args@{ modules, ... }:
    let
      argsToPass = builtins.removeAttrs args [ "system" "pkgs" "inputs" "modules" ];
      finalArgs = { lib = nixpkgs-lib; }
        // nixpkgs-lib.optionalAttrs (args ? pkgs) { inherit (args.pkgs) lib; }
        // argsToPass
        // {
          modules = modules
            ++ nixpkgs-lib.optional (args ? pkgs) ({ lib, ... }: {
              _module.args.pkgs = lib.mkForce args.pkgs;
            })
            ++ nixpkgs-lib.optional (args ? system) ({ lib, ... }: {
              nixpkgs.hostPlatform = lib.mkDefault args.system;
            });
        };
    in
      evalConfig finalArgs;

  darwinFor =
    machineName:
    let
      pkgsUnstable = import sources."nixos-unstable" {
        hostPlatform = system;
        config.allowUnfree = true;
      };
      configPath = "${toString ./.}/machines/aarch64/${machineName}/default.nix";
    in
    darwinSystem {
      inherit system;
      pkgs = pkgsUnstable;
      specialArgs = { inherit args sources; };
      modules = [
        (builtins.removeAttrs hefe.ops.machines.aarch64."${machineName}" [ "__readTree" "__readTreeChildren" ])
        "${home-manager}/nix-darwin"
        {
          home-manager.useGlobalPkgs = true;
          home-manager.useUserPackages = true;

          environment.darwinConfig = configPath;

          nix.nixPath = lib.mkForce [
            "darwin=${sources."nix-darwin"}"
            "nixpkgs=${sources."nixos-unstable"}"
            "darwin-config=${configPath}"
          ];
        }
      ];
    };

  switchScriptFor =
    let
      darwinPath = sources."nix-darwin".outPath;
      nixpkgsPath = sources."nixos-unstable".outPath;
      repoPath = toString ../.;
    in
    hostname:
    let
      configPath = "${repoPath}/ops/machines/aarch64/${hostname}/default.nix";
    in
    pkgs.writeShellScriptBin "switch" ''
        set -euo pipefail

        echo "Building darwin configuration for ${hostname}..."
        SYSTEM_PATH=$(nix-build -A ops.darwin.${hostname}.system --no-out-link)

        echo "Activating configuration..."
        sudo env NIX_PATH="darwin=${darwinPath}:nixpkgs=${nixpkgsPath}:darwin-config=${configPath}" $SYSTEM_PATH/activate
    '';

  conf = name: {
    "${name}" = {
      switch = switchScriptFor name;
      system = (darwinFor name).system;
    };
  };

  machines = lib.attrsets.mergeAttrsList (
    lib.attrsets.attrValues (
      lib.attrsets.mergeAttrsList (
        builtins.map (x: { "${x}" = (conf (lib.removeSuffix ".nix" x)); }) (
          builtins.attrNames (
            lib.attrsets.filterAttrs (k: v: v == "directory") (builtins.readDir ./machines/aarch64)
          )
        )
      )
    )
  );

in
machines

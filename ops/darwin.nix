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

  nixos = sources."nixos-25.11";
  nixos-unstable = sources."nixos-unstable";
  nix-darwin = sources."nix-darwin";
  home-manager = sources."home-manager";

  nixpkgs-lib = (import nixos { }).lib;

  evalConfig = import (nix-darwin + "/eval-config.nix");

  darwinSystem =
    args@{ modules, ... }:
    let
      argsToPass = builtins.removeAttrs args [
        "system"
        "pkgs"
        "inputs"
        "modules"
      ];
      finalArgs = argsToPass // {
        lib = nixpkgs-lib;
        modules =
          modules
          ++ nixpkgs-lib.optional (args ? pkgs) (
            { lib, ... }:
            {
              _module.args = {
                pkgs = lib.mkForce args.pkgs;
                unstable = (import nixos-unstable {
                  inherit (pkgs) system;
                  config.allowUnfree = true;
                });
              };
            }
          )
          ++ nixpkgs-lib.optional (args ? system) (
            { lib, ... }:
            {
              nixpkgs.hostPlatform = lib.mkDefault args.system;
            }
          );
      };
    in
    evalConfig finalArgs;

  darwinFor =
    machineName:
    let
      over = pkgs: {
        direnv = pkgs.direnv.overrideAttrs (oldAttrs: {
          doCheck = false;
          doInstallCheck = false;
          # Some older nixpkgs versions run checkPhase via a checkTarget hook in the Makefile, 
          # so we scrub the check phases entirely to be absolutely certain it doesn't try.
          checkPhase = "";
          installCheckPhase = "";
        });
      };
      
      pkgsStable = import nixos {
        localSystem = { inherit system; };
        config.allowUnfree = true;
        config.packageOverrides = over;
      };

      pkgsUnstable = import nixos-unstable {
        localSystem = { inherit system; };
        config.allowUnfree = true;
        config.packageOverrides = over;
      };

      machineDir = "${toString ./.}/machines/aarch64/${machineName}";
      configPath = "${toString ./.}/machines/aarch64/${machineName}/default.nix";
    in
    darwinSystem {
      inherit system;
      pkgs = pkgsStable;

      # 🚀 Bypass the strict channel version mismatch assertion
      enableNixpkgsReleaseCheck = false;

      specialArgs = {
        inherit args sources;
        unstable = pkgsUnstable;
      };
      modules = [
        (builtins.removeAttrs hefe.ops.machines.aarch64."${machineName}" [
          "__readTree"
          "__readTreeChildren"
        ])
        "${home-manager}/nix-darwin"

        # Dynamically inject your home_emile.nix file if it exists for this host
        (
          { unstable, ... }:
          {
            home-manager.users.emile = import "${machineDir}/home_emile.nix";

            # 🌟 Inject variables directly into the home_emile.nix module arguments
            home-manager.extraSpecialArgs = {
              inherit unstable;
            };

            # Set the state version globally for safety
            home-manager.sharedModules = [ { home.stateVersion = "22.11"; } ];
          }
        )

        {
          home-manager.useGlobalPkgs = true;
          home-manager.useUserPackages = true;

          environment.darwinConfig = configPath;

          nix.nixPath = lib.mkForce [
            "darwin=${nix-darwin}"
            "nixpkgs=${nixos}"
            "darwin-config=${configPath}"
          ];
        }
      ];
    };

  switchScriptFor =
    let
      darwinPath = nix-darwin.outPath;
      nixpkgsPath = nixos.outPath;
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

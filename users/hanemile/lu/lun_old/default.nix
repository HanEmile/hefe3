{ hefe
, system ? "aarch64-darwin"
, ...
}:

let
  overlays = [
    (import hefe.third_party.sources.rust-overlay)

    (final: prev: {
      rust-bin-wasm = prev.rust-bin.stable.latest.default.override {
        targets = [ "wasm32-unknown-unknown" ];
      };

      rustPlatformWithWasm = prev.callPackage (
        { makeRustPlatform, rust-bin-wasm }:
        makeRustPlatform {
          rustc = rust-bin-wasm;
          cargo = rust-bin-wasm;
        }
      ) {};
    })
  ];

  pkgs = import hefe.third_party.nixpkgs.path { inherit system overlays; };

  naersk' = pkgs.callPackage hefe.third_party.sources.naersk {
    cargo = pkgs.rust-bin-wasm;
    rustc = pkgs.rust-bin-wasm;
  };

  naerskPkg =
    name:
    naersk'.buildPackage {
      src = ./.;
      pname = "${name}";
      overrideMain = old: {
        preConfigure = ''
          cargo_build_options="$cargo_build_options -p ${name}"
        '';
      };
    };
in
  naerskPkg "lun"

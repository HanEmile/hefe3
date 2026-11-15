{ hefe
, system ? "aarch64-darwin"
, rust-overlay ? hefe.third_party."rust-overlay"
, nixos ? hefe.third_party."nixos-25.11"
, naersk ? hefe.third_party."naersk"
, ...
}:

let
  overlays = [
    (import rust-overlay)

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

  pkgs = import nixos { inherit system overlays; };

  naersk' = pkgs.callPackage naersk {
    cargo = pkgs.rust-bin-wasm;
    rustc = pkgs.rust-bin-wasm;
  };

  naerskPkg =
    name:
    naersk'.buildPackage {
      src = ./crates + "/${name}";
      pname = "${name}";
      overrideMain = old: {
        preConfigure = ''
          cargo_build_options="$cargo_build_options -p ${name}"
        '';
      };
    };
in
{
  native = naerskPkg "malwok_app";
  web = pkgs.stdenv.mkDerivation {
    name = "malwok-web";
    version = "0.0.1";
    src = ./.;

    cargoDeps = pkgs.rustPlatformWithWasm.importCargoLock {
      lockFile = ./Cargo.lock;
    };

    nativeBuildInputs = with pkgs; [
      rustPlatformWithWasm.cargoSetupHook
      rustPlatformWithWasm.cargoBuildHook

      trunk
     (wasm-bingen-cli.override {
        version = "0.2.100";
        hash = "sha256-3RJzK7mkYFrs7C/WkhW9Rr4LdP5ofb2FdYGz1P7Uxog=";
        cargoHash = "sha256-tD0OY2PounRqsRiFh8Js5nyknQ809ZcHMvCOLrvYHRE=";
     })

     binaryen # for wasm-opt
    ];

    buildPhase = ''
      runHook preBuild
      export RUSTFLAGS='--cfg=web_sys_unstable_apis --cfg getrandom_backend="wasm_js"'

      cd crates/malwok_app
      cp ../../Cargo.lock .

      trunk build -v --offline --frozen --release

      runHook postBuild
    '';

    installPhase = ''
      runHook preInstall
      cp -R dist $out
      runHook postInstall
    '';
  };
}

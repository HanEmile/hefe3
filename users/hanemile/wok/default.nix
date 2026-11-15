{
  hefe,

  # imports
  rust-overlay ? hefe.third_party."rust-overlay",
  nixos ? hefe.third_party."nixos-25.05",
  craneSrc ? hefe.third_party."crane",
  advisory-db ? hefe.third_party."advisory-db",

  # vars
  system ? "aarch64-darwin",
  rustChannel ? "stable",
  ...
}:

let
  # nixos foo
  pkgs = import nixos {
    inherit system;
    overlays = [ (import rust-overlay) ];
  };
  lib = pkgs.lib;

  # crane foo
  crane = (import (craneSrc.outPath + "/flake.nix")).outputs { };

  # rust foo
  rustToolchainFor =
    x:
    x.rust-bin."${rustChannel}".latest.default.override {
      extensions = [ "rust-src" ];
      targets = [ "wasm32-unknown-unknown" ];
    };
  craneLib = (crane.mkLib pkgs).overrideToolchain rustToolchainFor;

  # src
  unfilteredRoot = ./.; # The original, unfiltered source
  src = lib.fileset.toSource {
    root = unfilteredRoot;
    fileset = lib.fileset.unions [
      # Default files from crane (Rust and cargo files)
      (craneLib.fileset.commonCargoSources unfilteredRoot)
      (lib.fileset.fileFilter (
        file:
        lib.any file.hasExt [
          "html"
          "scss"
          "js"
          "png"
          "json"
        ]
      ) unfilteredRoot)
      # Example of a folder for images, icons, etc
      (lib.fileset.maybeMissing ./assets)
    ];
  };

  commonArgs = {
    inherit src;
    strictDeps = true;

    buildInputs = [
      pkgs.libvirt
    ] ++ lib.optionals pkgs.stdenv.isDarwin [
      pkgs.libiconv
    ];
    nativeBuildInputs = [
      pkgs.libvirt
      pkgs.pkg-config
    ];

    env.RUSTFLAGS = "-L native=${pkgs.libvirt}/lib";
  };

  # Build *just* the cargo dependencies, so we can reuse
  # all of that work (e.g. via cachix) when running in CI
  cargoArtifacts = craneLib.buildDepsOnly (
    commonArgs
    // {
      # You cannot run cargo test on a wasm build
      doCheck = false;
    }
  );

  # The "web" target
  # To be hosted using `python3 -m http.server -d result -b ::1 8081`
  web = craneLib.buildTrunkPackage (
    commonArgs
    // {

      inherit cargoArtifacts;
      # We must force the target, otherwise cargo will attempt to use your native target
      CARGO_BUILD_TARGET = "wasm32-unknown-unknown";

      # The version of wasm-bindgen-cli here must match the one from Cargo.lock.
      # When updating to a new version replace the hash values with lib.fakeHash,
      # then try to do a build, which will fail but will print out the correct value
      # for `hash`. Replace the value and then repeat the process but this time the
      # printed value will be for the second `hash` below
      wasm-bindgen-cli = pkgs.buildWasmBindgenCli rec {
        src = pkgs.fetchCrate {
          pname = "wasm-bindgen-cli";
          version = "0.2.104";
          hash = "sha256-9kW+a7IreBcZ3dlUdsXjTKnclVW1C1TocYfY8gUgewE=";
          # hash = lib.fakeHash;
        };

        cargoDeps = pkgs.rustPlatform.fetchCargoVendor {
          inherit src;
          inherit (src) pname version;
          hash = "sha256-V0AV5jkve37a5B/UvJ9B3kwOW72vWblST8Zxs8oDctE=";
          # hash = lib.fakeHash;
        };
      };
    }
  );

  # the "native" target
  native = craneLib.buildPackage (
    commonArgs
    // {
      inherit cargoArtifacts;
    }
  );

  # again, reusing the dependency artifacts from above.
  #
  # Note that this is done as a separate derivation so that
  # we can block the CI if there are issues here, but not
  # prevent downstream consumers from building our crate by itself.
  clippy = craneLib.cargoClippy (
    commonArgs
    // {
      inherit cargoArtifacts;
      cargoClippyExtraArgs = "--all-targets -- --deny warnings";
    }
  );

  documentation = craneLib.cargoDoc (
    commonArgs
    // {
      inherit cargoArtifacts;
      # This can be commented out or tweaked as necessary, e.g. set to
      # `--deny rustdoc::broken-intra-doc-links` to only enforce that lint
      env.RUSTDOCFLAGS = "--deny warnings";
    }
  );

  # Check formatting
  fmt = craneLib.cargoFmt {
    inherit src;
  };

  toml-fmt = craneLib.taploFmt {
    src = pkgs.lib.sources.sourceFilesBySuffices src [ ".toml" ];
    # taplo arguments can be further customized below as needed
    # taploExtraArgs = "--config ./taplo.toml";
  };

  # Audit dependencies
  audit = craneLib.cargoAudit {
    inherit src advisory-db;
  };

  # Audit licenses
  deny = craneLib.cargoDeny {
    inherit src;
  };

  packages = [
    web
    native
  ];

  doc = [
    documentation
  ];

  formatters = [
    fmt
    toml-fmt
  ];
  linters = [
    clippy
  ];
  auditors = [
    audit # uses advisory-db
    # deny # licenses
  ];

  checks = formatters ++ linters ++ auditors;

  all = packages ++ doc ++ checks;
in
{
  inherit
    web # the web-build
    native # native application
    checks # format, lint, audit
    doc # cargo doc
    all # all of the above
    ;
}

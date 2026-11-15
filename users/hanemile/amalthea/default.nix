# Amalthea — astrophotography ecosystem application.
#
# Build targets:
#   nix-build -A users.hanemile.projects.go.amalthea.native
#   nix-build -A users.hanemile.projects.go.amalthea.backend
#   nix-build -A users.hanemile.projects.go.amalthea.web
{
  hefe,
  pkgs,
  lib,
  ...
}:

let
  # Build a Rust toolchain that includes the wasm32 target. We layer
  # rust-overlay onto a fresh `pkgs` import and then ask crane to use
  # the resulting toolchain. The native/backend builds keep using the
  # host toolchain, so this overlay only matters for the WASM pipeline.
  rustOverlay = import hefe.third_party.rust-overlay;
  pkgsRust = import pkgs.path {
    inherit (pkgs) system;
    overlays = [ rustOverlay ];
  };
  rustWasmToolchain = pkgsRust.rust-bin.stable.latest.default.override {
    targets = [ "wasm32-unknown-unknown" ];
  };

  craneCore = import hefe.third_party.crane { inherit pkgs; };
  craneLib = craneCore;
  craneLibWasm = craneCore.overrideToolchain rustWasmToolchain;

  # Source filtering: include only Rust-relevant files.
  src = lib.cleanSourceWith {
    src = ./.;
    filter = path: type:
      (craneLib.filterCargoSources path type)
      || (lib.hasSuffix ".html" path)
      || (lib.hasSuffix ".json" path);
  };

  # Common arguments for all builds.
  commonArgs = {
    inherit src;
    strictDeps = true;

    buildInputs = with pkgs; [
      openssl
    ] ++ lib.optionals pkgs.stdenv.hostPlatform.isDarwin [
      apple-sdk
      darwin.libiconv
    ];

    nativeBuildInputs = with pkgs; [
      pkg-config
    ];
  };

  # WASM-specific build args. We skip the package's `cargo test` because
  # nothing on wasm32 can run unit tests in nix's sandbox.
  wasmArgs = commonArgs // {
    CARGO_BUILD_TARGET = "wasm32-unknown-unknown";
    doCheck = false;
  };

  # Host-target dependency cache.
  #
  # IMPORTANT for build performance: `cargoExtraArgs` here MUST match
  # the args used by every consumer (`native`, `backend`,
  # `workspace-test`) verbatim. If they diverge, cargo resolves a
  # different feature set per-crate and rebuilds every dep — the cache
  # gets decompressed and then thrown away. We saw `eframe`, `wgpu`,
  # `hyper`, ~50 other crates re-compiling on every native build because
  # cargoArtifacts ran `--workspace …` while the consumer ran `-p
  # amalthea-native`, producing different feature unifications.
  #
  # Solution: cargoArtifacts builds the FULL workspace minus
  # amalthea-web. All consumers compile the full workspace minus
  # amalthea-web. They install only their own binary via `cargoBuildCommand`
  # / install hook tweaks, but cargo's feature resolution stays
  # identical → all dep `.rmeta`/`.rlib` files are cache hits.
  #
  # We exclude amalthea-web because its wasm-only feature flags (uuid/js,
  # getrandom/wasm_js) would otherwise bleed into host feature
  # resolution and break Cargo.lock.
  workspaceArgs = "--workspace --exclude amalthea-web";

  cargoArtifacts = craneLib.buildDepsOnly (commonArgs // {
    pname = "amalthea-deps";
    cargoExtraArgs = workspaceArgs;
  });

  cargoArtifactsWasm = craneLibWasm.buildDepsOnly (wasmArgs // {
    pname = "amalthea-deps-wasm";
    cargoExtraArgs = "-p amalthea-web";
  });

  webRaw = craneLibWasm.buildPackage (wasmArgs // {
    cargoArtifacts = cargoArtifactsWasm;
    pname = "amalthea-web-raw";
    cargoExtraArgs = "-p amalthea-web";
  });

  # Build a wasm-bindgen-cli pinned to the same version as the
  # `wasm-bindgen` crate locked in this workspace's Cargo.lock. Schema
  # versions must match exactly, so we don't fall back to nixpkgs'.
  wasmBindgenVersion = "0.2.120";
  wasmBindgenCli = pkgs.rustPlatform.buildRustPackage rec {
    pname = "wasm-bindgen-cli";
    version = wasmBindgenVersion;
    src = pkgs.fetchCrate {
      inherit pname version;
      hash = "sha256-Dkkx8Bhfk+y/jEz9Fzwytmv2N3Gj/7ST+5MlPRzzetU=";
    };
    cargoHash = "sha256-5Zu/Sh9aBMxB+KGC1MHWJAQ8PuE40M6lsenkpFEwJ6A=";
    nativeBuildInputs = [ pkgs.pkg-config ];
    buildInputs = [ pkgs.openssl ]
      ++ lib.optionals pkgs.stdenv.hostPlatform.isDarwin [ pkgs.apple-sdk pkgs.darwin.libiconv ];
    doCheck = false;
  };

  # Native desktop binary.
  #
  # `cargoExtraArgs` matches the cargoArtifacts args exactly so the dep
  # cache is fully reused. We then narrow the *install* step to only
  # pick up the amalthea-native binary via `cargoBuildCommand`'s implicit
  # workspace build + crane's binary-detection pass.
  native = craneLib.buildPackage (commonArgs // {
    inherit cargoArtifacts;
    pname = "amalthea-native";
    cargoExtraArgs = "${workspaceArgs} --bin amalthea-native";
    doCheck = false;

    buildInputs = commonArgs.buildInputs ++ lib.optionals pkgs.stdenv.hostPlatform.isLinux (with pkgs; [
      wayland
      libxkbcommon
      xorg.libX11
      xorg.libXcursor
      xorg.libXrandr
      xorg.libXi
      vulkan-loader
    ]);
  });

  # Backend API server.
  backend = craneLib.buildPackage (commonArgs // {
    inherit cargoArtifacts;
    pname = "amalthea-backend";
    cargoExtraArgs = "${workspaceArgs} --bin amalthea-backend";
    doCheck = false;
  });

  # Run `cargo test` across the whole workspace (excluding amalthea-web,
  # which can't run in the sandbox). Use this to exercise unit tests
  # that live inside `amalthea-app` — the `native`/`backend` targets
  # only run tests scoped to their own package.
  workspace-test = craneLib.cargoTest (commonArgs // {
    inherit cargoArtifacts;
    pname = "amalthea-workspace-test";
    cargoExtraArgs = "--workspace --exclude amalthea-web";

    buildInputs = commonArgs.buildInputs ++ lib.optionals pkgs.stdenv.hostPlatform.isLinux (with pkgs; [
      wayland
      libxkbcommon
      xorg.libX11
      xorg.libXcursor
      xorg.libXrandr
      xorg.libXi
      vulkan-loader
    ]);
  });

  # WASM web build — produces a $out/ ready to be served as static
  # files (index.html, *.js shim, *.wasm). Pipeline:
  #   1. crane builds amalthea-web targeting wasm32-unknown-unknown
  #   2. wasm-bindgen post-processes the .wasm into a JS-loadable module
  #   3. wasm-opt shrinks the binary (release builds only)
  web = pkgs.runCommand "amalthea-web" {
    nativeBuildInputs = [ wasmBindgenCli pkgs.binaryen ];
  } ''
    mkdir -p $out
    cp ${./crates/amalthea-web/index.html} $out/index.html

    # Locate the .wasm produced by crane. cdylibs land in lib/.
    wasm_in=$(find ${webRaw} -name 'amalthea_web*.wasm' | head -n1)
    if [ -z "$wasm_in" ]; then
      echo "no wasm artifact found in ${webRaw}" >&2
      exit 1
    fi

    # wasm-bindgen emits amalthea_web.js + amalthea_web_bg.wasm.
    wasm-bindgen \
      --target web \
      --no-typescript \
      --out-dir $out \
      --out-name amalthea_web \
      "$wasm_in"

    # Optional shrink pass.
    if command -v wasm-opt >/dev/null; then
      mv $out/amalthea_web_bg.wasm $out/amalthea_web_bg.unopt.wasm
      wasm-opt -O2 \
        $out/amalthea_web_bg.unopt.wasm \
        -o $out/amalthea_web_bg.wasm
      rm $out/amalthea_web_bg.unopt.wasm
    fi
  '';

in let
  # Convenience: a single derivation containing both the backend binary
  # and a wrapper script that launches it with STATIC_DIR pointing at the
  # bundled WASM dist. Run with:
  #
  #   nix-build -A users.hanemile.projects.go.amalthea.serve && ./result/bin/amalthea-serve
  #
  # The wrapper takes the same env vars as the raw backend (LISTEN_ADDR,
  # DB_PATH, IMMICH_URL, ...) and forwards extra args to it.
  serve = pkgs.writeShellApplication {
    name = "amalthea-serve";
    runtimeInputs = [ outputs.backend ];
    text = ''
      : "''${LISTEN_ADDR:=127.0.0.1:8080}"
      : "''${DB_PATH:=$HOME/.local/share/amalthea/amalthea.db}"
      mkdir -p "$(dirname "$DB_PATH")"
      export LISTEN_ADDR DB_PATH
      export STATIC_DIR="${outputs.web}"
      echo "amalthea: serving WASM from $STATIC_DIR"
      echo "amalthea: binding $LISTEN_ADDR"
      echo "amalthea: db at $DB_PATH"
      exec amalthea-backend "$@"
    '';
  };

  outputs = {
    inherit native backend web serve workspace-test;
  };
in
  outputs

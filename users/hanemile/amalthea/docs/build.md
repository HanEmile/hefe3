# Build

Last reviewed: 2026-05-01 | Review frequency: monthly

Source: `users/hanemile/projects/go/amalthea/default.nix`.

## Entry points

All build commands run from the **repo root** (`hefe3/`), not the
amalthea subdirectory.

```sh
# Native desktop binary (amalthea-native). Also runs cargo test on
# the host-target workspace as part of buildPackage.
nix-build -A users.hanemile.projects.go.amalthea.native

# Backend HTTP server (amalthea-backend). Also runs the workspace tests.
nix-build -A users.hanemile.projects.go.amalthea.backend

# WASM web build — index.html + amalthea_web.js + amalthea_web_bg.wasm.
# Skips cargo test (wasm32 can't run them in the Nix sandbox).
nix-build -A users.hanemile.projects.go.amalthea.web

# One-command server: backend + bundled WASM dist, ready to run.
nix-build -A users.hanemile.projects.go.amalthea.serve
./result/bin/amalthea-serve            # listens on 127.0.0.1:8080 by default
```

There is no global `cargo` on PATH for this project. All builds /
tests / type checks go through Nix.

## Why cargo-via-Nix

- Crane drives `cargo check --release --locked --all-targets` inside
  a deps-only step, then the build step on top — the same way native
  test suites get run as part of `buildPackage`.
- Locking `Cargo.lock` is enforced (`--locked`), so any feature flag
  change that would alter the resolution surfaces immediately.

## Host vs WASM dependency separation

The host targets (native, backend) and the wasm target (web) **share
the same workspace and Cargo.lock**, but resolve different feature
sets — uuid wants `js` only on wasm32, getrandom wants `wasm_js`,
etc.

Crane's deps-only step caches dependencies. If the host cache step
sees the wasm-only feature flags, it forces `Cargo.lock` regen and
host builds break. To avoid this:

- WASM-only deps + features live **only in the `amalthea-web`
  Cargo.toml**, not in the workspace-wide `amalthea-app`.
- The host-target deps cache uses
  `cargoExtraArgs = "--workspace --exclude amalthea-web"`.

## wasm-bindgen pinning

`wasm-bindgen-cli` schema must match the `wasm-bindgen` crate
version exactly. Nixpkgs ships an older version than what the
crate's locked at, so `default.nix` builds `wasm-bindgen-cli`
0.2.120 from crates.io with explicit `hash` + `cargoHash`. Bump in
sync with the crate.

## WASM pipeline

`web` derivation:

1. `craneLibWasm.buildPackage` — crane with the rust-overlay
   wasm32-unknown-unknown toolchain compiles `amalthea-web` to a
   `.wasm` cdylib.
2. `wasm-bindgen --target web` post-processes into
   `amalthea_web.js` + `amalthea_web_bg.wasm`.
3. `wasm-opt -O2` shrinks the binary.
4. `cp index.html` from the crate.

Output is `$out/{index.html, amalthea_web.js, amalthea_web_bg.wasm}`.

## Serve wrapper

`serve` is a `pkgs.writeShellApplication` that:

1. Sets `STATIC_DIR` to the bundled `web` derivation's path.
2. Sets `LISTEN_ADDR` (default `127.0.0.1:8080`) and
   `DB_PATH` (default `$HOME/.local/share/amalthea/amalthea.db`).
3. Execs `amalthea-backend`.

All env vars are overridable (`LISTEN_ADDR=… DB_PATH=… ./result/bin/amalthea-serve`).

## Deploy

```sh
nix-build -A ops.nixos.amalthea.deploy && ./result/bin/deploy
```

Targets the VM at 192.168.75.13 on medano. The web entry point sits
behind nginx at amalthea.medano.emile.space.

## Cross-references

- `docs/architecture.md` — crate layout
- `docs/verifier.md` — Go + Playwright verifier (built outside Nix)
- `default.nix` — the actual derivations

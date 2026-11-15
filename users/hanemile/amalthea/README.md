# Amalthea

Astrophotography ecosystem application with a Blender-like BSP interface built on egui.

Last reviewed: 2026-04-30 | Review frequency: monthly

## What it does

- Browse and manage astrophotography images from an Immich instance
- Stack and align sub-frames with a guided stacking editor
- Inspect EXIF data, view histograms, browse timelines
- Plan observations with an interactive 3D sky viewer (Earth, stars, telescope framing)
- Authenticate via SSO (Authelia OIDC)

## Build

Requires Nix. All builds happen from the hefe3 repo root.

```sh
# Native desktop binary
nix-build -A users.hanemile.projects.go.amalthea.native
./result/bin/amalthea-native

# Backend server (serves WASM + proxies Immich API)
nix-build -A users.hanemile.projects.go.amalthea.backend
./result/bin/amalthea-backend

# WASM web build (static files, served by backend)
nix-build -A users.hanemile.projects.go.amalthea.web
```

## Development

For local development without Nix:

```sh
cd users/hanemile/projects/go/amalthea

# Install Rust with wasm target
rustup target add wasm32-unknown-unknown

# Run native app
cargo run -p amalthea-native

# Run backend
IMMICH_URL=http://photo.medano.emile.space:9091 \
LISTEN_ADDR=127.0.0.1:8080 \
cargo run -p amalthea-backend

# Build WASM (requires wasm-bindgen-cli and wasm-opt)
cargo build -p amalthea-web --target wasm32-unknown-unknown --release
wasm-bindgen --out-dir dist --target web target/wasm32-unknown-unknown/release/amalthea_web.wasm
```

## Deploy

Deploy the amalthea VM on medano:

```sh
nix-build -A ops.nixos.amalthea.deploy && timeout 600 time ./result/bin/deploy
```

## Architecture

```
crates/
  amalthea-app/       Shared egui application (BSP layout, panels, immich client)
  amalthea-native/    Desktop entry point (eframe)
  amalthea-web/       WASM entry point (eframe + wasm-bindgen)
  amalthea-backend/   Axum server (WASM host, Immich proxy, OIDC handler)
```

The primary target is the browser. The backend runs on the same hypervisor as Immich, so image data never leaves the local network — all transfers happen at bridge speed (~10Gbps). The browser loads the WASM app from the backend, which proxies all Immich API calls.

## Infrastructure

- **VM**: `amalthea` at 192.168.75.13 on medano (virbr0, 4GB RAM)
- **Web**: `amalthea.medano.emile.space` (TLS via ACME, nginx reverse proxy on medano)
- **Auth**: Authelia OIDC client `amalthea` at auth.medano.emile.space
- **Storage**: Immich at photo VM (192.168.75.9:9091), accessed via API key

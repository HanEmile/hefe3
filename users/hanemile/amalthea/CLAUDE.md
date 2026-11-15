# Amalthea

Astrophotography ecosystem application. Blender-like BSP egui UI for
browsing, aligning, and stacking astro images, plus observation
planning.

Last reviewed: 2026-05-01 | Review frequency: monthly

## Documentation rules (read first, keep current)

**Before starting any task**: open the docs that cover the
subsystems the task touches. The index below is the single source of
truth — every other file is at most one click away. If you don't
know which doc applies, start with `docs/architecture.md`.

**While working**: keep the relevant docs in sync with the change.
A code change that contradicts a doc is a bug — fix the doc in the
same edit. The PLAN.md status table gets a new row when a feature
ships.

**Before finishing**: bump the `Last reviewed` line on any doc you
touched. Add cross-references when a new concept fits naturally
into another doc.

## Doc index

Everything below is a `docs/<topic>.md` file (or a top-level file
where it makes more sense to live there). One indirection from this
page to the detail.

- [`docs/architecture.md`](docs/architecture.md) — crate layout, single-state pattern, frame lifecycle.
- [`docs/state.md`](docs/state.md) — every `AppState` field and why it's there.
- [`docs/stacking.md`](docs/stacking.md) — node graph, two-tier evaluator, dark-frame logic, caching.
- [`docs/alignment.md`](docs/alignment.md) — `Alignment` model, viewer, auto-align job.
- [`docs/panels.md`](docs/panels.md) — panel inventory + how to add one.
- [`docs/theme.md`](docs/theme.md) — colour tokens + live-tweakable persistence.
- [`docs/ui-conventions.md`](docs/ui-conventions.md) — gestures, density, selection rules.
- [`docs/build.md`](docs/build.md) — Nix targets + WASM pipeline + serve wrapper.
- [`test/verify/README.md`](test/verify/README.md) — Go + Playwright verifier.
- [`PLAN.md`](PLAN.md) — feature status table, design notes, ideas log, open questions.
- [`README.md`](README.md) — user-facing summary + non-Nix dev workflow.

## Build (essentials)

All builds run from the **repo root** (`hefe3/`). No global `cargo`
on PATH — go through Nix.

```sh
# Native desktop binary (also runs cargo test on the workspace)
nix-build -A users.hanemile.projects.go.amalthea.native

# Backend HTTP server
nix-build -A users.hanemile.projects.go.amalthea.backend

# WASM web build (skips tests; wasm32 can't run them)
nix-build -A users.hanemile.projects.go.amalthea.web

# Server bundle: backend + WASM dist, single command
nix-build -A users.hanemile.projects.go.amalthea.serve
./result/bin/amalthea-serve
```

For agents: prefer `nix-build` over invoking `cargo` directly. To
type-check before a full build, build the smallest target that
exercises the changed crate (`backend` for app/backend, `native`
for app/native).

Full details: [`docs/build.md`](docs/build.md).

## Coding rules

- Work in structured, planned steps. No dirty hacks.
- Clean, extensible, maintainable code. Prefer clarity over cleverness.
- Performance is paramount: cache aggressively, prefetch, saturate
  the local network.
- All public types and functions have doc comments.
- Documentation includes last-reviewed timestamps + review frequency.
- Never hardcode `Color32::from_*` for chrome / text / borders —
  read `state.theme.*`. See `docs/theme.md`.
- Panels never own state. Mutate `AppState` directly. See
  `docs/architecture.md`.
- Background workers communicate via mpsc channels held in
  `AppState` and a `job_version` counter so stale results self-discard.
  See `docs/stacking.md` for the canonical pattern.

## Working with the user

- Run `say "<message>"` (macOS speech) to announce relevant milestones —
  task done, build green, test failure, blocker. Default closing call:
  `say "I'm done"`. Keep messages short (≤ 5 words) and only announce
  when something the user might want to check has actually happened
  (e.g. don't `say` after every tool call).

## Infrastructure

- VM: 192.168.75.13 on medano (virbr0)
- Web: amalthea.medano.emile.space (nginx → backend:8080)
- Auth: Authelia OIDC client "amalthea"
- Immich: API key for server-to-server access (photo VM at 192.168.75.9:9091)

## Pinned dependencies

| Dependency | Purpose | Pin |
|---|---|---|
| crane | Nix Rust build | `third_party/npins` |
| rust-overlay | Rust toolchain + wasm target | `third_party/npins` |
| egui / eframe | UI framework | Cargo.lock |
| wgpu | 3D rendering (sky viewer) | Cargo.lock |
| axum | Backend HTTP server | Cargo.lock |
| reqwest | HTTP client (Immich API) | Cargo.lock |
| wasm-bindgen-cli | Built from crates.io to match the `wasm-bindgen` crate version exactly | `default.nix` |

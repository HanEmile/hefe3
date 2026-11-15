# Architecture

Last reviewed: 2026-05-01 | Review frequency: monthly

## Crates

```
amalthea-app/      shared egui application logic
  ├─ bsp/          Blender-style panel layout engine
  ├─ panels/       per-panel render functions (1 file per panel)
  ├─ immich/       hand-written async Immich API client
  ├─ sky/          3D sky / horizon / framing geometry (wgpu-backed)
  ├─ auth/         OIDC state machine (Authelia)
  ├─ state.rs      AppState — single mutable struct passed to every panel
  ├─ theme.rs      live-tweakable colour tokens + egui Visuals plumbing
  ├─ stacking.rs   two-tier graph evaluator (sync scrub + async full)
  ├─ auto_align.rs background star-anchor alignment job
  ├─ persist.rs    AppState ⇄ backend SQLite (theme.json today)
  ├─ workspace.rs  Workspace = name + root BSP node
  ├─ config.rs     AppConfig (URLs, paths) — env-loaded, settings-editable
  ├─ cache.rs      LruCache<K,V> + ThumbnailCache / ImageCache wrappers
  ├─ import.rs     Immich shared-link → frame-pixels message pipeline
  └─ api.rs        thin wrapper around backend's /db/* HTTP endpoints

amalthea-native/   eframe desktop entry (re-exports AmaltheaApp)
amalthea-web/      WASM entry — wasm-bindgen `start` + canvas attach
amalthea-backend/  Axum server: serves WASM, proxies Immich, OIDC, SQLite
```

`amalthea-web` and `amalthea-native` are tiny — the only platform-specific
file each owns is its entry point. Everything else lives in
`amalthea-app` and is shared.

## Single-state pattern

`AppState` is one big mutable struct. Every panel takes
`(ui, …, state: &mut AppState)`. This is deliberate:

- Panels don't own their state; the BSP layout can move them around
  freely without losing data.
- One source of truth means features like the alignment job can
  trivially reach into per-panel state, and panels can trivially read
  whatever they need (selections, themes, caches, jobs).
- Background workers communicate with the main thread by sending
  result messages over `mpsc::channel`s held inside `AppState` (see
  `stacking_jobs`, `align_jobs`, `import_rx`).

Trade-off: the struct is large. Mitigated by grouping related fields
together (collections / stacking / alignment / sky / trip planner /
… each form a contiguous block with a section comment).

See `docs/state.md` for a field-by-field map.

## Frame lifecycle (`AmaltheaApp::update`)

Each frame, in order:

1. `persist::load_initial(state)` — once-per-run theme load.
2. `persist::flush(state)` — persist theme if `theme_dirty`.
3. `theme::apply(ctx, &state.theme)` — push current theme into
   `egui::Visuals` and `egui::Style`.
4. Top header bar (workspace tabs, import URL, auth status).
5. Footer status bar (status message, asset info, cache stats).
6. `process_import_messages` — drain `state.import_rx`, decode JPEGs
   into the GPU texture cache + raw `frame_pixels` for stacking.
7. `stacking::seed_default_graph_if_needed(state)` — first time we
   see a non-empty collection, lay down Source→Stack→Adjust→Output.
8. `stacking::evaluate_if_dirty(state, ctx)` — pump completed full-res
   jobs, then if dirty: synchronous scrub-pass + spawn full-res job.
9. `auto_align::pump_results(state)` — drain finished alignment jobs.
10. `request_full_image_if_needed(ctx, state)` — load preview-size
    JPEG for the selected asset if we don't have it.
11. Central panel — BSP `show()` walks the tree, dispatches each
    leaf to the matching `panels::*::show`.

The `dirty` flag on `StackingPreview` is the gate: every state change
that affects the preview flips it; the eval at step 8 picks it up.

## Cross-references

- `docs/state.md` — `AppState` field map
- `docs/stacking.md` — node graph, evaluator, caching
- `docs/panels.md` — panel conventions + per-panel summary
- `docs/theme.md` — colour tokens + persistence
- `docs/alignment.md` — Alignment data + auto-align job
- `docs/build.md` — Nix targets + WASM pipeline
- `docs/ui-conventions.md` — gestures, density, multi-select

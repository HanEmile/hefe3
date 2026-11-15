# Amalthea — Self-updating Plan

Last updated: 2026-05-01 (FFT phase-correlation alignment supersedes the triangle-matching attempt, items #41–#43)

This file tracks ongoing work, design decisions, and a verification matrix. Update it as features land or new ideas emerge.

## Status

| # | Item | Status | Notes |
|---|---|---|---|
| 1 | Sky planner: zoom-only desync | DONE | Default `sky_sync_zoom = false` (`state.rs`); toggle stays in panel. |
| 2 | Stacking: thumbnail preview grid | DONE | Header + thumb grid + node editor laid out in `stacking_editor.rs`. Reject overlay marks dropped frames. |
| 3 | Stacking: node editor | DONE | MVP: 6 node kinds (Source/Reject/Calibrate/Effect/Stack/Output), draggable nodes, click-to-wire ports, bezier edges, right-click menu to add/delete nodes. No execution yet. |
| 4 | Workspace tab: rename on double-click | DONE | Inline `TextEdit`; Enter/focus-loss commits. Right-click also has Rename/Delete. |
| 5 | UI theme: higher contrast, accent | DONE | New `theme.rs` module: navy chrome (#0a1218 → #1a2532), amber accent (#e8a040), proper text/dim/faint hierarchy. Applied via `egui::Visuals` plus per-panel overrides. |
| 6 | Multi-select + right-click "Stack" | DONE | Plain click replaces; Cmd/Ctrl-click toggles; Shift-click range. Context menu adds the selection to the stacking session. |
| 7 | amalthea-web nix build | DONE | crane + rust-overlay wasm32 toolchain, wasm-bindgen-cli pinned to 0.2.120 built from crates.io, optional wasm-opt pass. Excluded amalthea-web from host-target dep graph to avoid feature-resolution leak. |
| 8 | Verification framework (Go + Playwright) | DONE | `test/verify/cmd/verify/main.go`. 6 checks. Hard checks pass headless; pixel checks soft (GPU-dependent). |
| 9 | Self-updating plan file | DONE | This file. |
| 10 | One-command nix serve | DONE | `nix-build -A …amalthea.serve && ./result/bin/amalthea-serve`. Wrapper sets `STATIC_DIR` to the bundled WASM dist and execs the backend. |
| 11 | Theme as runtime state + SQLite persistence | DONE | `Theme` struct on `AppState`, applied via `theme::apply` each frame. `persist::load_initial`/`flush` round-trip through `/db/settings/theme.json`. |
| 12 | UI density: poweruser layout | DONE | Tighter `Style::spacing` (item 4×3, button pad 4×2), 0-radius corners by default, header bar 28px, log filter chips solid-coloured when active. |
| 13 | Live settings panel | DONE | Color pickers for every theme token, light/dark presets, corner-radius slider, editable text fields for backend/Immich/OIDC/db_path. Auto-save on change. |
| 14 | Stacking graph: typed nodes | DONE | `StackingNode` is now an enum with `Source / Stack / Adjust / Output` variants, each carrying its own params. Nodes render param widgets inline (radio buttons, sliders, text edit, save button). |
| 15 | Default Source→Stack→Adjust→Output graph | DONE | `stacking::seed_default_graph_if_needed` seeds the chain whenever `stacking_frames` is non-empty and the graph is empty. Re-seeded on Clear. |
| 16 | Stack execution pipeline | DONE | DAG topo-sort + per-pixel evaluator: mean/median/max stacking on the cached preview-size RGBA buffers (`record_frame_pixels` captures them on import). Output uploads to a dedicated texture in `image_cache`; image viewer falls back to it when no asset is selected. |
| 17 | Live re-stack on edits | DONE | Adjust slider drags / Stack method changes / Output filename edits all flip `stacking_preview.dirty`; the next frame re-evaluates. Adjust does hue (IQ rotate), saturation (around luma), exposure (linear multiply), gamma (pow) — fast enough to slide live on ~50 preview frames. |
| 18 | Output node: save PNG | DONE | Native: writes to `~/Pictures/<filename>` (or absolute path if given). WASM: data-URL `<a download>` click. Header bar also has a "Save PNG" button. |
| 19 | Panel header-extra strip | DONE | BSP `show()` now passes panels two rects: `header_extra` (a slot inside the BSP header bar between the type dropdown and the action buttons) + `content`. Image viewer uses it for an asset-picker dropdown. |
| 20 | Stacking nodes auto-grow | DONE | Each `StackingNodeSlot` carries a `measured_height: Option<f32>`. Bodies render in a foreground egui Area, the renderer reads `ui.min_rect().height()`, and clamps to `>= fallback_body_height(kind)` so we never collapse below content. Two-frame settle. |
| 21 | Output node Save click works | DONE | Side-effect of moving node bodies into a `Order::Middle` egui Area: clicks now reach inner widgets instead of being eaten by the canvas drag/click handler. The Save button inside Output also writes through `stacking::save_output`. |
| 22 | Image collections (groups) | DONE | New `Collection { id, name, kind, items }` model with kinds Lights/Darks/Bias/Stacked/Custom. Timeline right-click menu now offers "Add N image(s) to: <existing groups>" + "New <Kind> group". |
| 23 | Source nodes reference a collection | DONE | `SourceNode::collection_id`. The Source body has a ComboBox for picking which group to draw from. The default-graph seed binds to the first non-empty Lights group, and adds a second Source bound to a non-empty Darks group when one exists. |
| 24 | Stack node: dark-frame subtraction | DONE | `StackNode::subtract_darks`. Stack now exposes two input ports (Lights, Darks). Frames on the Darks port are averaged per-pixel and subtracted from each light before the chosen reduction. |
| 25 | Group preview popup | DONE | The editor's panel-wide preview strip is gone. Source nodes show the first 6 thumbs in-line plus a "Browse…" button; clicking the Source title bar (or Browse) opens a floating popup with the full grid + per-thumb accept/reject and remove. |
| 26 | Box-select multiple nodes | DONE | Drag from empty canvas paints a selection rect (Shift to add to existing). Drag-move a selected node moves the whole selection. Right-click → "Delete N selected". |
| 27 | Pan and zoom node editor | DONE | Middle-mouse drag and Space+drag pan; Cmd/Ctrl+scroll zooms toward the pointer (clamped 0.4×–2.0×). Grid pattern follows the pan offset. |
| 28 | Per-instance image viewer | DONE | `state.image_viewer_picks: HashMap<u64, ImageViewerPick>` keyed on PanelId. Each Image Viewer panel keeps its own pick: Primary (follows global selection), StackingPreview, or a specific Asset. Two viewer panels can show different images side-by-side. |
| 29 | Wire creation in node editor works | DONE | Output ports moved into foreground egui Areas with `Sense::click_and_drag()`. Drag from output → drop on input creates an edge. Click-then-click also works as a fallback. |
| 30 | Pan/zoom in node editor works | DONE | Driven by raw input (egui `Sense` only senses primary-button drags by default). Middle-drag and Space+left-drag pan; scroll zooms toward the pointer. |
| 31 | Default Stacking workspace | DONE | Vertical split: top = Image Viewer (defaults to live preview), bottom = Stacking Editor. |
| 32 | Histogram reads from stacking_preview pixels | DONE | `pixel_cache` was never populated; histogram now reads `stacking_preview.frame_pixels` for thumbnails (or `stacking_preview.output` for the live preview). |
| 33 | Alignment Viewer panel | DONE | `panels::alignment_viewer`. Pick group / reference / secondary, blend slider, Δx/Δy sliders. Stores `Alignment{dx,dy}` per `(collection_id, asset_id)` in `state.alignments`. Compose-in-Rust blend preview cached by content key in `image_cache`. |
| 34 | Stack honors per-frame alignment | DONE | `eval_source` shifts each frame by its `Alignment` before passing to the Stack node, so misaligned frames register against the reference before reduction. |
| 35 | Binary-tree merge cache | TODO | See "Alignment merge tree" section below. |
| 36 | Star-based auto-align | SUPERSEDED → 41 | First attempt: triangle-matched stars + Procrustes solver. Collapsed on real preview JPEGs because tile-median bg models cannot distinguish "saturated foreground" (orange roof) from "saturated diffuse object" (Orion nebula) — whatever rule masks one masks the other, and the triangle matcher had nothing to vote on. Replaced by item 41. |
| 37 | Per-frame quality scoring | DONE | `quality.rs`: focus = variance-of-Laplacian over the brightest 32 stars, plus star count + median FWHM. Cached in `state.stacking_preview.frame_quality`. |
| 38 | "Top X% of median" quality slider | DONE | New row in the alignment viewer's slider bar. Drives `quality::apply_threshold` which flips `accepted` on items below `median × threshold`. The user can still override per-frame from the stacking editor popup. |
| 39 | Earth-rotation cross-check + auto-calibrated orientation | DONE | `align_verify.rs` predicts per-pair Δx/Δy from sidereal rate × cos(δ) × Δt × plate-scale. Auto-discovers `ImageOrientation` (8 candidates) by minimising residual on the first job. Persisted on `CollectionAlignment`. |
| 40 | Star tracker panel | DONE | `panels::star_tracker`. Star picker dropdown (#0..#31), left = stacked sub-pixel crops of just that star, right = trail overlay on a mid-frame with predicted Earth-rotation path dashed in green, bottom = residual table colour-coded by |r| (green <2 px, amber <10 px, red ≥10 px). |
| 41 | FFT phase-correlation alignment | DONE | New `align.rs`: high-pass via wide box-blur subtraction → 2-D Hann window → padded `rustfft` 2-D FFT → magnitude-normalised cross-power spectrum → inverse FFT → 3-point parabolic peak fit. Verified on `/tmp/amalthea-test/0027.jpg…0041.jpg`: pairwise alignments converge monotonically with sub-pixel residuals. `auto_align.rs` orchestrates: detects stars per frame, picks the reference (most detections = sharpest), runs `align::align` per pair, builds `TrackedStar` trails by projecting reference stars through the FFT-derived translation. Star detection still runs but only feeds visualization. |
| 42 | Tracked-star reference overlay | DONE | New row in alignment viewer: slider 4..=32 (default 8) for `tracked_star_count` + `show overlay` checkbox. Image viewer's `draw_tracked_star_overlay` reads `state.alignments[cid].star_trails` for the displayed asset and draws labelled `#0..#N-1` markers on the matched centroids. |
| 43 | Auto-pick reference frame | DONE | `auto_align::run_auto_align` defaults to the frame with the most detected stars (a proxy for sharpest + deepest exposure). The picked id is round-tripped through `AlignJobResult::chosen_reference` and persisted into `CollectionAlignment.reference`. Manual override still wins. |

## Verification results — last run

```
PASS page_loads
PASS title_is_amalthea
PASS canvas_present
PASS wasm_attaches_no_console_error
WARN canvas_has_content                     (GPU-dependent, headless)
WARN theme_chrome_color                     (GPU-dependent, headless)

6/6 hard checks passed (2 soft warnings)
```

## Design notes

### Theme (#5)
- Background tiers: `CANVAS=#0a1218`, `PANEL=#121a24`, `CHROME=#1a2532`, `RAISED=#222f3d`
- Accent: `ACCENT=#e8a040`, `ACCENT_DIM=#a87028`
- Text: `TEXT=#d8dde3`, `TEXT_DIM=#9ba6b3`, `TEXT_FAINT=#6b7785`
- Status: `SUCCESS=#6ec67a`, `WARN=#eec04d`, `ERROR=#e26a6a`
- Selection in egui: amber outline, not blue

### Node editor (#3)
- Storage: `state.stacking_nodes: Vec<StackingNode>` + `state.stacking_edges: Vec<StackingEdge>`
- Each node: rect with title bar (kind-colored), input/output ports as small circles on left/right edges
- Connections: cubic-bezier sampled into line segments
- Right-click empty canvas → "Add Source/Reject/Calibrate/Effect/Stack/Output"
- Click an output port, then click an input port → wire created (no duplicates)
- Drag node body to move; right-click node → Delete

### Multi-select (#6)
- `state.timeline_selection: HashSet<String>` (asset IDs) for batch ops
- `selected_asset` continues to track the *primary* (last clicked) for image viewer
- Cmd/Ctrl-click: toggle in selection set
- Shift-click: range from anchor (last single-clicked)
- Plain click: replace selection with single asset
- Right-click on a thumb in the selection: context menu with "Add N to stacking session" and "Clear selection". If right-clicking an unselected thumb the action operates on just that one.

### Web build (#7)
The Cargo.lock issue: adding wasm-only feature flags (uuid `js`, getrandom `wasm_js`) at the workspace level forced lock regeneration. Fixed by:
1. Adding the wasm-only deps **only in `amalthea-web`** (not the workspace-wide `amalthea-app`).
2. Telling `craneLib.buildDepsOnly` for the host targets to `--workspace --exclude amalthea-web`, so the host dep cache never resolves with WASM features.

The wasm-bindgen-cli version must match the crate's `wasm-bindgen` version exactly. Nixpkgs ships an older version, so we build wasm-bindgen-cli 0.2.120 from crates.io with explicit hashes.

### Verification framework (#8)
- Location: `users/hanemile/projects/go/amalthea/test/verify/`
- `playwright-go` v0.4902.0
- `Playwright.Install()` autodownloads Chromium on first run
- Backend launched on a free port via `LISTEN_ADDR=127.0.0.1:0` style port allocation
- Page is a single egui canvas — most assertions read `canvas.toDataURL()` and sample pixels
- Pixel checks marked `softCheck` so headless-without-GPU runs still pass

## Test matrix

| Feature | Manual | Playwright | Status |
|---|---|---|---|
| App boot, no crashes | y | y | DONE |
| Workspace rename via double-click | y | y (TODO) | impl done, click-driver test pending |
| Sky zoom independent per panel | y | n (canvas-internal) | impl done |
| Stacking thumbnail preview shows | y | y (TODO) | impl done |
| Node editor renders nodes | y | y (TODO) | impl done |
| Theme contrast | y | y (pixel sample) | DONE |
| Multi-select adds to stacking | y | y (TODO) | impl done |

## Alignment merge tree (planned)

Goal: stack N frames in a way that scales, aligns sub-stacks before
combining them with their siblings, and caches every intermediate so
small parameter tweaks don't redo unrelated work.

Sketch of the shape:

```
              root
             /    \
          stack    stack
          /  \     /   \
         f0  f1   f2   f3        (leaves are individual frames)
```

Each internal node owns:

- `inputs: [NodeId; 2]` — its two children
- `align: Alignment` — how the right child is offset against the left
- `method: StackMethod` — the reduction used at this level
- `cache: Option<FramePixels>` — last computed output
- `key: u64` — a hash of (left.key, right.key, align, method)

Invalidation: when a leaf's pixel buffer or alignment changes, walk up
the parent chain bumping `key` and dropping `cache`. Independent
sub-trees (e.g. an unrelated dark group) keep their cache.

Why we want this:

- **Locality of edits**: tweak one frame's `Alignment` and only the
  ancestor chain (log N nodes) rebuilds.
- **Hierarchical alignment**: aligning sub-stacks instead of N×N pairs
  reduces noise and gives a much cleaner reference at each level.
- **Preview vs full**: the same tree can run on preview-size buffers
  for live tweaks and on full-resolution buffers for the final save.

What's done so far (#33, #34):

- Per-frame `Alignment` model + `state.alignments` storage.
- Alignment Viewer panel with reference / secondary picker, Δx/Δy
  sliders, and a compose-blend preview cached by content key.
- The Stack evaluator applies `Alignment` per frame before reducing,
  so the live preview already responds to slider edits.

Next steps (not yet done):

- Build a tree explicitly inside the Stack node when N > 2; today the
  Stack does a flat reduction.
- Cache each tree node's output in a hash-keyed `LruCache<u64, FramePixels>`
  so repeated runs reuse intermediates.
- Auto-align: cross-correlation pass at coarse pyramid levels to seed
  the offsets, then per-pair refinement.

## Star-based auto-align — out of scope (deferred)

Captured here so the design intent doesn't get lost. The current
auto-align (#36–#40) handles 99% of practical imaging on an equatorial
mount; the items below extend it to alt-az / unconventional setups
and to fully metadata-free workflows.

- **Field rotation around image centre for un-tracked alt-az
  mounts.** The Procrustes solver already returns a per-frame `theta`,
  so the data path is in place; what's missing is a *predictor* on the
  verifier side that turns (alt, az, latitude, Δt) into an expected
  rotation rate, so the cross-check works for alt-az imaging the same
  way the translation cross-check works for equatorial. Likely lives
  next to `align_verify::predict_sky_drift` as
  `predict_field_rotation`.
- **Plate solving (full WCS).** Not needed for the *self-consistent*
  rotation cross-check we ship — that only relies on Δt + plate scale
  + cos(δ). A full WCS would let us drop `trip_target_ra` /
  `trip_target_dec` and discover them automatically; sketch:
  `astap` / `astrometry.net` shell-out, or a vendored GAIA subset for
  a pure-Rust path. Owner: TBD.
- **GPU compute for star detection.** Detection on a 1440²-px buffer
  is ~50 ms single-threaded today. Not worth a wgpu compute pass yet;
  revisit if we move to full-resolution images (>16 MP) or batch
  scoring across hundreds of frames.
- **Per-star outlier rejection inside `match_and_solve`.** The fit is
  currently least-squares over inliers selected by greedy nearest-
  neighbour. A second-pass refinement (drop pairs with > 2σ residual,
  re-fit) would tighten `confidence` numbers and let us reduce the
  9-px gating window in trail building.
- **Sub-frame timestamp interpolation** for sub-second exposures.
  `chrono` precision is fine; what's missing is reading the EXIF
  `SubSecTimeOriginal` field (Immich currently doesn't expose it).
  When it lands we just thread it through `quality::pick_best_timestamp`.

## Ideas log

- Hot-reload of import URL (poll for changes when album updates)
- Star catalog from Hipparcos (binary blob, mmap)
- IndexedDB-backed image cache for WASM persistence across reloads
- Color-managed image viewer (treat decoded RGB as sRGB, optional Rec.2020)
- Node editor: typed sockets ("frames" vs "calibration") so wires only connect compatible ports
- Stacking pipeline executor (fan-in: rejects subtract from sources, calibration applies, then stack reduces)
- Per-frame quality metrics (star count, FWHM, roundness) populated on import

## Open questions

- Click-driving canvas-internal egui widgets from Playwright requires synthetic mouse coords against the canvas bounding box — write a helper for that next round?
- Move CSS fallback bg in index.html to use `theme::CANVAS` so the loader matches the rendered canvas while wgpu is still warming up?

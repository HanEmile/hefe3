# AppState reference

Last reviewed: 2026-05-01 | Review frequency: monthly

`AppState` (in `crates/amalthea-app/src/state.rs`) is the single
mutable struct passed to every panel renderer. Fields are grouped
logically below.

## Selection / browsing

| field | purpose |
|---|---|
| `selected_asset: Option<AssetResponse>` | "primary" — last clicked thumbnail; drives the default Image Viewer view |
| `search_results: Vec<AssetResponse>` | most recent album / search result, populates the Gallery |
| `image_zoom: f32`, `image_offset: Vec2` | zoom + pan for the Image Viewer panel (shared across viewers) |

## Caches

| field | purpose |
|---|---|
| `thumbnail_cache: ThumbnailCache` | LRU of GPU `TextureHandle`s for grid thumbs |
| `image_cache: ImageCache` | LRU of GPU `TextureHandle`s for full preview images and the synthesised stacking-preview texture |
| `pending_requests: PendingRequests` | dedup tracker for in-flight HTTP fetches |

## Logging

| field | purpose |
|---|---|
| `log_entries: Vec<LogEntry>` | ring buffer (max 1000) of in-app log lines |
| `log_level_filter: Vec<String>` | which levels are visible in the log panel |

## Collections (image groups)

| field | purpose |
|---|---|
| `collections: Vec<Collection>` | named, typed groups of asset IDs |
| `next_collection_id: u64` | monotonic id allocator |
| `last_collection_id: Option<u64>` | last group used in the Gallery's "Add to…" menu |

`Collection { id, name, kind, items: Vec<CollectionItem> }`,
`CollectionItem { asset_id, filename, accepted: bool }`,
`CollectionKind = Lights | Darks | Bias | Stacked | Custom`.

Source nodes in the stacking graph reference a `collection_id` instead
of carrying their own asset list.

## Stacking graph

| field | purpose |
|---|---|
| `stacking_nodes: Vec<StackingNodeSlot>` | nodes in the editor graph |
| `stacking_edges: Vec<StackingEdge>` | edges (with `to_port: In::{Default, Lights, Darks}`) |
| `stacking_next_node_id: u64` | id allocator |
| `stacking_drag` / `stacking_pending_edge` / `stacking_box_drag` | transient drag/wire/slice state |
| `stacking_selection: HashSet<u64>` | selected node IDs (for box-select + multi-drag) |
| `stacking_pan: Vec2`, `stacking_zoom: f32` | canvas viewport transform |
| `stacking_open_popup: Option<u64>` | which Source node has its group-browser popup open |
| `stacking_default_seeded: bool` | reset when collections go empty so a fresh import re-seeds |

`StackingNodeSlot { id, pos, node, measured_height }`.
`StackingNode = Source(SourceNode) | Stack(StackNode) | Adjust(AdjustNode) | Output(OutputNode)`.
See `docs/stacking.md` for the per-variant param fields.

## Stacking evaluation

`stacking_preview: StackingPreview` is the runtime side of the graph:

| field | purpose |
|---|---|
| `frame_pixels: HashMap<String, FramePixels>` | preview-size (~1440px) raw RGBA |
| `frame_pixels_low: HashMap<String, FramePixels>` | scrub-size (~480px) mirror, lazy |
| `frame_quality: HashMap<String, FrameQuality>` | per-frame focus / star count / FWHM, lazy |
| `frame_timestamps: HashMap<String, DateTime<Utc>>` | parsed once at import (EXIF or file-created, whichever has more sub-second precision) |
| `output: Option<FramePixels>` | latest hi-res result (saved by Output node) |
| `stack_cache`, `stack_cache_low` | pre-Adjust Stack-output cache, keyed on inputs |
| `dark_mean_cache`, `dark_mean_cache_low` | per-pixel dark master + hot-pixel count |
| `running, dirty, error` | UI status flags |
| `job_version: u64` | bumped on each dirty event; stale results are dropped |
| `diagnostics: Option<StackDiagnostics>` | last-run lights/darks/hot/W×H for the UI |

Background-job plumbing:

- `stacking_jobs: Option<StackingJobs>` — mpsc for full-res stack results.
- `align_jobs: Option<AlignJobs>` + `align_running` + `align_status` — auto-align job state.

## Alignment

| field | purpose |
|---|---|
| `alignments: HashMap<u64, CollectionAlignment>` | per-collection ref + per-asset offsets + star trails + auto-discovered orientation |
| `alignment_ui: AlignmentUi` | Alignment Viewer panel state (collection_id, secondary, blend, **quality_threshold**, **tracked_star_count** 4..=32, **overlay_tracked_stars**) |
| `star_tracker_ui: StarTrackerUi` | Star Tracker panel state (collection_id, selected_star, crop_size, show_predicted) |

`CollectionAlignment { reference, offsets: HashMap<asset_id, Alignment{dx,dy,theta,anchor,confidence}>,
orientation: Option<ImageOrientation>, star_trails: Vec<TrackedStar> }`.
The reference frame is the origin; everyone else is rigidly transformed
(translation + rotation around image centre) by its `Alignment` before
stacking. `orientation` maps image-pixel axes to sky axes (flipX × flipY
× transpose, 8 combinations) for the Earth-rotation cross-check.
`star_trails` tracks the brightest reference stars across every frame
for the Star Tracker panel.

See `docs/alignment.md` for the full pipeline.

## Theme

| field | purpose |
|---|---|
| `theme: Theme` | live colour palette |
| `theme_dirty: bool` | settings-panel edits flip this; persist layer flushes |
| `theme_loaded: bool` | once-per-run guard for `persist::load_initial` |

See `docs/theme.md`.

## Per-panel state

| field | purpose |
|---|---|
| `image_viewer_picks: HashMap<u64, ImageViewerPick>` | per-PanelId pick — Primary / StackingPreview / Asset(id) |
| `sky_panel_zooms: HashMap<u64, f32>` | per-PanelId sky zoom override when `sky_sync_zoom = false` |

## Trip planner / Sky viewer

`trip_*` fields hold observer location / target / equipment for the
trip planner; `sky_*` hold viewport state for the sky viewer. See the
panel files for the meaning of each.

## Import pipeline

| field | purpose |
|---|---|
| `import_rx`, `import_tx` | mpsc connecting the import worker thread to the UI |
| `thumbnails_loaded`, `thumbnails_total` | progress indicator |
| `share_source: ShareSource { immich_base, key }` | last opened Immich share — used for on-demand fetches |
| `requested_images: HashSet<String>` | dedup the on-click full-image fetch |

## Cross-references

- `docs/architecture.md` — single-state pattern + frame lifecycle
- `docs/stacking.md` — what each `StackingNode::*` variant carries and how the evaluator uses it
- `docs/alignment.md` — `Alignment`, `CollectionAlignment`, auto-align job

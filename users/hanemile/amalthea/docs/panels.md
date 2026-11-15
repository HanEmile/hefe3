# Panels

Last reviewed: 2026-05-01 | Review frequency: monthly
Last touched: gallery sidebar, stacking inspector, fixed-size nodes (2026-05-01).

Each panel is a single file under `crates/amalthea-app/src/panels/`
with a `pub fn show(...)` that draws into a `Rect` provided by the
BSP layout. Panels never own state; they read and mutate `AppState`
directly.

## Conventions

- Signature: `show(ui, [panel_id,] [header_extra,] rect, state: &mut AppState)`.
- Panels read colours from `state.theme.*` only (never hardcode RGB).
  See `docs/theme.md`.
- Panels that need to render controls in the BSP's header bar take a
  `header_extra: Rect` parameter (currently only the Image Viewer).
- Internal widget bodies live in foreground `egui::Area`s when they
  must beat the canvas-wide drag handler to a click (Stacking Editor
  node bodies, ports).

## BSP integration

The Blender-style layout engine in `bsp/` calls back into the panel
dispatcher in `lib.rs::render_panel`:

```rust
PanelKind::ImageViewer => panels::image_viewer::show(ui, panel_id, header_extra, rect, state)
PanelKind::Histogram => panels::histogram::show(ui, rect, state)
…
```

`render_panel` is the only place that knows panel→variant mapping.
Adding a new panel = add a `PanelKind` variant + a module file + a
`render_panel` arm.

## Panel inventory

### Image Viewer (`image_viewer.rs`)

Per-panel pick via `state.image_viewer_picks[PanelId]`:
`Primary | StackingPreview | Asset(id)`. The header strip carries a
ComboBox so two viewers can show different images side-by-side.
Mouse wheel zooms toward the cursor; drag pans; double-click resets.

Two overlays draw on top of the displayed image:

- **Anchor crosshair** (`draw_alignment_overlays`) — single labelled
  point per asset, opt-in via the Stacking Inspector's "show ref
  point" toggle (writes to `state.alignment_overlay_show`).
- **Tracked-star markers** (`draw_tracked_star_overlay`) — labelled
  `#0..#N-1` circles on every reference star whose centroid was
  located in the displayed frame. Reads
  `state.alignments[cid].star_trails`, gated on
  `state.alignment_ui.overlay_tracked_stars`. The slider that drives
  the count lives in the Alignment Viewer.

### Gallery (`timeline.rs` — display name "Gallery")

Two columns:
- **Left sidebar** (`SIDEBAR_W = 220`): list of collections with
  kind chip + name + item count. Double-click name to rename inline.
  Right-click → Rename / Sync (binds to the current Immich share or
  re-pulls from the bound album) / Delete. "New" button at the top
  creates a new group of any `CollectionKind`.
- **Right grid**: scrolling thumbnail grid of `state.search_results`.
  Each thumb shows up to 4 kind-coloured badges in the top-left for
  the groups it's already in.

Click selects; Cmd-click toggles in `timeline_selection`; Shift-click
selects a range. Right-click opens "Add N images to <group>" submenu
listing existing collections + "New <Kind> group" for each
`CollectionKind`.

### Stacking Editor (`stacking_editor.rs`)

Pan/zoom canvas with the typed node graph. Header has Stack / Save
PNG / Reset buttons. **Node frames are fixed-size on screen — zoom
only moves them around**, like a Blender node tree. Body widgets
render at native size, never overflow their frame. See
`docs/stacking.md` and `docs/ui-conventions.md`.

### Stacking Inspector (`stacking_inspector.rs`)

Right-side sidebar (default in the Stacking workspace). Pick a
collection, see one row per frame: thumbnail + filename + Δx/Δy and
anchor coords + an `anchor` checkbox. Toggling it on adds the
asset to `state.alignment_overlay_show`; the Image Viewer reads
that set and overlays a bullseye crosshair at the recorded anchor
of any displayed asset whose toggle is on. Use it to confirm that
the auto-align job locked onto the same star across frames.

### Alignment Viewer (`alignment_viewer.rs`)

Top bar: collection picker + reference / secondary frame pickers +
Auto-align button. Centre: blended preview (`(1-blend)·ref + blend·sec_shifted`)
with a centre crosshair. Bottom bar: Δx / Δy / Blend sliders. Edits
write to `state.alignments[collection_id].offsets[asset_id]`.

### Sky Viewer (`sky_viewer.rs`)

Stereographic horizon plot, bright-star catalog, target dot. Drag
horizontally to scrub time; scroll to zoom. Per-panel zoom by default
(`sky_sync_zoom = false`).

### Histogram (`histogram.rs`)

R/G/B + luminance overlay for the *visible* image. Reads pixels from
`state.stacking_preview.frame_pixels[asset_id]` for thumbnails or
`state.stacking_preview.output` for the live preview.

### EXIF Inspector (`exif_inspector.rs`)

Grid of EXIF key/value pairs from `selected_asset.exif_info`.

### Star Tracker (`star_tracker.rs`)

Visualises the star trails and Earth-rotation cross-check produced by
the auto-align job. Three regions:

1. **Top bar** — Group picker, Star picker (#0..#31), crop-window
   size slider, *show predicted path* toggle.
2. **Left viewport** — sub-pixel-aligned average of the chosen
   star's crop across every frame where it was detected. A clean
   spot = good alignment; a streak = residual drift or rotation.
3. **Right viewport** — trail of the star's measured centroids
   overlaid on a mid-frame backdrop, with the predicted Earth-rotation
   path as dashed-green circles when site / target / optics are set
   on the trip planner.
4. **Bottom table** — per-frame Δt, measured / predicted Δx,Δy,
   `|r|` colour-coded green (<2 px) / amber (<10 px) / red (≥10 px).

State lives in `state.star_tracker_ui`. Read-only against
`state.alignments[cid].star_trails` and
`state.stacking_preview.frame_timestamps`.

See `docs/alignment.md` for the math and the full pipeline.

### Settings (`settings.rs`)

Live theme editor (see `docs/theme.md`), backend / Immich / OIDC
/ db_path text fields, cache stats. Every change auto-saves via
`persist::flush`.

### Log Viewer (`log_viewer.rs`)

Filter chips for ERROR / WARN / INFO / DEBUG (filled when active).
Scrolling list of `state.log_entries`.

### Trip Planner (`trip_planner.rs`)

Observer location + target + telescope params; computes pixel scale,
Sun/Moon altitude, target altitude over time.

## Adding a new panel

1. Create `crates/amalthea-app/src/panels/<name>.rs` with
   `pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState)`.
2. Register the module in `panels/mod.rs`:
   - `pub mod <name>;`
   - Add a `PanelKind::<Variant>`
   - Add to `PanelKind::ALL`
   - Add a `name()` arm
3. Dispatch in `lib.rs::render_panel`.
4. Optional: include in a default workspace in `workspace.rs`.

## Cross-references

- `docs/architecture.md` — frame lifecycle that calls the panels
- `docs/theme.md` — colour token usage
- `docs/ui-conventions.md` — gestures, density rules

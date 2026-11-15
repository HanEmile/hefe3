# Theme

Last reviewed: 2026-05-01 | Review frequency: monthly

Source: `crates/amalthea-app/src/theme.rs`,
`crates/amalthea-app/src/persist.rs`,
`crates/amalthea-app/src/panels/settings.rs`.

## Tokens

`Theme` is a struct of `Color32` fields, all editable live from the
Settings panel:

| token | role |
|---|---|
| `canvas` | outermost background (between panels) |
| `panel` | default panel fill |
| `chrome` | header / status / chrome bars |
| `raised` | hovered surface tint |
| `border` | hairline rules + panel outlines |
| `accent` | selections, focus rings, active tabs |
| `accent_dim` | dimmed accent for non-focused states |
| `text` / `text_dim` / `text_faint` | foreground tiers |
| `success` / `warn` / `error` / `info` / `debug` | status colours (drive log-level chips, alerts) |
| `corner_radius: u8` | global widget corner radius (default 0 — sharp / power-user) |

`Theme::dark()` is the default astrophotography preset (deep navy +
amber accent). `Theme::light()` mirrors the structure for a
high-contrast light mode. Both are reachable from preset buttons in
the Settings panel.

`Theme::is_dark()` peeks the canvas luma to decide whether to start
from `Visuals::dark()` or `Visuals::light()` before applying tokens.

## Live application

`theme::apply(ctx, &theme)` runs every frame inside `update()`:

1. Build `egui::Visuals` from the current tokens.
2. Push tightened `egui::style::Spacing` (item 4×3 px, button pad
   4×2, indent 12, interact 28×18) — the "power-user density".
3. `ctx.set_style(...)`.

Cost: a struct copy + a context pointer write. Cheap enough to do
unconditionally each frame, which is what makes settings-panel edits
feel instant.

## Panel-level usage

Panels read `state.theme.<token>` directly when painting custom
shapes. Hardcoded `Color32::from_*` is reserved for content that
isn't theme-driven (raw image pixels, the sky viewer's deliberately
green horizon line, the stacking-editor's grid lines).

`theme::level_color(theme, "ERROR" | "WARN" | "INFO" | "DEBUG")` is
the canonical way log-level → colour mapping is done so the log
panel chips and inline log text agree.

## Persistence

`persist.rs` round-trips the theme through the backend's
`/db/settings/theme.json` endpoint:

- `persist::load_initial(state)` runs on the first frame; blocks for
  ≤2s on the network round-trip via a single-thread tokio runtime
  spawned on a worker. Falls back to defaults if the GET fails.
- `persist::flush(state)` runs every frame and short-circuits on
  `!theme_dirty`. When dirty, fires-and-forgets a PUT to the same
  endpoint. The flag is reset before the request goes out so rapid
  edits coalesce into one in-flight save at a time per change.
- `Color32` is serialised as `[u8; 3]` (RGB) via custom serde
  attributes — egui's `Color32` doesn't implement Serialize/Deserialize.

## Settings panel UX

`panels::settings::show` exposes:

- "Dark preset" / "Light preset" buttons (instant swap).
- A grid of `egui::color_edit_button_srgb` for every token.
- A `Slider<u8>` for `corner_radius`.
- Editable text fields for `backend_url`, `immich_url`,
  `oidc_issuer`, `oidc_client_id`, `db_path`. Each change to a
  string is also auto-persisted (theme + config edits flow through
  the same dirty/flush plumbing today; per-field config persistence
  is a future task).

## Cross-references

- `docs/state.md` — `theme`, `theme_dirty`, `theme_loaded` fields
- `docs/ui-conventions.md` — power-user spacing rationale

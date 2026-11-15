# UI conventions

Last reviewed: 2026-05-01 | Review frequency: monthly

Power-user-first interaction. Sharp corners by default, tight
spacing, gestures that match Blender / pro tools.

## Density

`theme::apply` pushes `egui::style::Spacing` defaults:
- `item_spacing = (4, 3)`
- `button_padding = (4, 2)`
- `interact_size = (28, 18)`
- `indent = 12`
- `corner_radius = 0` (theme-controlled)

Panels never set their own spacing — they trust the global style. If
something needs custom rounding (e.g. the stacking-preview pill
indicator), that's painted directly with `painter.rect_*`, not via
egui's widget-style path.

## Selection model (Gallery)

| gesture | action |
|---|---|
| click | replace selection with single asset; promote to `selected_asset` |
| Cmd/Ctrl-click | toggle in `timeline_selection` |
| Shift-click | range-select from anchor to clicked thumb |
| right-click | context menu over current selection (or single thumb if not in selection) |

`timeline_anchor` tracks the last single-clicked asset id so range
selection has a defined endpoint.

## Stacking node editor gestures

Canvas-level (no node under cursor):

| gesture | action |
|---|---|
| left-drag (no Shift) | box-select (replaces selection) |
| Shift + left-drag | **slice**: cut every edge that the line crosses |
| middle-drag | pan |
| Space + left-drag | pan (Blender muscle memory) |
| scroll wheel | zoom toward cursor (no modifier needed; clamped 0.4×–2.0×) |
| right-click | "Add node…" submenu (Source from group / Source / Stack / Adjust / Output / Reset) |

Box-select with Shift held at drag-start is the **slice** gesture
(cuts edges) — distinct from "additive" selection. Holding Shift on
the slice end-tap doesn't matter; what matters is the modifier at
the start of the drag.

Node-level:

| gesture | action |
|---|---|
| click title | select (replaces) + open Source group browser if it's a Source |
| Shift/Cmd-click title | toggle in selection |
| drag title | move node (or whole selection if multi-selected) |
| right-click title | "Delete node" / "Delete N selected" |
| click output port | start a wire (click-click flow) |
| drag output → input | finish a wire (drag-and-drop flow) |
| click empty canvas while wiring | cancel pending wire |

## Image Viewer

| gesture | action |
|---|---|
| scroll | zoom toward cursor (clamped 0.1×–50×) |
| drag | pan |
| double-click | reset zoom + pan |
| header dropdown | switch between Primary / Stacking preview / specific asset (per-panel) |

## Workspace tabs

| gesture | action |
|---|---|
| click | activate workspace |
| double-click | rename inline (TextEdit; Enter / focus-loss commits) |
| right-click | context menu (Rename, Delete) |
| `+` button | new workspace |

## Sky Viewer

- Drag horizontally: scrub time.
- Scroll: zoom (per-panel by default; toggle in panel header).

## Theme rule

**Never hardcode `Color32::from_*` for chrome / text / borders.**
Read `state.theme.<token>`. Hardcoded RGB is reserved for content
colours that aren't themable (raw image pixels, explicit semantic
markers like sky-viewer horizon green).

## Cross-references

- `docs/theme.md` — token list + persistence
- `docs/panels.md` — per-panel UX summary
- `docs/stacking.md` — node-graph behaviour

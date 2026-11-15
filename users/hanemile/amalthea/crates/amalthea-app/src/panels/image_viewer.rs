/// Full-resolution image viewer with pan and zoom.
///
/// Each Image Viewer panel keeps its own pick (via `state.image_viewer_picks`
/// keyed on PanelId) so two viewers can show different images side-by-side.
/// The header strip carries an asset-picker dropdown that updates the
/// per-panel pick.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use egui::{Align2, Color32, FontId, Pos2, Rect, Sense, Ui, Vec2};

use super::util::trim;
use crate::state::{AppState, ImageViewerPick};

/// Zoom speed factor applied per scroll delta unit.
const ZOOM_SPEED: f32 = 0.001;
/// Minimum allowed zoom level.
const MIN_ZOOM: f32 = 0.1;
/// Maximum allowed zoom level.
const MAX_ZOOM: f32 = 50.0;

/// Render the image viewer panel.
pub fn show(ui: &mut Ui, panel_id: u64, header_extra: Rect, rect: Rect, state: &mut AppState) {
    show_header_picker(ui, panel_id, header_extra, state);

    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.canvas);

    // Resolve which image to display for *this* panel.
    let pick = state
        .image_viewer_picks
        .get(&panel_id)
        .cloned()
        .unwrap_or_default();
    let asset_id: Option<String> = match pick {
        ImageViewerPick::Asset(id) => Some(id),
        ImageViewerPick::StackingPreview if state.stacking_preview.output.is_some() => {
            Some(crate::stacking::preview_asset_id().to_string())
        }
        ImageViewerPick::StackingPreview => None,
        ImageViewerPick::Primary => match &state.selected_asset {
            Some(a) => Some(a.id.clone()),
            None if state.stacking_preview.output.is_some() => {
                Some(crate::stacking::preview_asset_id().to_string())
            }
            None => None,
        },
    };

    let Some(asset_id) = asset_id else {
        painter.text(
            rect.center(),
            Align2::CENTER_CENTER,
            "No image selected",
            FontId::proportional(16.0),
            state.theme.text_faint,
        );
        return;
    };

    // Snapshot the texture id + size once so the rest of this function
    // doesn't hold a borrow on `state.image_cache`. We later reborrow
    // through `state.image_cache.peek` if we need the texture again,
    // but for compositing the image + overlays we just need id + size.
    let texture_meta: Option<(egui::TextureId, Vec2)> = state
        .image_cache
        .get(&asset_id)
        .map(|t| (t.id(), t.size_vec2()));

    let Some((texture_id, texture_size)) = texture_meta else {
        painter.text(
            rect.center(),
            Align2::CENTER_CENTER,
            "Loading…",
            FontId::proportional(14.0),
            state.theme.text_faint,
        );
        return;
    };

    let response = ui.allocate_rect(rect, Sense::click_and_drag());

    if response.hovered() {
        let scroll_delta = ui.input(|i| i.smooth_scroll_delta.y);
        if scroll_delta != 0.0 {
            let old_zoom = state.image_zoom;
            state.image_zoom = (old_zoom + scroll_delta * ZOOM_SPEED * old_zoom)
                .clamp(MIN_ZOOM, MAX_ZOOM);

            if let Some(pointer_pos) = ui.input(|i| i.pointer.hover_pos()) {
                let center = rect.center();
                let pointer_offset = pointer_pos - center;
                let zoom_ratio = state.image_zoom / old_zoom;
                state.image_offset = state.image_offset * zoom_ratio
                    + pointer_offset * (zoom_ratio - 1.0);
            }
        }
    }

    if response.dragged() {
        state.image_offset += response.drag_delta();
    }

    if response.double_clicked() {
        state.image_zoom = 1.0;
        state.image_offset = Vec2::ZERO;
    }

    let image_size = texture_size * state.image_zoom;
    let image_center = rect.center() + state.image_offset;
    let image_rect = Rect::from_center_size(image_center, image_size);

    painter.with_clip_rect(rect).image(
        texture_id,
        image_rect,
        Rect::from_min_max(Pos2::ZERO, Pos2::new(1.0, 1.0)),
        Color32::WHITE,
    );

    // Alignment-anchor crosshair overlay for any frame the user has
    // toggled "show ref point" on in the Stacking Inspector.
    draw_alignment_overlays(&painter, &asset_id, image_rect, texture_size, state);

    // Tracked-star overlay (#0..#N-1) — drawn whenever the alignment
    // viewer's "show overlay" checkbox is on AND we have trails for a
    // collection containing this asset.
    if state.alignment_ui.overlay_tracked_stars {
        draw_tracked_star_overlay(&painter, &asset_id, image_rect, texture_size, state);
    }

    // Zoom indicator (bottom-right pill).
    let zoom_text = format!("{:.0}%", state.image_zoom * 100.0);
    let galley = painter.layout_no_wrap(
        zoom_text.clone(),
        FontId::monospace(11.0),
        state.theme.text,
    );
    let text_size = galley.size();
    let pill_rect = Rect::from_min_size(
        Pos2::new(
            rect.max.x - text_size.x - 20.0,
            rect.max.y - text_size.y - 14.0,
        ),
        Vec2::new(text_size.x + 12.0, text_size.y + 6.0),
    );
    let pill_bg = state.theme.chrome;
    painter.rect_filled(
        pill_rect,
        2.0,
        Color32::from_rgba_unmultiplied(pill_bg.r(), pill_bg.g(), pill_bg.b(), 200),
    );
    painter.text(
        pill_rect.center(),
        Align2::CENTER_CENTER,
        zoom_text,
        FontId::monospace(11.0),
        state.theme.text,
    );
}

// ---------------------------------------------------------------------------
// Header-extra: asset picker dropdown
// ---------------------------------------------------------------------------

const PRIMARY_ENTRY: &str = "__primary__";
const PREVIEW_ENTRY: &str = "__stacking_preview__";

fn show_header_picker(ui: &mut Ui, panel_id: u64, header_extra: Rect, state: &mut AppState) {
    if header_extra.width() < 120.0 {
        return;
    }

    let preview_available = state.stacking_preview.output.is_some();
    let pick = state
        .image_viewer_picks
        .get(&panel_id)
        .cloned()
        .unwrap_or_default();

    let (current_marker, current_label) = match &pick {
        ImageViewerPick::StackingPreview => {
            (PREVIEW_ENTRY.to_string(), "Stacking preview".to_string())
        }
        ImageViewerPick::Asset(id) => {
            let label = state
                .search_results
                .iter()
                .find(|a| &a.id == id)
                .map(|a| trim(&a.original_file_name, 28))
                .unwrap_or_else(|| trim(id, 28));
            (id.clone(), label)
        }
        ImageViewerPick::Primary => match &state.selected_asset {
            Some(a) => (PRIMARY_ENTRY.to_string(), format!("(primary: {})", trim(&a.original_file_name, 22))),
            None if preview_available => (
                PRIMARY_ENTRY.to_string(),
                "(primary: stacking preview)".to_string(),
            ),
            None => (PRIMARY_ENTRY.to_string(), "(primary: none)".to_string()),
        },
    };

    let area_id = egui::Id::new(("image_viewer_picker_area", panel_id));
    egui::Area::new(area_id)
        .fixed_pos(header_extra.min)
        .order(egui::Order::Foreground)
        .show(ui.ctx(), |ui| {
            ui.set_clip_rect(ui.ctx().screen_rect());
            ui.horizontal(|ui| {
                ui.label(
                    egui::RichText::new("View:")
                        .size(10.0)
                        .color(state.theme.text_dim),
                );

                let mut picked: Option<String> = None;
                egui::ComboBox::from_id_salt(("image_viewer_picker", panel_id))
                    .selected_text(
                        egui::RichText::new(current_label)
                            .size(10.0)
                            .color(state.theme.text),
                    )
                    .width((header_extra.width() - 60.0).min(220.0).max(120.0))
                    .show_ui(ui, |ui| {
                        if ui
                            .selectable_label(current_marker == PRIMARY_ENTRY, "(primary)")
                            .clicked()
                        {
                            picked = Some(PRIMARY_ENTRY.to_string());
                        }
                        if preview_available
                            && ui
                                .selectable_label(
                                    current_marker == PREVIEW_ENTRY,
                                    "(stacking preview)",
                                )
                                .clicked()
                        {
                            picked = Some(PREVIEW_ENTRY.to_string());
                        }

                        if !state.search_results.is_empty() {
                            ui.separator();
                            for asset in &state.search_results {
                                let is_current = current_marker == asset.id;
                                let label = trim(&asset.original_file_name, 40);
                                if ui.selectable_label(is_current, label).clicked() {
                                    picked = Some(asset.id.clone());
                                }
                            }
                        }
                    });

                if let Some(id) = picked {
                    let new_pick = if id == PRIMARY_ENTRY {
                        ImageViewerPick::Primary
                    } else if id == PREVIEW_ENTRY {
                        ImageViewerPick::StackingPreview
                    } else {
                        ImageViewerPick::Asset(id)
                    };
                    state.image_viewer_picks.insert(panel_id, new_pick);
                    state.image_zoom = 1.0;
                    state.image_offset = Vec2::ZERO;
                }
            });
        });
}

/// Draw the alignment-anchor crosshair for the currently-displayed
/// asset, when the Stacking Inspector has marked it as "show ref
/// point". The anchor is in the source frame's pixel coordinates;
/// we map it through the same transform as the displayed image.
/// Overlay labelled markers (#0..#N-1) on the tracked reference stars
/// for whichever frame is currently displayed. Looks the asset up in
/// every collection's `star_trails` and draws the recorded centroid
/// for matching frames.
fn draw_tracked_star_overlay(
    painter: &egui::Painter,
    asset_id: &str,
    image_rect: Rect,
    texture_size: Vec2,
    state: &AppState,
) {
    if texture_size.x <= 0.0 || texture_size.y <= 0.0 {
        return;
    }
    // Find any collection whose trails include this asset. Multiple
    // collections might cover the same asset; we draw from the first
    // that matches — adequate for the common case where a frame is in
    // exactly one Lights group.
    for align_data in state.alignments.values() {
        if align_data.star_trails.is_empty() {
            continue;
        }
        let any_match = align_data
            .star_trails
            .iter()
            .any(|t| t.frames.iter().any(|(id, _)| id == asset_id));
        if !any_match {
            continue;
        }
        for trail in &align_data.star_trails {
            // Find this asset's slot in the trail.
            let Some((_, det)) = trail.frames.iter().find(|(id, _)| id == asset_id) else {
                continue;
            };
            let Some((sx_px, sy_px)) = det else { continue };
            let nx = sx_px / texture_size.x;
            let ny = sy_px / texture_size.y;
            let cx = image_rect.min.x + nx * image_rect.width();
            let cy = image_rect.min.y + ny * image_rect.height();

            painter.circle_stroke(
                Pos2::new(cx, cy),
                8.0,
                egui::Stroke::new(1.0, state.theme.accent),
            );
            painter.text(
                Pos2::new(cx + 11.0, cy - 2.0),
                egui::Align2::LEFT_BOTTOM,
                format!("#{}", trail.id),
                FontId::proportional(10.0),
                state.theme.accent,
            );
        }
        return;
    }
}

fn draw_alignment_overlays(
    painter: &egui::Painter,
    asset_id: &str,
    image_rect: Rect,
    texture_size: Vec2,
    state: &AppState,
) {
    if !state.alignment_overlay_show.contains(asset_id) {
        return;
    }
    // Find the anchor recorded for this asset in any collection's
    // alignment table (an asset can appear in multiple collections;
    // the anchor is the same coordinate either way, so the first hit
    // is fine).
    let anchor: Option<(u32, u32)> = state.alignments.values().find_map(|a| {
        a.offsets.get(asset_id).and_then(|al| al.anchor)
    });
    let Some((ax, ay)) = anchor else { return };

    if texture_size.x <= 0.0 || texture_size.y <= 0.0 {
        return;
    }

    // Map (ax, ay) ∈ [0, w]×[0, h] → screen position inside `image_rect`.
    let nx = ax as f32 / texture_size.x;
    let ny = ay as f32 / texture_size.y;
    let sx = image_rect.min.x + nx * image_rect.width();
    let sy = image_rect.min.y + ny * image_rect.height();

    // Outer ring + crosshair.
    let color = state.theme.accent;
    painter.circle_stroke(
        Pos2::new(sx, sy),
        12.0,
        egui::Stroke::new(1.5, color),
    );
    painter.line_segment(
        [Pos2::new(sx - 18.0, sy), Pos2::new(sx - 4.0, sy)],
        egui::Stroke::new(1.0, color),
    );
    painter.line_segment(
        [Pos2::new(sx + 4.0, sy), Pos2::new(sx + 18.0, sy)],
        egui::Stroke::new(1.0, color),
    );
    painter.line_segment(
        [Pos2::new(sx, sy - 18.0), Pos2::new(sx, sy - 4.0)],
        egui::Stroke::new(1.0, color),
    );
    painter.line_segment(
        [Pos2::new(sx, sy + 4.0), Pos2::new(sx, sy + 18.0)],
        egui::Stroke::new(1.0, color),
    );
}

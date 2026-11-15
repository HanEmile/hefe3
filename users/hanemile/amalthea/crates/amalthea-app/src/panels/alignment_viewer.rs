/// Alignment viewer — pick a collection + reference + secondary frame,
/// show them blended on top of each other, and tune the secondary's
/// `dx`/`dy` offset until features line up.
///
/// The offset is stored in `state.alignments[collection_id]` and the
/// stacker honors it — set the offset right here and the live preview
/// (in another image-viewer panel) updates accordingly.
///
/// MVP scope: 2D translation only. Rotation / sub-pixel alignment is a
/// next-pass concern; the data model already has room for it.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use egui::{Color32, FontId, Pos2, Rect, Stroke, Ui, Vec2};

use super::util::trim;
use crate::state::{Alignment, AppState, CollectionAlignment, FramePixels};

const TOP_BAR_H: f32 = 28.0;
const SLIDER_BAR_H: f32 = 96.0;

pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.panel);

    // Layout: top bar (selectors) | image area | slider bar.
    let top = Rect::from_min_size(rect.min, Vec2::new(rect.width(), TOP_BAR_H));
    let bottom = Rect::from_min_size(
        Pos2::new(rect.min.x, rect.max.y - SLIDER_BAR_H),
        Vec2::new(rect.width(), SLIDER_BAR_H),
    );
    let viewport = Rect::from_min_max(
        Pos2::new(rect.min.x, top.max.y),
        Pos2::new(rect.max.x, bottom.min.y),
    );

    show_top_bar(ui, top, state);
    show_viewport(ui, viewport, state);
    show_slider_bar(ui, bottom, state);
}

// ---------------------------------------------------------------------------
// Top bar — pick collection + reference + secondary
// ---------------------------------------------------------------------------

fn show_top_bar(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.chrome);
    painter.line_segment(
        [Pos2::new(rect.min.x, rect.max.y), Pos2::new(rect.max.x, rect.max.y)],
        Stroke::new(1.0, state.theme.border),
    );

    let area_id = egui::Id::new("alignment_viewer_top_bar");
    egui::Area::new(area_id)
        .fixed_pos(Pos2::new(rect.min.x + 6.0, rect.min.y + 4.0))
        .order(egui::Order::Foreground)
        .show(ui.ctx(), |ui| {
            ui.set_max_size(Vec2::new(rect.width() - 12.0, rect.height() - 8.0));
            ui.horizontal(|ui| {
                ui.label(
                    egui::RichText::new("Group:")
                        .size(10.0)
                        .color(state.theme.text_dim),
                );
                let current_label = state
                    .alignment_ui
                    .collection_id
                    .and_then(|id| state.collection(id))
                    .map(|c| format!("{} — {}", c.kind.label(), c.name))
                    .unwrap_or_else(|| "(pick a group)".to_string());

                let mut new_collection: Option<u64> = None;
                egui::ComboBox::from_id_salt("alignment_collection_pick")
                    .selected_text(
                        egui::RichText::new(current_label)
                            .size(10.0)
                            .color(state.theme.text),
                    )
                    .width(200.0)
                    .show_ui(ui, |ui| {
                        for c in &state.collections {
                            let label = format!("{} — {} ({})", c.kind.label(), c.name, c.items.len());
                            if ui
                                .selectable_label(state.alignment_ui.collection_id == Some(c.id), label)
                                .clicked()
                            {
                                new_collection = Some(c.id);
                            }
                        }
                        if state.collections.is_empty() {
                            ui.label(
                                egui::RichText::new("(no groups yet)")
                                    .size(10.0)
                                    .color(state.theme.text_faint),
                            );
                        }
                    });
                if let Some(id) = new_collection {
                    state.alignment_ui.collection_id = Some(id);
                    // Snapshot accepted ids first to avoid overlapping
                    // borrows below.
                    let accepted: Vec<String> = state
                        .collection(id)
                        .map(|c| {
                            c.items
                                .iter()
                                .filter(|it| it.accepted)
                                .map(|it| it.asset_id.clone())
                                .collect()
                        })
                        .unwrap_or_default();
                    if let Some(first) = accepted.first().cloned() {
                        let entry = state.alignments.entry(id).or_default();
                        if entry.reference.is_none() {
                            entry.reference = Some(first.clone());
                        }
                        let ref_id = entry.reference.clone();
                        if state.alignment_ui.secondary.is_none() {
                            state.alignment_ui.secondary = accepted
                                .into_iter()
                                .find(|aid| Some(aid) != ref_id.as_ref());
                        }
                    }
                }

                ui.separator();

                // Reference picker.
                show_frame_picker(
                    ui,
                    state,
                    "Ref:",
                    "alignment_ref_pick",
                    state.alignment_ui.collection_id,
                    state
                        .alignment_ui
                        .collection_id
                        .and_then(|id| state.alignments.get(&id))
                        .and_then(|a| a.reference.clone()),
                    |state, asset_id| {
                        if let Some(cid) = state.alignment_ui.collection_id {
                            state
                                .alignments
                                .entry(cid)
                                .or_default()
                                .reference = Some(asset_id);
                        }
                    },
                );

                ui.separator();

                // Secondary picker.
                show_frame_picker(
                    ui,
                    state,
                    "Tweak:",
                    "alignment_secondary_pick",
                    state.alignment_ui.collection_id,
                    state.alignment_ui.secondary.clone(),
                    |state, asset_id| {
                        state.alignment_ui.secondary = Some(asset_id);
                    },
                );

                ui.separator();

                // Auto-align job trigger + status indicator.
                let busy = state.align_running;
                let cid = state.alignment_ui.collection_id;
                let label = if busy { "Aligning…" } else { "Auto-align" };
                let resp = ui.add_enabled(!busy && cid.is_some(), egui::Button::new(label));
                if resp.clicked() {
                    if let Some(id) = cid {
                        crate::auto_align::spawn_auto_align(state, id);
                    }
                }
                if let Some(status) = &state.align_status {
                    ui.label(
                        egui::RichText::new(status)
                            .size(10.0)
                            .color(state.theme.text_faint),
                    );
                }
            });
        });
}

fn show_frame_picker(
    ui: &mut Ui,
    state: &mut AppState,
    label: &str,
    salt: &str,
    collection_id: Option<u64>,
    selected: Option<String>,
    on_pick: impl FnOnce(&mut AppState, String),
) {
    ui.label(
        egui::RichText::new(label)
            .size(10.0)
            .color(state.theme.text_dim),
    );

    let current = match (&selected, collection_id) {
        (Some(id), Some(cid)) => state
            .collection(cid)
            .and_then(|c| c.items.iter().find(|it| &it.asset_id == id))
            .map(|it| trim(&it.filename, 20))
            .unwrap_or_else(|| "(missing)".to_string()),
        _ => "(pick)".to_string(),
    };

    let mut new_pick: Option<String> = None;
    egui::ComboBox::from_id_salt(salt)
        .selected_text(
            egui::RichText::new(current)
                .size(10.0)
                .color(state.theme.text),
        )
        .width(160.0)
        .show_ui(ui, |ui| match collection_id.and_then(|id| state.collection(id)) {
            Some(c) => {
                for item in &c.items {
                    let is_current = selected.as_deref() == Some(item.asset_id.as_str());
                    if ui
                        .selectable_label(is_current, trim(&item.filename, 28))
                        .clicked()
                    {
                        new_pick = Some(item.asset_id.clone());
                    }
                }
            }
            None => {
                ui.label(
                    egui::RichText::new("(pick a group first)")
                        .size(10.0)
                        .color(state.theme.text_faint),
                );
            }
        });
    if let Some(id) = new_pick {
        on_pick(state, id);
    }
}

// ---------------------------------------------------------------------------
// Viewport — blend reference and secondary on top of each other
// ---------------------------------------------------------------------------

fn show_viewport(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.canvas);

    let collection_id = state.alignment_ui.collection_id;
    let reference_id = collection_id
        .and_then(|id| state.alignments.get(&id))
        .and_then(|a| a.reference.clone());
    let secondary_id = state.alignment_ui.secondary.clone();

    let ref_pixels = reference_id
        .as_ref()
        .and_then(|id| state.stacking_preview.frame_pixels.get(id));
    let sec_pixels = secondary_id
        .as_ref()
        .and_then(|id| state.stacking_preview.frame_pixels.get(id));

    let (Some(ref_pixels), Some(sec_pixels)) = (ref_pixels, sec_pixels) else {
        painter.text(
            rect.center(),
            egui::Align2::CENTER_CENTER,
            "Pick a group, reference, and secondary frame to align",
            FontId::proportional(11.0),
            state.theme.text_faint,
        );
        return;
    };

    if ref_pixels.width != sec_pixels.width || ref_pixels.height != sec_pixels.height {
        painter.text(
            rect.center(),
            egui::Align2::CENTER_CENTER,
            "Reference / secondary differ in size — alignment is undefined",
            FontId::proportional(11.0),
            state.theme.error,
        );
        return;
    }

    // Compose a blended preview into a fresh ColorImage. Cheap on
    // 1440px buffers; we only refresh when alignment changes.
    let alignment = collection_id
        .and_then(|cid| state.alignments.get(&cid))
        .and_then(|a| {
            secondary_id
                .as_ref()
                .and_then(|id| a.offsets.get(id).copied())
        })
        .unwrap_or_default();
    let blend = state.alignment_ui.blend.clamp(0.0, 1.0);

    // Cache key: ref id + sec id + alignment.dx,dy + blend rounded.
    let key = format!(
        "__align_view_{:?}_{:?}_{:.2}_{:.2}_{:.3}",
        reference_id, secondary_id, alignment.dx, alignment.dy, blend,
    );
    let cached = state.image_cache.contains(&key);
    if !cached {
        let img = compose_blend(ref_pixels, sec_pixels, alignment, blend);
        let color_image = egui::ColorImage::from_rgba_unmultiplied(
            [img.width as usize, img.height as usize],
            &img.rgba,
        );
        let texture =
            ui.ctx()
                .load_texture(&key, color_image, egui::TextureOptions::LINEAR);
        state.image_cache.insert(key.clone(), texture);
    }

    if let Some(tex) = state.image_cache.get(&key) {
        let ts = tex.size_vec2();
        let scale = (rect.width() / ts.x).min(rect.height() / ts.y) * 0.95;
        let display = ts * scale;
        let centered = Rect::from_center_size(rect.center(), display);
        painter.image(
            tex.id(),
            centered,
            Rect::from_min_max(Pos2::ZERO, Pos2::new(1.0, 1.0)),
            Color32::WHITE,
        );

        // Crosshair through the centre — helps eyeballing translation.
        let cx = centered.center().x;
        let cy = centered.center().y;
        let cross = Color32::from_rgba_unmultiplied(255, 255, 255, 32);
        painter.line_segment(
            [Pos2::new(centered.min.x, cy), Pos2::new(centered.max.x, cy)],
            Stroke::new(0.5, cross),
        );
        painter.line_segment(
            [Pos2::new(cx, centered.min.y), Pos2::new(cx, centered.max.y)],
            Stroke::new(0.5, cross),
        );
    }
}

/// Compose a blended frame: `(1 - blend) * reference + blend * shifted_secondary`.
fn compose_blend(
    reference: &FramePixels,
    secondary: &FramePixels,
    alignment: Alignment,
    blend: f32,
) -> FramePixels {
    let w = reference.width as i32;
    let h = reference.height as i32;
    let dx = alignment.dx.round() as i32;
    let dy = alignment.dy.round() as i32;
    let mut out = vec![0u8; (w * h * 4) as usize];
    let inv_blend = 1.0 - blend;
    for y in 0..h {
        for x in 0..w {
            let i = ((y * w + x) * 4) as usize;
            let r0 = reference.rgba[i] as f32;
            let g0 = reference.rgba[i + 1] as f32;
            let b0 = reference.rgba[i + 2] as f32;

            let sx = x - dx;
            let sy = y - dy;
            let (r1, g1, b1) = if (0..w).contains(&sx) && (0..h).contains(&sy) {
                let j = ((sy * w + sx) * 4) as usize;
                (
                    secondary.rgba[j] as f32,
                    secondary.rgba[j + 1] as f32,
                    secondary.rgba[j + 2] as f32,
                )
            } else {
                (0.0, 0.0, 0.0)
            };

            out[i] = (r0 * inv_blend + r1 * blend).clamp(0.0, 255.0) as u8;
            out[i + 1] = (g0 * inv_blend + g1 * blend).clamp(0.0, 255.0) as u8;
            out[i + 2] = (b0 * inv_blend + b1 * blend).clamp(0.0, 255.0) as u8;
            out[i + 3] = 255;
        }
    }
    FramePixels { width: reference.width, height: reference.height, rgba: out }
}

// ---------------------------------------------------------------------------
// Slider bar — translate-x, translate-y, blend
// ---------------------------------------------------------------------------

fn show_slider_bar(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.chrome);
    painter.line_segment(
        [Pos2::new(rect.min.x, rect.min.y), Pos2::new(rect.max.x, rect.min.y)],
        Stroke::new(1.0, state.theme.border),
    );

    let collection_id = state.alignment_ui.collection_id;
    let secondary_id = state.alignment_ui.secondary.clone();

    // Pull current Alignment, mutate via the slider, write back.
    let mut alignment = collection_id
        .and_then(|cid| state.alignments.get(&cid))
        .and_then(|a| {
            secondary_id
                .as_ref()
                .and_then(|id| a.offsets.get(id).copied())
        })
        .unwrap_or_default();
    let mut blend = state.alignment_ui.blend.clamp(0.0, 1.0);
    let mut quality = state.alignment_ui.quality_threshold.clamp(0.0, 1.0);
    let prev_quality = quality;

    let area_id = egui::Id::new("alignment_viewer_sliders");
    egui::Area::new(area_id)
        .fixed_pos(Pos2::new(rect.min.x + 6.0, rect.min.y + 6.0))
        .order(egui::Order::Foreground)
        .show(ui.ctx(), |ui| {
            ui.set_max_size(Vec2::new(rect.width() - 12.0, rect.height() - 12.0));
            ui.spacing_mut().slider_width = (ui.available_width() / 3.0 - 80.0).max(120.0);
            ui.horizontal(|ui| {
                ui.label(
                    egui::RichText::new("Δx")
                        .size(10.0)
                        .color(state.theme.text_dim),
                );
                ui.add(egui::Slider::new(&mut alignment.dx, -200.0..=200.0).show_value(true));

                ui.add_space(8.0);
                ui.label(
                    egui::RichText::new("Δy")
                        .size(10.0)
                        .color(state.theme.text_dim),
                );
                ui.add(egui::Slider::new(&mut alignment.dy, -200.0..=200.0).show_value(true));

                ui.add_space(8.0);
                ui.label(
                    egui::RichText::new("Blend")
                        .size(10.0)
                        .color(state.theme.text_dim),
                );
                ui.add(egui::Slider::new(&mut blend, 0.0..=1.0).show_value(true));

                ui.add_space(8.0);
                if ui.small_button("Reset").clicked() {
                    alignment = Alignment::default();
                }
            });

            // Quality cutoff row — auto-rejects frames whose focus +
            // star count score falls below `median × threshold`.
            ui.horizontal(|ui| {
                ui.label(
                    egui::RichText::new("Quality cutoff (top X% of median)")
                        .size(10.0)
                        .color(state.theme.text_dim),
                );
                ui.add(egui::Slider::new(&mut quality, 0.0..=1.0).show_value(true));
                let stats = collection_id.map(|cid| count_stats(state, cid));
                if let Some((accepted, total)) = stats {
                    ui.label(
                        egui::RichText::new(format!("{accepted}/{total} accepted"))
                            .size(10.0)
                            .color(state.theme.text_faint),
                    );
                }
            });

            // Reference-star count row — drives both the auto-align
            // worker (how many star trails to build) and the image
            // viewer overlay.
            ui.horizontal(|ui| {
                ui.label(
                    egui::RichText::new("Reference stars (overlay + tracking)")
                        .size(10.0)
                        .color(state.theme.text_dim),
                );
                ui.add(
                    egui::Slider::new(
                        &mut state.alignment_ui.tracked_star_count,
                        4..=32,
                    )
                    .show_value(true),
                );
                ui.checkbox(
                    &mut state.alignment_ui.overlay_tracked_stars,
                    egui::RichText::new("show overlay").size(10.0),
                );
            });
        });

    // Write back. Only do so when actually meaningful (need a target).
    if let (Some(cid), Some(asset_id)) = (collection_id, secondary_id) {
        let entry: &mut CollectionAlignment = state.alignments.entry(cid).or_default();
        let prev = entry.offsets.get(&asset_id).copied().unwrap_or_default();
        if (prev.dx - alignment.dx).abs() > 1e-3 || (prev.dy - alignment.dy).abs() > 1e-3 {
            entry.offsets.insert(asset_id, alignment);
            state.stacking_preview.dirty = true;
        }
    }
    state.alignment_ui.blend = blend;
    state.alignment_ui.quality_threshold = quality;

    // Re-apply the threshold whenever the slider moved.
    if (quality - prev_quality).abs() > 1e-3 {
        if let Some(cid) = collection_id {
            crate::quality::apply_threshold(state, cid, quality);
        }
    }
}

/// Count accepted vs total items in `collection_id`.
fn count_stats(state: &AppState, collection_id: u64) -> (usize, usize) {
    state
        .collection(collection_id)
        .map(|c| {
            let total = c.items.len();
            let accepted = c.items.iter().filter(|it| it.accepted).count();
            (accepted, total)
        })
        .unwrap_or((0, 0))
}


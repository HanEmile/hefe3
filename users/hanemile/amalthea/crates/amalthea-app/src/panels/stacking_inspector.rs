/// Stacking Inspector — list of frames in a chosen collection with
/// per-frame alignment metadata (Δx, Δy, anchor point) and a "show
/// ref point" toggle. The Image Viewer panel reads
/// `state.alignment_overlay_show` and draws a crosshair at the
/// anchor of any frame whose toggle is on AND that's currently
/// displayed in the viewer — turn the toggle on for several frames
/// to see how anchors line up across the stack.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use egui::{Color32, Pos2, Rect, ScrollArea, Sense, Stroke, StrokeKind, Ui, Vec2};

use super::util::trim;
use crate::state::AppState;

pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.panel);

    let area_id = egui::Id::new("stacking_inspector_area");
    egui::Area::new(area_id)
        .fixed_pos(rect.min)
        .show(ui.ctx(), |ui| {
            ui.set_clip_rect(rect);
            ui.set_min_size(rect.size());
            ui.set_max_size(rect.size());

            ui.add_space(6.0);
            show_header(ui, state);
            ui.separator();

            // Default to whichever collection the user touched last
            // in the gallery sidebar. The user can override with the
            // dropdown.
            if state.inspector_collection_id.is_none() {
                state.inspector_collection_id = state.last_collection_id;
            }

            ScrollArea::vertical()
                .id_salt("stacking_inspector_scroll")
                .max_height(rect.height() - 60.0)
                .show(ui, |ui| {
                    show_frames(ui, state);
                });
        });
}

fn show_header(ui: &mut Ui, state: &mut AppState) {
    ui.horizontal(|ui| {
        ui.add_space(8.0);
        ui.label(
            egui::RichText::new("Inspector")
                .size(12.0)
                .strong()
                .color(state.theme.text),
        );
    });

    ui.horizontal(|ui| {
        ui.add_space(8.0);
        ui.label(
            egui::RichText::new("Group:")
                .size(10.0)
                .color(state.theme.text_dim),
        );
        let current_label = state
            .inspector_collection_id
            .and_then(|id| state.collection(id))
            .map(|c| format!("{} — {}", c.kind.label(), c.name))
            .unwrap_or_else(|| "(pick a group)".to_string());

        let mut picked: Option<u64> = None;
        egui::ComboBox::from_id_salt("inspector_group_pick")
            .selected_text(
                egui::RichText::new(current_label)
                    .size(10.0)
                    .color(state.theme.text),
            )
            .width(180.0)
            .show_ui(ui, |ui| {
                for c in &state.collections {
                    let label = format!("{} — {} ({})", c.kind.label(), c.name, c.items.len());
                    if ui
                        .selectable_label(state.inspector_collection_id == Some(c.id), label)
                        .clicked()
                    {
                        picked = Some(c.id);
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
        if let Some(id) = picked {
            state.inspector_collection_id = Some(id);
        }
    });

    // Show all / hide all helpers.
    ui.horizontal(|ui| {
        ui.add_space(8.0);
        if ui.small_button("Show all anchors").clicked() {
            if let Some(id) = state.inspector_collection_id {
                if let Some(c) = state.collection(id) {
                    let to_add: Vec<String> =
                        c.items.iter().map(|it| it.asset_id.clone()).collect();
                    for aid in to_add {
                        state.alignment_overlay_show.insert(aid);
                    }
                }
            }
        }
        if ui.small_button("Hide all").clicked() {
            state.alignment_overlay_show.clear();
        }
        ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
            ui.add_space(8.0);
            ui.label(
                egui::RichText::new(format!("{} shown", state.alignment_overlay_show.len()))
                    .size(9.0)
                    .color(state.theme.text_faint),
            );
        });
    });
}

fn show_frames(ui: &mut Ui, state: &mut AppState) {
    let Some(cid) = state.inspector_collection_id else {
        ui.add_space(8.0);
        ui.label(
            egui::RichText::new("No group selected.")
                .size(10.0)
                .color(state.theme.text_faint),
        );
        return;
    };
    let collection = match state.collection(cid) {
        Some(c) => c.clone(),
        None => {
            state.inspector_collection_id = None;
            return;
        }
    };
    if collection.items.is_empty() {
        ui.add_space(8.0);
        ui.label(
            egui::RichText::new("Group is empty.")
                .size(10.0)
                .color(state.theme.text_faint),
        );
        return;
    }

    let alignments = state.alignments.get(&cid).cloned().unwrap_or_default();
    let reference = alignments.reference.clone();

    for item in &collection.items {
        show_frame_row(ui, state, item, &reference, &alignments);
    }
}

fn show_frame_row(
    ui: &mut Ui,
    state: &mut AppState,
    item: &crate::state::CollectionItem,
    reference: &Option<String>,
    alignments: &crate::state::CollectionAlignment,
) {
    let asset_id = item.asset_id.clone();
    let is_reference = reference.as_deref() == Some(asset_id.as_str());
    let alignment = alignments.offsets.get(&asset_id).copied();
    let mut showing = state.alignment_overlay_show.contains(&asset_id);

    ui.horizontal(|ui| {
        ui.add_space(6.0);

        // Mini thumbnail.
        let thumb = 28.0;
        let (thumb_rect, _) = ui.allocate_exact_size(Vec2::splat(thumb), Sense::hover());
        if let Some(tex) = state.thumbnail_cache.peek(&asset_id) {
            let ts = tex.size_vec2();
            let scale = (thumb / ts.x).min(thumb / ts.y);
            let display = ts * scale;
            let centered = Rect::from_center_size(thumb_rect.center(), display);
            ui.painter().image(
                tex.id(),
                centered,
                Rect::from_min_max(Pos2::ZERO, Pos2::new(1.0, 1.0)),
                Color32::WHITE,
            );
        } else {
            ui.painter().rect_filled(thumb_rect, 1.0, state.theme.canvas);
        }
        if !item.accepted {
            ui.painter().rect_filled(
                thumb_rect,
                1.0,
                Color32::from_rgba_unmultiplied(20, 8, 8, 160),
            );
            ui.painter().line_segment(
                [thumb_rect.left_top(), thumb_rect.right_bottom()],
                Stroke::new(1.0, state.theme.error),
            );
        }
        ui.painter().rect_stroke(
            thumb_rect,
            1.0,
            Stroke::new(0.5, state.theme.border),
            StrokeKind::Inside,
        );

        ui.vertical(|ui| {
            ui.spacing_mut().item_spacing.y = 1.0;
            // First line: filename (+ "(ref)" badge for the reference frame).
            let label = if is_reference {
                egui::RichText::new(format!("{} · ref", trim(&item.filename, 22)))
                    .size(10.0)
                    .color(state.theme.accent)
            } else {
                egui::RichText::new(trim(&item.filename, 28))
                    .size(10.0)
                    .color(state.theme.text)
            };
            ui.label(label);

            // Second line: alignment + anchor.
            let line = match alignment {
                Some(a) => {
                    let anchor_str = match a.anchor {
                        Some((x, y)) => format!("anchor ({x},{y})"),
                        None => "no anchor".to_string(),
                    };
                    format!("Δ ({:+.0},{:+.0})  ·  {}", a.dx, a.dy, anchor_str)
                }
                None => "(no alignment yet)".to_string(),
            };
            ui.label(
                egui::RichText::new(line)
                    .size(9.0)
                    .monospace()
                    .color(state.theme.text_faint),
            );
        });

        ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
            ui.add_space(6.0);
            if ui
                .checkbox(&mut showing, egui::RichText::new("anchor").size(10.0))
                .changed()
            {
                if showing {
                    state.alignment_overlay_show.insert(asset_id.clone());
                } else {
                    state.alignment_overlay_show.remove(&asset_id);
                }
            }
        });
    });
}


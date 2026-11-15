/// Log viewer panel with centralized logging from all components.
///
/// Filter buttons sit in a row at the top — each is filled in its own
/// level colour when active, dim and outlined when not, so the contrast
/// is high in both light and dark themes.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use egui::{Color32, FontId, Pos2, Rect, ScrollArea, Sense, Stroke, StrokeKind, Ui, Vec2};

use crate::state::AppState;
use crate::theme;

/// Render the log viewer panel.
pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.panel);

    let area_id = egui::Id::new("log_viewer_area");
    egui::Area::new(area_id)
        .fixed_pos(rect.min)
        .show(ui.ctx(), |ui| {
            ui.set_clip_rect(rect);
            ui.set_min_size(rect.size());
            ui.set_max_size(rect.size());

            ui.vertical(|ui| {
                show_filter_bar(ui, rect, state);

                ui.add_space(2.0);
                ui.separator();

                ScrollArea::vertical()
                    .id_salt("log_scroll")
                    .max_height(rect.height() - 32.0)
                    .stick_to_bottom(true)
                    .show(ui, |ui| {
                        let entries: Vec<_> = state
                            .log_entries
                            .iter()
                            .filter(|e| state.log_level_filter.contains(&e.level))
                            .collect();

                        if entries.is_empty() {
                            ui.label(
                                egui::RichText::new("No log entries")
                                    .size(11.0)
                                    .color(state.theme.text_faint),
                            );
                        }

                        for entry in entries {
                            ui.horizontal(|ui| {
                                ui.label(
                                    egui::RichText::new(&entry.timestamp)
                                        .size(10.0)
                                        .color(state.theme.text_faint)
                                        .monospace(),
                                );
                                ui.label(
                                    egui::RichText::new(&entry.level)
                                        .size(10.0)
                                        .color(theme::level_color(&state.theme, &entry.level))
                                        .monospace(),
                                );
                                ui.label(
                                    egui::RichText::new(&entry.source)
                                        .size(10.0)
                                        .color(state.theme.text_dim)
                                        .monospace(),
                                );
                                ui.label(
                                    egui::RichText::new(&entry.message)
                                        .size(11.0)
                                        .color(state.theme.text),
                                );
                            });
                        }
                    });
            });
        });
}

/// Header row with level-filter toggles and a Clear button.
fn show_filter_bar(ui: &mut Ui, _rect: Rect, state: &mut AppState) {
    ui.horizontal(|ui| {
        ui.add_space(4.0);
        ui.label(
            egui::RichText::new("Levels:")
                .size(10.0)
                .color(state.theme.text_dim),
        );

        for level in &["ERROR", "WARN", "INFO", "DEBUG"] {
            let active = state.log_level_filter.iter().any(|l| l == level);
            if filter_chip(ui, level, theme::level_color(&state.theme, level), active, &state.theme).clicked() {
                if active {
                    state.log_level_filter.retain(|l| l != *level);
                } else {
                    state.log_level_filter.push(level.to_string());
                }
            }
            ui.add_space(2.0);
        }

        ui.separator();
        if ui.small_button("Clear").clicked() {
            state.log_entries.clear();
        }
    });
}

/// One filter chip — solid level colour when active, hollow outline when
/// not. We draw it manually so the contrast is the same on light and
/// dark themes (default `selectable_label` colours are too subtle).
fn filter_chip(
    ui: &mut Ui,
    text: &str,
    color: Color32,
    active: bool,
    t: &crate::theme::Theme,
) -> egui::Response {
    let font = FontId::monospace(10.0);
    let galley = ui.painter().layout_no_wrap(text.to_string(), font.clone(), color);
    let pad = Vec2::new(6.0, 2.0);
    let size = galley.size() + pad * 2.0;
    let (rect, resp) = ui.allocate_exact_size(size, Sense::click());

    let (fill, fg, stroke) = if active {
        (color, contrasting_text(color), color)
    } else if resp.hovered() {
        (t.raised, color, color)
    } else {
        (t.panel, color, color.linear_multiply(0.6))
    };

    ui.painter().rect(
        rect,
        2.0,
        fill,
        Stroke::new(1.0, stroke),
        StrokeKind::Inside,
    );
    ui.painter().text(
        rect.center(),
        egui::Align2::CENTER_CENTER,
        text,
        font,
        fg,
    );

    // Suppress unused-variable warning for Pos2 import (we don't use it
    // directly here but the painter helpers do).
    let _ = Pos2::ZERO;

    resp
}

/// Pick a foreground that has decent contrast against `bg` — black or
/// white based on relative luminance.
fn contrasting_text(bg: Color32) -> Color32 {
    let [r, g, b, _] = bg.to_array();
    let y = 0.299 * r as f32 + 0.587 * g as f32 + 0.114 * b as f32;
    if y > 140.0 {
        Color32::BLACK
    } else {
        Color32::WHITE
    }
}

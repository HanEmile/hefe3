/// Settings panel — the live control surface for the whole application.
///
/// Every editable field mutates state directly. A `state.theme_dirty`
/// flag is raised when colors change so the persist layer (in `lib.rs`)
/// can debounce-save the theme to the backend SQLite store.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use egui::{Color32, Rect, ScrollArea, Ui};

use crate::state::AppState;
use crate::theme::Theme;

pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.panel);

    let area_id = egui::Id::new("settings_panel_area");
    egui::Area::new(area_id)
        .fixed_pos(rect.min)
        .show(ui.ctx(), |ui| {
            ui.set_clip_rect(rect);
            ui.set_min_size(rect.size());
            ui.set_max_size(rect.size());

            ScrollArea::vertical()
                .id_salt("settings_scroll")
                .max_height(rect.height())
                .show(ui, |ui| {
                    ui.add_space(4.0);
                    show_section(ui, state);
                });
        });
}

fn show_section(ui: &mut Ui, state: &mut AppState) {
    section(ui, "Theme", state.theme.text);
    show_theme_editor(ui, state);

    ui.add_space(8.0);
    section(ui, "Backend", state.theme.text);
    text_field(ui, "URL", &mut state.config.backend_url);
    text_field(ui, "DB path (server-side)", &mut state.config.db_path);

    ui.add_space(8.0);
    section(ui, "Immich", state.theme.text);
    text_field(ui, "URL", &mut state.config.immich_url);

    ui.add_space(8.0);
    section(ui, "Authentication", state.theme.text);
    text_field(ui, "OIDC Issuer", &mut state.config.oidc_issuer);
    text_field(ui, "Client ID", &mut state.config.oidc_client_id);
    let auth_status = match &state.auth {
        crate::auth::AuthState::Authenticated { .. } => "Authenticated",
        crate::auth::AuthState::Unauthenticated => "Not authenticated",
        crate::auth::AuthState::AwaitingCallback { .. } => "Awaiting callback...",
        crate::auth::AuthState::Error(e) => e.as_str(),
    };
    readonly_row(ui, state, "Status", auth_status);

    ui.add_space(8.0);
    section(ui, "Cache", state.theme.text);
    readonly_row(
        ui,
        state,
        "Thumbnails",
        &format!("{} entries", state.thumbnail_cache.len()),
    );
    readonly_row(
        ui,
        state,
        "Images",
        &format!("{} entries", state.image_cache.len()),
    );

    ui.add_space(8.0);
    section(ui, "Persistence", state.theme.text);
    if state.theme_dirty {
        ui.label(
            egui::RichText::new("Theme has unsaved changes — auto-save on idle.")
                .size(10.0)
                .color(state.theme.warn),
        );
    } else {
        ui.label(
            egui::RichText::new("Theme persisted.")
                .size(10.0)
                .color(state.theme.text_dim),
        );
    }
}

// ---------------------------------------------------------------------------
// Theme editor
// ---------------------------------------------------------------------------

fn show_theme_editor(ui: &mut Ui, state: &mut AppState) {
    ui.horizontal(|ui| {
        if ui.small_button("Dark preset").clicked() {
            state.theme = Theme::dark();
            state.theme_dirty = true;
        }
        if ui.small_button("Light preset").clicked() {
            state.theme = Theme::light();
            state.theme_dirty = true;
        }
        ui.label(
            egui::RichText::new("(Edits apply immediately and auto-save.)")
                .size(10.0)
                .color(state.theme.text_faint),
        );
    });

    ui.add_space(4.0);

    egui::Grid::new("theme_color_grid")
        .num_columns(4)
        .spacing([8.0, 2.0])
        .show(ui, |ui| {
            color_cell(ui, "canvas", &mut state.theme.canvas, &mut state.theme_dirty);
            color_cell(ui, "panel", &mut state.theme.panel, &mut state.theme_dirty);
            color_cell(ui, "chrome", &mut state.theme.chrome, &mut state.theme_dirty);
            color_cell(ui, "raised", &mut state.theme.raised, &mut state.theme_dirty);
            ui.end_row();

            color_cell(ui, "border", &mut state.theme.border, &mut state.theme_dirty);
            color_cell(ui, "accent", &mut state.theme.accent, &mut state.theme_dirty);
            color_cell(ui, "accent dim", &mut state.theme.accent_dim, &mut state.theme_dirty);
            ui.end_row();

            color_cell(ui, "text", &mut state.theme.text, &mut state.theme_dirty);
            color_cell(ui, "text dim", &mut state.theme.text_dim, &mut state.theme_dirty);
            color_cell(ui, "text faint", &mut state.theme.text_faint, &mut state.theme_dirty);
            ui.end_row();

            color_cell(ui, "info", &mut state.theme.info, &mut state.theme_dirty);
            color_cell(ui, "debug", &mut state.theme.debug, &mut state.theme_dirty);
            color_cell(ui, "success", &mut state.theme.success, &mut state.theme_dirty);
            ui.end_row();

            color_cell(ui, "warn", &mut state.theme.warn, &mut state.theme_dirty);
            color_cell(ui, "error", &mut state.theme.error, &mut state.theme_dirty);
            ui.end_row();
        });

    ui.add_space(4.0);
    ui.horizontal(|ui| {
        ui.label(
            egui::RichText::new("Corner radius")
                .size(10.0)
                .color(state.theme.text_dim),
        );
        let mut r = state.theme.corner_radius as i32;
        if ui.add(egui::Slider::new(&mut r, 0..=8).show_value(true)).changed() {
            state.theme.corner_radius = r as u8;
            state.theme_dirty = true;
        }
    });
}

/// One labelled colour swatch + picker.
fn color_cell(ui: &mut Ui, label: &str, c: &mut Color32, dirty: &mut bool) {
    ui.horizontal(|ui| {
        let mut rgba = [c.r(), c.g(), c.b()];
        if ui.color_edit_button_srgb(&mut rgba).changed() {
            *c = Color32::from_rgb(rgba[0], rgba[1], rgba[2]);
            *dirty = true;
        }
        ui.label(egui::RichText::new(label).size(10.0));
    });
}

// ---------------------------------------------------------------------------
// Field helpers
// ---------------------------------------------------------------------------

fn section(ui: &mut Ui, title: &str, color: Color32) {
    ui.label(
        egui::RichText::new(title)
            .size(12.0)
            .strong()
            .color(color),
    );
}

fn text_field(ui: &mut Ui, label: &str, value: &mut String) {
    ui.horizontal(|ui| {
        ui.add_sized(
            [120.0, 16.0],
            egui::Label::new(egui::RichText::new(label).size(10.0)),
        );
        ui.add(
            egui::TextEdit::singleline(value)
                .desired_width(280.0)
                .font(egui::FontId::monospace(10.0)),
        );
    });
}

fn readonly_row(ui: &mut Ui, state: &AppState, label: &str, value: &str) {
    ui.horizontal(|ui| {
        ui.add_sized(
            [120.0, 16.0],
            egui::Label::new(
                egui::RichText::new(label)
                    .size(10.0)
                    .color(state.theme.text_dim),
            ),
        );
        ui.label(
            egui::RichText::new(value)
                .size(10.0)
                .monospace()
                .color(state.theme.text),
        );
    });
}

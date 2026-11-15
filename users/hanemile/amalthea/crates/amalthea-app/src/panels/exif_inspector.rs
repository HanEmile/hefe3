/// EXIF metadata inspector panel.
///
/// Displays EXIF metadata for the currently selected image in a two-column
/// layout (label | value). Scrollable when metadata overflows the panel area.
///
/// Fields shown: filename, date, dimensions, camera make/model, lens, focal
/// length, aperture (f/N), exposure time, ISO, GPS coordinates.
///
/// Last reviewed: 2026-04-30 | Review frequency: monthly

use egui::{
    Align2, FontId, Pos2, Rect, ScrollArea, Stroke, Ui, Vec2,
};

use crate::state::AppState;

/// Left column width fraction (label column).
const LABEL_WIDTH_FRACTION: f32 = 0.4;

/// Row height for each metadata entry.
const ROW_HEIGHT: f32 = 22.0;

/// Horizontal padding inside cells.
const CELL_PADDING: f32 = 6.0;

/// Render the EXIF inspector panel.
///
/// Shows a scrollable two-column table of EXIF metadata for the selected
/// asset, or a placeholder message when no image is selected.
pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);

    // Background fill.
    painter.rect_filled(rect, 0.0, state.theme.canvas);

    let Some(asset) = &state.selected_asset else {
        painter.text(
            rect.center(),
            Align2::CENTER_CENTER,
            "No image selected",
            FontId::proportional(14.0),
            state.theme.text_faint,
        );
        return;
    };

    // Collect metadata rows as (label, value) pairs.
    let mut rows: Vec<(&str, String)> = Vec::new();

    // Filename.
    rows.push(("Filename", asset.original_file_name.clone()));

    // Date.
    rows.push(("Date", asset.file_created_at.clone()));

    // Dimensions.
    rows.push((
        "Dimensions",
        format!("{} x {}", asset.width, asset.height),
    ));

    // EXIF data from the embedded exif_info, if present.
    if let Some(exif) = &asset.exif_info {
        if let Some(make) = &exif.make {
            rows.push(("Camera Make", make.clone()));
        }
        if let Some(model) = &exif.model {
            rows.push(("Camera Model", model.clone()));
        }
        if let Some(lens) = &exif.lens_model {
            rows.push(("Lens", lens.clone()));
        }
        if let Some(focal_length) = exif.focal_length {
            rows.push(("Focal Length", format!("{:.1} mm", focal_length)));
        }
        if let Some(f_number) = exif.f_number {
            rows.push(("Aperture", format!("f/{:.1}", f_number)));
        }
        if let Some(exposure_time) = &exif.exposure_time {
            rows.push(("Exposure", exposure_time.clone()));
        }
        if let Some(iso) = exif.iso {
            rows.push(("ISO", iso.to_string()));
        }
        if let (Some(lat), Some(lon)) = (exif.latitude, exif.longitude) {
            rows.push(("GPS", format!("{:.6}, {:.6}", lat, lon)));
        }
    }

    // Render the rows in a scrollable area within the panel rect.
    //
    // We use an egui Area + ScrollArea to render within the given rect,
    // since panels receive a raw Rect rather than an allocated Ui region.
    let area_id = egui::Id::new("exif_inspector_scroll_area");

    egui::Area::new(area_id)
        .fixed_pos(rect.min)
        .show(ui.ctx(), |ui| {
            ui.set_clip_rect(rect);
            ui.set_min_size(rect.size());
            ui.set_max_size(rect.size());

            ScrollArea::vertical()
                .id_salt("exif_inspector_scroll")
                .max_height(rect.height())
                .max_width(rect.width())
                .show(ui, |ui| {
                    let available_width = rect.width();
                    let label_width = available_width * LABEL_WIDTH_FRACTION;
                    let value_width = available_width - label_width;

                    for (label, value) in &rows {
                        ui.horizontal(|ui| {
                            ui.set_height(ROW_HEIGHT);
                            ui.set_min_width(available_width);

                            // Label column — dimmer text.
                            let label_rect = ui.allocate_space(Vec2::new(
                                label_width - CELL_PADDING,
                                ROW_HEIGHT,
                            ));
                            ui.painter().text(
                                Pos2::new(
                                    label_rect.1.min.x + CELL_PADDING,
                                    label_rect.1.center().y,
                                ),
                                Align2::LEFT_CENTER,
                                *label,
                                FontId::proportional(12.0),
                                state.theme.text_dim,
                            );

                            // Value column — brighter text.
                            let value_rect = ui.allocate_space(Vec2::new(
                                value_width - CELL_PADDING,
                                ROW_HEIGHT,
                            ));
                            ui.painter().text(
                                Pos2::new(
                                    value_rect.1.min.x + CELL_PADDING,
                                    value_rect.1.center().y,
                                ),
                                Align2::LEFT_CENTER,
                                value.as_str(),
                                FontId::proportional(12.0),
                                state.theme.text,
                            );
                        });

                        // Subtle separator line.
                        let separator_rect = ui.allocate_space(Vec2::new(
                            available_width,
                            1.0,
                        ));
                        ui.painter().line_segment(
                            [
                                Pos2::new(
                                    separator_rect.1.min.x + CELL_PADDING,
                                    separator_rect.1.min.y,
                                ),
                                Pos2::new(
                                    separator_rect.1.max.x - CELL_PADDING,
                                    separator_rect.1.min.y,
                                ),
                            ],
                            Stroke::new(0.5, state.theme.border),
                        );
                    }
                });
        });
}

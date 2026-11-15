/// Observation trip planner panel.
///
/// Plan astrophotography sessions: choose location, date range, target object,
/// equipment settings, and get visibility predictions, exposure estimates.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use egui::{Rect, ScrollArea, Ui};
use crate::state::AppState;

/// Render the trip planner panel.
pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.canvas);

    let area_id = egui::Id::new("trip_planner_area");
    egui::Area::new(area_id)
        .fixed_pos(rect.min)
        .show(ui.ctx(), |ui| {
            ui.set_clip_rect(rect);
            ui.set_min_size(rect.size());
            ui.set_max_size(rect.size());

            ScrollArea::vertical()
                .id_salt("trip_planner_scroll")
                .max_height(rect.height())
                .show(ui, |ui| {
                    ui.add_space(8.0);

                    ui.horizontal(|ui| {
                        ui.add_space(8.0);
                        ui.label(
                            egui::RichText::new("Observation Trip Planner")
                                .size(14.0)
                                .strong()
                                .color(state.theme.text),
                        );
                    });

                    ui.separator();

                    // --- Location section ---
                    ui.horizontal(|ui| {
                        ui.add_space(8.0);
                        ui.label(egui::RichText::new("Location").size(12.0).color(state.theme.text_dim));
                    });
                    ui.horizontal(|ui| {
                        ui.add_space(16.0);
                        ui.label(egui::RichText::new("Name").size(10.0).color(state.theme.text_dim));
                        ui.text_edit_singleline(&mut state.trip_location_name);
                    });
                    ui.horizontal(|ui| {
                        ui.add_space(16.0);
                        ui.label(egui::RichText::new("Lat").size(10.0).color(state.theme.text_dim));
                        ui.add(egui::DragValue::new(&mut state.trip_lat).speed(0.01).range(-90.0..=90.0).suffix("°"));
                        ui.label(egui::RichText::new("Lon").size(10.0).color(state.theme.text_dim));
                        ui.add(egui::DragValue::new(&mut state.trip_lon).speed(0.01).range(-180.0..=180.0).suffix("°"));
                    });

                    ui.add_space(4.0);
                    ui.separator();

                    // --- Date/Time section ---
                    ui.horizontal(|ui| {
                        ui.add_space(8.0);
                        ui.label(egui::RichText::new("Date / Time (UTC)").size(12.0).color(state.theme.text_dim));
                    });
                    ui.horizontal(|ui| {
                        ui.add_space(16.0);
                        ui.add(egui::DragValue::new(&mut state.sky_year).speed(1.0).range(2020..=2040).prefix("Y "));
                        ui.add(egui::DragValue::new(&mut state.sky_month).speed(0.2).range(1..=12).prefix("M "));
                        ui.add(egui::DragValue::new(&mut state.sky_day).speed(0.3).range(1..=31).prefix("D "));
                        ui.add(egui::DragValue::new(&mut state.sky_hour).speed(0.1).range(0.0..=24.0).suffix("h").max_decimals(1));
                    });

                    ui.add_space(4.0);
                    ui.separator();

                    // --- Target section ---
                    ui.horizontal(|ui| {
                        ui.add_space(8.0);
                        ui.label(egui::RichText::new("Target").size(12.0).color(state.theme.text_dim));
                    });
                    ui.horizontal(|ui| {
                        ui.add_space(16.0);
                        ui.label(egui::RichText::new("Object").size(10.0).color(state.theme.text_dim));
                        ui.text_edit_singleline(&mut state.trip_target_name);
                    });
                    ui.horizontal(|ui| {
                        ui.add_space(16.0);
                        ui.label(egui::RichText::new("RA").size(10.0).color(state.theme.text_dim));
                        ui.add(egui::DragValue::new(&mut state.trip_target_ra).speed(0.1).range(0.0..=360.0).suffix("°"));
                        ui.label(egui::RichText::new("Dec").size(10.0).color(state.theme.text_dim));
                        ui.add(egui::DragValue::new(&mut state.trip_target_dec).speed(0.1).range(-90.0..=90.0).suffix("°"));
                    });

                    ui.add_space(4.0);
                    ui.separator();

                    // --- Equipment section ---
                    ui.horizontal(|ui| {
                        ui.add_space(8.0);
                        ui.label(egui::RichText::new("Equipment").size(12.0).color(state.theme.text_dim));
                    });
                    ui.horizontal(|ui| {
                        ui.add_space(16.0);
                        ui.label(egui::RichText::new("Focal length").size(10.0).color(state.theme.text_dim));
                        ui.add(egui::DragValue::new(&mut state.trip_focal_length).speed(1.0).range(10.0..=10000.0).suffix(" mm"));
                    });
                    ui.horizontal(|ui| {
                        ui.add_space(16.0);
                        ui.label(egui::RichText::new("Aperture").size(10.0).color(state.theme.text_dim));
                        ui.add(egui::DragValue::new(&mut state.trip_aperture).speed(0.1).range(1.0..=100.0).prefix("f/"));
                    });
                    ui.horizontal(|ui| {
                        ui.add_space(16.0);
                        ui.label(egui::RichText::new("Sensor pixel").size(10.0).color(state.theme.text_dim));
                        ui.add(egui::DragValue::new(&mut state.trip_pixel_size).speed(0.1).range(1.0..=20.0).suffix(" µm"));
                    });

                    ui.add_space(4.0);
                    ui.separator();

                    // --- Computed results ---
                    ui.horizontal(|ui| {
                        ui.add_space(8.0);
                        ui.label(egui::RichText::new("Calculations").size(12.0).color(state.theme.text_dim));
                    });

                    // Rule of 500: max exposure = 500 / focal_length
                    let max_exposure_500 = 500.0 / state.trip_focal_length;
                    // NPF rule: max exposure = (35 * aperture + 30 * pixel_size) / focal_length
                    let max_exposure_npf = (35.0 * state.trip_aperture + 30.0 * state.trip_pixel_size) / state.trip_focal_length;
                    // Image scale: arcsec/pixel = 206.265 * pixel_size_um / focal_length_mm
                    let image_scale = 206.265 * state.trip_pixel_size / state.trip_focal_length;

                    let result = |ui: &mut Ui, label: &str, value: &str| {
                        ui.horizontal(|ui| {
                            ui.add_space(16.0);
                            ui.label(egui::RichText::new(label).size(10.0).color(state.theme.text_dim));
                            ui.label(egui::RichText::new(value).size(10.0).monospace().color(state.theme.text));
                        });
                    };

                    result(ui, "Max exposure (500 rule)", &format!("{max_exposure_500:.1} s"));
                    result(ui, "Max exposure (NPF rule)", &format!("{max_exposure_npf:.1} s"));
                    result(ui, "Image scale", &format!("{image_scale:.2} \"/px"));

                    // Target altitude at the selected time.
                    if state.trip_target_ra != 0.0 || state.trip_target_dec != 0.0 {
                        let jd = crate::sky::jd_from_utc(
                            state.sky_year, state.sky_month, state.sky_day,
                            state.sky_hour as u32, ((state.sky_hour.fract()) * 60.0) as u32, 0.0,
                        );
                        let alt = crate::sky::object_altitude(
                            state.trip_target_ra as f64, state.trip_target_dec as f64,
                            state.trip_lat, state.trip_lon, jd,
                        );
                        let visibility = if alt >= 0.0 {
                            format!("VISIBLE at {alt:.1}° altitude")
                        } else {
                            format!("Below horizon ({alt:.1}°)")
                        };
                        result(ui, "Target visibility", &visibility);
                    }

                    result(ui, "FOV", &format!("{:.1}' x {:.1}' (approx)",
                        image_scale * 4000.0 / 60.0,
                        image_scale * 3000.0 / 60.0,  // assume ~3000px sensor height
                    ));

                    // Point the sky viewer to the target if we have coordinates.
                    if state.trip_target_ra != 0.0 || state.trip_target_dec != 0.0 {
                        ui.add_space(4.0);
                        ui.horizontal(|ui| {
                            ui.add_space(16.0);
                            if ui.button("Show in Sky Viewer").clicked() {
                                state.sky_center_ra = state.trip_target_ra;
                                state.sky_center_dec = state.trip_target_dec;
                                state.sky_zoom = 3.0;
                                state.add_log("INFO", "planner", &format!(
                                    "Centered sky viewer on {} (RA {:.1}° Dec {:.1}°)",
                                    state.trip_target_name, state.trip_target_ra, state.trip_target_dec,
                                ));
                            }
                        });
                    }

                    ui.add_space(8.0);
                });
        });
}

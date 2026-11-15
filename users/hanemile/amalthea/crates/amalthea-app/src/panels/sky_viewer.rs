/// Sky Viewer panel — interactive sky map with horizon and visibility.
///
/// Renders a stereographic projection of the night sky centered on the zenith
/// of the observer's location. Shows the horizon circle, altitude/azimuth grid,
/// bright stars color-coded by visibility, and highlights the target object.
///
/// Last reviewed: 2026-04-30 | Review frequency: monthly

use egui::{Color32, Pos2, Rect, Sense, Stroke, Ui, Vec2};
use crate::state::AppState;
use crate::sky;

/// Bright star catalog entry.
struct Star {
    ra: f64,
    dec: f64,
    mag: f32,
    name: &'static str,
}

/// Embedded catalog of ~40 bright stars.
fn bright_stars() -> Vec<Star> {
    vec![
        Star { ra: 101.29, dec: -16.72, mag: -1.46, name: "Sirius" },
        Star { ra: 213.92, dec: 19.18, mag: -0.05, name: "Arcturus" },
        Star { ra: 279.23, dec: 38.78, mag: 0.03, name: "Vega" },
        Star { ra: 78.63, dec: -8.20, mag: 0.12, name: "Rigel" },
        Star { ra: 114.83, dec: 5.22, mag: 0.34, name: "Procyon" },
        Star { ra: 88.79, dec: 7.41, mag: 0.50, name: "Betelgeuse" },
        Star { ra: 24.43, dec: -57.24, mag: 0.46, name: "Achernar" },
        Star { ra: 297.70, dec: 8.87, mag: 0.77, name: "Altair" },
        Star { ra: 152.09, dec: 11.97, mag: 1.35, name: "Regulus" },
        Star { ra: 310.36, dec: 45.28, mag: 1.25, name: "Deneb" },
        Star { ra: 68.98, dec: 16.51, mag: 0.85, name: "Aldebaran" },
        Star { ra: 344.41, dec: -29.62, mag: 1.16, name: "Fomalhaut" },
        Star { ra: 247.35, dec: -26.43, mag: 1.06, name: "Antares" },
        Star { ra: 201.30, dec: -11.16, mag: 0.97, name: "Spica" },
        Star { ra: 37.95, dec: 89.26, mag: 2.02, name: "Polaris" },
        Star { ra: 116.33, dec: 28.03, mag: 1.58, name: "Pollux" },
        Star { ra: 83.00, dec: -0.30, mag: 1.70, name: "Mintaka" },
        Star { ra: 83.86, dec: -1.20, mag: 1.69, name: "Alnilam" },
        Star { ra: 84.05, dec: -1.94, mag: 2.23, name: "Alnitak" },
        Star { ra: 191.93, dec: -59.69, mag: 0.77, name: "Mimosa" },
        Star { ra: 165.93, dec: 61.75, mag: 1.77, name: "Dubhe" },
        Star { ra: 200.98, dec: 54.93, mag: 1.85, name: "Alioth" },
        Star { ra: 263.40, dec: -37.10, mag: 1.62, name: "Shaula" },
        Star { ra: 186.65, dec: -63.10, mag: 1.33, name: "Acrux" },
    ]
}

/// Project Alt/Az to screen position in a zenith-centered polar plot.
/// Alt=90° maps to center, Alt=0° maps to the horizon circle edge.
/// Az=0 (North) points up.
fn altaz_to_screen(alt: f64, az: f64, center: Pos2, radius: f32) -> Pos2 {
    // Radial distance from center: zenith (alt=90) is center, horizon (alt=0) is edge.
    let r = (1.0 - alt / 90.0).max(0.0) as f32 * radius;
    // Azimuth: 0=North=up, 90=East=right. In screen coords, up is -Y.
    let angle = (az as f32 - 90.0).to_radians(); // rotate so North is up
    let x = center.x + r * angle.cos();
    let y = center.y - r * angle.sin();
    Pos2::new(x, y)
}

/// Render the sky viewer panel.
pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState, panel_id: u64) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.canvas);

    // Resolve zoom: shared or per-panel.
    let current_zoom = if state.sky_sync_zoom {
        state.sky_zoom
    } else {
        *state.sky_panel_zooms.get(&panel_id).unwrap_or(&state.sky_zoom)
    };

    // Interaction.
    let response = ui.allocate_rect(rect, Sense::click_and_drag());
    if response.hovered() {
        let scroll = ui.input(|i| i.smooth_scroll_delta.y);
        if scroll != 0.0 {
            let new_zoom = (current_zoom * (1.0 + scroll * 0.002)).clamp(0.5, 5.0);
            if state.sky_sync_zoom {
                state.sky_zoom = new_zoom;
            } else {
                state.sky_panel_zooms.insert(panel_id, new_zoom);
            }
        }
    }

    // Time slider: drag horizontally to change hour (always synced).
    if response.dragged() {
        let delta = response.drag_delta();
        state.sky_hour = (state.sky_hour + delta.x as f64 * 0.02).rem_euclid(24.0);
    }

    // Re-read zoom after possible mutation.
    let zoom = if state.sky_sync_zoom {
        state.sky_zoom
    } else {
        *state.sky_panel_zooms.get(&panel_id).unwrap_or(&state.sky_zoom)
    };

    // Compute Julian Date from trip planner date + sky_hour.
    let jd = sky::jd_from_utc(
        state.sky_year,
        state.sky_month,
        state.sky_day,
        state.sky_hour as u32,
        ((state.sky_hour.fract()) * 60.0) as u32,
        0.0,
    );
    let lst = sky::lst_degrees(jd, state.trip_lon);
    let lat = state.trip_lat;

    // Polar plot: zenith at center, horizon as circle.
    let center = rect.center();
    let radius = rect.width().min(rect.height()) * 0.45 * zoom;

    // Horizon circle.
    painter.circle_stroke(center, radius, Stroke::new(1.5, Color32::from_rgb(60, 80, 40)));

    // Fill below horizon (outside circle) with a subtle brown.
    // We just draw a filled circle for the sky area.
    painter.circle_filled(center, radius, Color32::from_rgba_unmultiplied(10, 15, 30, 200));

    // Cardinal directions on the horizon.
    let cardinals = [("N", 0.0), ("E", 90.0), ("S", 180.0), ("W", 270.0)];
    for (label, az) in cardinals {
        let pos = altaz_to_screen(0.0, az, center, radius);
        // Offset slightly outward.
        let offset_dir = (pos - center).normalized() * 14.0;
        painter.text(
            pos + offset_dir,
            egui::Align2::CENTER_CENTER,
            label,
            egui::FontId::proportional(11.0),
            Color32::from_rgb(100, 130, 80),
        );
    }

    // Altitude rings (30°, 60°).
    for alt in [30.0, 60.0] {
        let r = (1.0 - alt / 90.0) as f32 * radius;
        painter.circle_stroke(
            center,
            r,
            Stroke::new(0.5, Color32::from_rgba_unmultiplied(40, 50, 70, 80)),
        );
    }

    // Zenith marker.
    painter.circle_filled(center, 3.0, state.theme.text_faint);
    painter.text(
        center + Vec2::new(8.0, -8.0),
        egui::Align2::LEFT_BOTTOM,
        "Z",
        egui::FontId::proportional(9.0),
        state.theme.text_faint,
    );

    // Draw stars.
    let stars = bright_stars();
    for star in &stars {
        let altaz = sky::radec_to_altaz(star.ra, star.dec, lat, lst);
        if altaz.alt < -5.0 { continue; } // skip stars far below horizon

        let pos = altaz_to_screen(altaz.alt, altaz.az, center, radius);
        if !rect.contains(pos) { continue; }

        let brightness = ((3.0 - star.mag) / 4.5).clamp(0.0, 1.0);
        let star_radius = 1.0 + brightness * 3.0;

        // Color: green if above horizon, red if below, white/blue for bright ones above.
        let color = if altaz.alt >= 0.0 {
            let alpha = (120.0 + brightness * 135.0) as u8;
            Color32::from_rgba_unmultiplied(
                (180.0 + brightness * 75.0) as u8,
                (200.0 + brightness * 55.0) as u8,
                255,
                alpha,
            )
        } else {
            Color32::from_rgba_unmultiplied(150, 60, 60, 80)
        };

        painter.circle_filled(pos, star_radius, color);

        // Label bright stars.
        if star.mag < 1.0 && altaz.alt > 0.0 {
            painter.text(
                pos + Vec2::new(star_radius + 3.0, -2.0),
                egui::Align2::LEFT_CENTER,
                star.name,
                egui::FontId::proportional(8.0),
                state.theme.text_faint,
            );
        }
    }

    // Draw target object from trip planner.
    if state.trip_target_ra != 0.0 || state.trip_target_dec != 0.0 {
        let target_altaz = sky::radec_to_altaz(
            state.trip_target_ra as f64,
            state.trip_target_dec as f64,
            lat,
            lst,
        );
        let target_pos = altaz_to_screen(target_altaz.alt, target_altaz.az, center, radius);

        if rect.contains(target_pos) {
            // Crosshair.
            let ch = 8.0;
            let target_color = if target_altaz.alt >= 0.0 {
                Color32::from_rgb(80, 220, 80)
            } else {
                Color32::from_rgb(220, 80, 80)
            };
            painter.line_segment(
                [target_pos - Vec2::new(ch, 0.0), target_pos + Vec2::new(ch, 0.0)],
                Stroke::new(1.5, target_color),
            );
            painter.line_segment(
                [target_pos - Vec2::new(0.0, ch), target_pos + Vec2::new(0.0, ch)],
                Stroke::new(1.5, target_color),
            );
            painter.circle_stroke(target_pos, 6.0, Stroke::new(1.0, target_color));

            // Label.
            let label = if state.trip_target_name.is_empty() {
                format!("Target ({:.0}°)", target_altaz.alt)
            } else {
                format!("{} ({:.0}°)", state.trip_target_name, target_altaz.alt)
            };
            painter.text(
                target_pos + Vec2::new(10.0, -10.0),
                egui::Align2::LEFT_BOTTOM,
                &label,
                egui::FontId::proportional(10.0),
                target_color,
            );
        }

        // Target info in bottom-left.
        let visibility = if target_altaz.alt >= 0.0 {
            format!("VISIBLE — Alt {:.1}° Az {:.0}°", target_altaz.alt, target_altaz.az)
        } else {
            format!("BELOW HORIZON — Alt {:.1}°", target_altaz.alt)
        };
        let vis_color = if target_altaz.alt >= 0.0 {
            Color32::from_rgb(80, 200, 80)
        } else {
            Color32::from_rgb(200, 80, 80)
        };
        painter.text(
            Pos2::new(rect.min.x + 8.0, rect.max.y - 26.0),
            egui::Align2::LEFT_BOTTOM,
            &visibility,
            egui::FontId::monospace(10.0),
            vis_color,
        );
    }

    // Info overlay: time, LST, location.
    let info = format!(
        "{}h{:02}m UTC  LST {:.1}h  Lat {:.1}° Lon {:.1}°",
        state.sky_hour as u32,
        ((state.sky_hour.fract()) * 60.0) as u32,
        lst / 15.0,
        lat,
        state.trip_lon,
    );
    painter.text(
        Pos2::new(rect.min.x + 8.0, rect.max.y - 8.0),
        egui::Align2::LEFT_BOTTOM,
        &info,
        egui::FontId::monospace(10.0),
        state.theme.text_dim,
    );

    // Date display top-left.
    let date_str = format!(
        "{:04}-{:02}-{:02}",
        state.sky_year, state.sky_month, state.sky_day,
    );
    painter.text(
        Pos2::new(rect.min.x + 8.0, rect.min.y + 8.0),
        egui::Align2::LEFT_TOP,
        &date_str,
        egui::FontId::monospace(10.0),
        state.theme.text_faint,
    );

    // Sync toggle (top-right).
    let sync_label = if state.sky_sync_zoom { "Zoom: synced" } else { "Zoom: independent" };
    let sync_color = if state.sky_sync_zoom {
        Color32::from_rgb(80, 160, 80)
    } else {
        state.theme.text_faint
    };
    let sync_rect = egui::Rect::from_min_size(
        Pos2::new(rect.max.x - 110.0, rect.min.y + 4.0),
        Vec2::new(104.0, 14.0),
    );
    let sync_resp = ui.allocate_rect(sync_rect, Sense::click());
    painter.text(
        sync_rect.center(),
        egui::Align2::CENTER_CENTER,
        sync_label,
        egui::FontId::proportional(9.0),
        sync_color,
    );
    if sync_resp.clicked() {
        state.sky_sync_zoom = !state.sky_sync_zoom;
    }

    // Help text.
    painter.text(
        Pos2::new(rect.max.x - 8.0, rect.max.y - 8.0),
        egui::Align2::RIGHT_BOTTOM,
        "Drag L/R to change time | Scroll to zoom",
        egui::FontId::proportional(9.0),
        state.theme.border,
    );
}

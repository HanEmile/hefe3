/// Star tracker panel — visualises the star trails computed by the
/// auto-align job and overlays the predicted Earth-rotation path
/// from `align_verify` for cross-checking.
///
/// Layout:
///
///   ┌ top bar: collection picker + star picker + crop size       ┐
///   ├──────────────────┬─────────────────────────────────────────┤
///   │ STACK of just    │ TRAIL on a mid-frame: measured centroids│
///   │ this star (crops │ as dots, predicted Earth-rotation as a  │
///   │ aligned at sub-  │ dashed line. Toggle with `show predicted│
///   │ pixel)           │                                          │
///   ├──────────────────┴─────────────────────────────────────────┤
///   │ residual table: dt, Δx_meas, Δy_meas, Δx_pred, Δy_pred, |r|│
///   └─────────────────────────────────────────────────────────────┘
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use egui::{Color32, FontId, Pos2, Rect, ScrollArea, Stroke, StrokeKind, Ui, Vec2};

use super::util::trim;
use crate::align_verify::{frame_residuals, FrameResidual};
use crate::auto_align::SiteSnapshot;
use crate::state::{AppState, FramePixels, ImageOrientation, TrackedStar};

const TOP_BAR_H: f32 = 28.0;
const TABLE_H: f32 = 140.0;

/// Build a `SiteSnapshot` from the trip-planner fields on `AppState`.
/// The verifier consumes f64 throughout so we widen the f32 trip
/// fields here, in one place.
fn site_from_state(state: &AppState) -> SiteSnapshot {
    SiteSnapshot {
        lat: state.trip_lat,
        lon: state.trip_lon,
        target_ra: state.trip_target_ra as f64,
        target_dec: state.trip_target_dec as f64,
        focal_length_mm: state.trip_focal_length as f64,
        pixel_size_um: state.trip_pixel_size as f64,
    }
}

pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.panel);

    let top = Rect::from_min_size(rect.min, Vec2::new(rect.width(), TOP_BAR_H));
    let table = Rect::from_min_size(
        Pos2::new(rect.min.x, rect.max.y - TABLE_H),
        Vec2::new(rect.width(), TABLE_H),
    );
    let viewport = Rect::from_min_max(
        Pos2::new(rect.min.x, top.max.y),
        Pos2::new(rect.max.x, table.min.y),
    );

    show_top_bar(ui, top, state);
    show_viewport(ui, viewport, state);
    show_residual_table(ui, table, state);
}

fn show_top_bar(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.chrome);
    painter.line_segment(
        [Pos2::new(rect.min.x, rect.max.y), Pos2::new(rect.max.x, rect.max.y)],
        Stroke::new(1.0, state.theme.border),
    );

    egui::Area::new(egui::Id::new("star_tracker_top_bar"))
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
                let current = state
                    .star_tracker_ui
                    .collection_id
                    .and_then(|id| state.collection(id))
                    .map(|c| format!("{} — {}", c.kind.label(), c.name))
                    .unwrap_or_else(|| "(pick a group)".to_string());
                let mut new_collection: Option<u64> = None;
                egui::ComboBox::from_id_salt("star_tracker_collection")
                    .selected_text(
                        egui::RichText::new(current).size(10.0).color(state.theme.text),
                    )
                    .width(180.0)
                    .show_ui(ui, |ui| {
                        for c in &state.collections {
                            let label = format!("{} — {}", c.kind.label(), c.name);
                            if ui
                                .selectable_label(
                                    state.star_tracker_ui.collection_id == Some(c.id),
                                    label,
                                )
                                .clicked()
                            {
                                new_collection = Some(c.id);
                            }
                        }
                    });
                if let Some(id) = new_collection {
                    state.star_tracker_ui.collection_id = Some(id);
                    state.star_tracker_ui.selected_star = Some(0);
                }

                ui.separator();

                // Star picker.
                let trail_count = state
                    .star_tracker_ui
                    .collection_id
                    .and_then(|id| state.alignments.get(&id))
                    .map(|a| a.star_trails.len())
                    .unwrap_or(0);
                let current_star = state
                    .star_tracker_ui
                    .selected_star
                    .map(|i| format!("Star #{i}"))
                    .unwrap_or_else(|| "(none)".to_string());
                ui.label(
                    egui::RichText::new("Star:")
                        .size(10.0)
                        .color(state.theme.text_dim),
                );
                egui::ComboBox::from_id_salt("star_tracker_star")
                    .selected_text(
                        egui::RichText::new(current_star)
                            .size(10.0)
                            .color(state.theme.text),
                    )
                    .width(80.0)
                    .show_ui(ui, |ui| {
                        for i in 0..trail_count.min(32) {
                            let label = format!("#{i}");
                            if ui
                                .selectable_label(
                                    state.star_tracker_ui.selected_star == Some(i as u32),
                                    label,
                                )
                                .clicked()
                            {
                                state.star_tracker_ui.selected_star = Some(i as u32);
                            }
                        }
                        if trail_count == 0 {
                            ui.label(
                                egui::RichText::new("(run auto-align first)")
                                    .size(10.0)
                                    .color(state.theme.text_faint),
                            );
                        }
                    });

                ui.separator();
                ui.label(
                    egui::RichText::new("Crop:")
                        .size(10.0)
                        .color(state.theme.text_dim),
                );
                let mut crop = state.star_tracker_ui.crop_size as f32;
                ui.add(egui::Slider::new(&mut crop, 16.0..=256.0).show_value(true));
                state.star_tracker_ui.crop_size = crop.round() as u32;

                ui.separator();
                ui.checkbox(
                    &mut state.star_tracker_ui.show_predicted,
                    egui::RichText::new("show predicted path").size(10.0),
                );
            });
        });
}

fn show_viewport(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.canvas);

    let Some(cid) = state.star_tracker_ui.collection_id else {
        painter.text(
            rect.center(),
            egui::Align2::CENTER_CENTER,
            "Pick a group with a completed auto-align job",
            FontId::proportional(11.0),
            state.theme.text_faint,
        );
        return;
    };

    let alignment_data = state.alignments.get(&cid).cloned();
    let Some(alignment_data) = alignment_data else {
        painter.text(
            rect.center(),
            egui::Align2::CENTER_CENTER,
            "No alignments yet — run auto-align in the Alignment Viewer",
            FontId::proportional(11.0),
            state.theme.text_faint,
        );
        return;
    };

    let star_id = state.star_tracker_ui.selected_star.unwrap_or(0);
    let Some(trail) = alignment_data
        .star_trails
        .iter()
        .find(|t| t.id == star_id)
        .cloned()
    else {
        painter.text(
            rect.center(),
            egui::Align2::CENTER_CENTER,
            "No trail for this star yet",
            FontId::proportional(11.0),
            state.theme.text_faint,
        );
        return;
    };

    // Split the viewport: left half = stack of the star's crops,
    // right half = trail overlay on a mid-frame.
    let mid_x = rect.min.x + rect.width() * 0.5;
    let left = Rect::from_min_max(rect.min, Pos2::new(mid_x - 1.0, rect.max.y));
    let right = Rect::from_min_max(Pos2::new(mid_x + 1.0, rect.min.y), rect.max);

    show_crop_stack(ui, left, state, &trail);
    show_trail_overlay(ui, right, state, &trail, &alignment_data);
}

fn show_crop_stack(ui: &mut Ui, rect: Rect, state: &mut AppState, trail: &TrackedStar) {
    let painter = ui.painter_at(rect);
    painter.rect_stroke(
        rect.shrink(2.0),
        0.0,
        Stroke::new(1.0, state.theme.border),
        StrokeKind::Inside,
    );
    painter.text(
        Pos2::new(rect.min.x + 8.0, rect.min.y + 8.0),
        egui::Align2::LEFT_TOP,
        format!("Star #{} stacked crops", trail.id),
        FontId::proportional(10.0),
        state.theme.text_dim,
    );

    // Build a per-call crop stack: for each frame with a detection,
    // sample a `crop_size` window around the centroid and average.
    let crop = state.star_tracker_ui.crop_size as i32;
    let half = crop / 2;
    let mut acc = vec![0.0_f32; (crop * crop * 3) as usize];
    let mut n = 0_u32;

    for (asset_id, det) in &trail.frames {
        let Some((x, y)) = det else { continue };
        let Some(pixels) = state.stacking_preview.frame_pixels.get(asset_id) else {
            continue;
        };
        let cx = x.round() as i32;
        let cy = y.round() as i32;
        sample_crop(pixels, cx - half, cy - half, crop as u32, &mut acc);
        n += 1;
    }
    if n == 0 {
        painter.text(
            rect.center(),
            egui::Align2::CENTER_CENTER,
            "no detections for this star yet",
            FontId::proportional(10.0),
            state.theme.text_faint,
        );
        return;
    }
    let inv_n = 1.0 / n as f32;
    let mut rgba = vec![0u8; (crop * crop * 4) as usize];
    for i in 0..(crop * crop) as usize {
        for c in 0..3 {
            let v = acc[i * 3 + c] * inv_n;
            rgba[i * 4 + c] = v.clamp(0.0, 255.0) as u8;
        }
        rgba[i * 4 + 3] = 255;
    }

    let key = format!("__star_tracker_crop_{}_{}_{}", trail.id, n, crop);
    if !state.image_cache.contains(&key) {
        let color_image = egui::ColorImage::from_rgba_unmultiplied([crop as usize, crop as usize], &rgba);
        let texture = ui.ctx().load_texture(&key, color_image, egui::TextureOptions::LINEAR);
        state.image_cache.insert(key.clone(), texture);
    }
    if let Some(tex) = state.image_cache.get(&key) {
        let ts = tex.size_vec2();
        let scale = (rect.width() / ts.x).min((rect.height() - 24.0) / ts.y) * 0.9;
        let display = ts * scale;
        let centered = Rect::from_center_size(rect.center(), display);
        painter.image(
            tex.id(),
            centered,
            Rect::from_min_max(Pos2::ZERO, Pos2::new(1.0, 1.0)),
            Color32::WHITE,
        );
        // Tiny crosshair on the centroid (which is the crop centre by
        // construction).
        let cx = centered.center().x;
        let cy = centered.center().y;
        let cross = Color32::from_rgba_unmultiplied(255, 200, 80, 96);
        painter.line_segment(
            [Pos2::new(cx - 8.0, cy), Pos2::new(cx + 8.0, cy)],
            Stroke::new(0.5, cross),
        );
        painter.line_segment(
            [Pos2::new(cx, cy - 8.0), Pos2::new(cx, cy + 8.0)],
            Stroke::new(0.5, cross),
        );
        painter.text(
            Pos2::new(centered.center().x, centered.max.y + 10.0),
            egui::Align2::CENTER_TOP,
            format!("{n} frames stacked"),
            FontId::proportional(9.0),
            state.theme.text_faint,
        );
    }
}

fn sample_crop(src: &FramePixels, ox: i32, oy: i32, size: u32, acc: &mut [f32]) {
    let w = src.width as i32;
    let h = src.height as i32;
    let stride = (w * 4) as usize;
    for y in 0..size as i32 {
        for x in 0..size as i32 {
            let sx = ox + x;
            let sy = oy + y;
            let idx = ((y as usize) * size as usize + x as usize) * 3;
            if sx < 0 || sy < 0 || sx >= w || sy >= h {
                continue;
            }
            let p = (sy as usize) * stride + (sx as usize) * 4;
            acc[idx] += src.rgba[p] as f32;
            acc[idx + 1] += src.rgba[p + 1] as f32;
            acc[idx + 2] += src.rgba[p + 2] as f32;
        }
    }
}

fn show_trail_overlay(
    ui: &mut Ui,
    rect: Rect,
    state: &mut AppState,
    trail: &TrackedStar,
    alignment_data: &crate::state::CollectionAlignment,
) {
    let painter = ui.painter_at(rect);
    painter.rect_stroke(
        rect.shrink(2.0),
        0.0,
        Stroke::new(1.0, state.theme.border),
        StrokeKind::Inside,
    );
    painter.text(
        Pos2::new(rect.min.x + 8.0, rect.min.y + 8.0),
        egui::Align2::LEFT_TOP,
        format!("Star #{} trail", trail.id),
        FontId::proportional(10.0),
        state.theme.text_dim,
    );

    // Use a mid-frame as the backdrop, shrunk to fit the panel.
    let backdrop_id = trail
        .frames
        .get(trail.frames.len() / 2)
        .map(|(id, _)| id.clone());
    let Some(backdrop_id) = backdrop_id else {
        return;
    };
    let Some(backdrop) = state.stacking_preview.frame_pixels.get(&backdrop_id) else {
        painter.text(
            rect.center(),
            egui::Align2::CENTER_CENTER,
            "backdrop frame not loaded",
            FontId::proportional(10.0),
            state.theme.text_faint,
        );
        return;
    };

    let key = format!("__star_tracker_backdrop_{}", backdrop_id);
    if !state.image_cache.contains(&key) {
        let color_image = egui::ColorImage::from_rgba_unmultiplied(
            [backdrop.width as usize, backdrop.height as usize],
            &backdrop.rgba,
        );
        let texture = ui.ctx().load_texture(&key, color_image, egui::TextureOptions::LINEAR);
        state.image_cache.insert(key.clone(), texture);
    }
    let bw = backdrop.width as f32;
    let bh = backdrop.height as f32;
    if let Some(tex) = state.image_cache.get(&key) {
        let scale = (rect.width() / bw).min(rect.height() / bh) * 0.92;
        let display = Vec2::new(bw * scale, bh * scale);
        let centered = Rect::from_center_size(rect.center(), display);
        painter.image(
            tex.id(),
            centered,
            Rect::from_min_max(Pos2::ZERO, Pos2::new(1.0, 1.0)),
            Color32::from_rgba_unmultiplied(255, 255, 255, 200),
        );

        // Draw measured centroids as connected dots.
        let to_canvas = |x: f32, y: f32| -> Pos2 {
            Pos2::new(centered.min.x + x * scale, centered.min.y + y * scale)
        };
        let measured_color = Color32::from_rgba_unmultiplied(232, 160, 64, 220);
        let mut prev: Option<Pos2> = None;
        for (_, det) in &trail.frames {
            if let Some((x, y)) = det {
                let p = to_canvas(*x, *y);
                painter.circle_filled(p, 2.5, measured_color);
                if let Some(prev_p) = prev {
                    painter.line_segment([prev_p, p], Stroke::new(1.0, measured_color));
                }
                prev = Some(p);
            } else {
                prev = None;
            }
        }

        // Draw predicted path (dashed) when the verifier produced one.
        if state.star_tracker_ui.show_predicted {
            let orientation = alignment_data.orientation.unwrap_or_default();
            let site = site_from_state(state);
            let offsets: Vec<(String, crate::state::Alignment)> = trail
                .frames
                .iter()
                .filter_map(|(id, _)| {
                    alignment_data
                        .offsets
                        .get(id)
                        .map(|a| (id.clone(), *a))
                })
                .collect();
            let residuals = frame_residuals(
                &offsets,
                &state.stacking_preview.frame_timestamps,
                &site,
                orientation,
            );
            let predicted_color = Color32::from_rgba_unmultiplied(110, 198, 122, 200);
            // Anchor predicted path at the reference's measured trail
            // start, so visual comparison is direct.
            let anchor = trail.frames.first().and_then(|(_, det)| *det);
            if let Some((ax, ay)) = anchor {
                let mut prev_p: Option<Pos2> = None;
                for r in &residuals {
                    if let (Some(px), Some(py)) = (r.predicted_dx, r.predicted_dy) {
                        let p = to_canvas(ax + px, ay + py);
                        if let Some(prev) = prev_p {
                            // Dashed: short-on / short-off.
                            painter.line_segment(
                                [prev, p],
                                Stroke::new(1.0, predicted_color),
                            );
                        }
                        painter.circle_stroke(p, 2.0, Stroke::new(1.0, predicted_color));
                        prev_p = Some(p);
                    }
                }
            }
        }
    }
}

fn show_residual_table(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.chrome);
    painter.line_segment(
        [Pos2::new(rect.min.x, rect.min.y), Pos2::new(rect.max.x, rect.min.y)],
        Stroke::new(1.0, state.theme.border),
    );

    let Some(cid) = state.star_tracker_ui.collection_id else { return };
    let Some(alignment_data) = state.alignments.get(&cid).cloned() else { return };
    let site = site_from_state(state);
    let orientation = alignment_data.orientation.unwrap_or_default();
    let offsets: Vec<(String, crate::state::Alignment)> = alignment_data
        .offsets
        .iter()
        .map(|(k, v)| (k.clone(), *v))
        .collect();
    let residuals: Vec<FrameResidual> = frame_residuals(
        &offsets,
        &state.stacking_preview.frame_timestamps,
        &site,
        orientation,
    );

    egui::Area::new(egui::Id::new("star_tracker_table"))
        .fixed_pos(Pos2::new(rect.min.x + 6.0, rect.min.y + 4.0))
        .order(egui::Order::Foreground)
        .show(ui.ctx(), |ui| {
            ui.set_max_size(Vec2::new(rect.width() - 12.0, rect.height() - 8.0));
            ui.label(
                egui::RichText::new(format!(
                    "Orientation: {}  ·  plate scale: {:.2} \"/px",
                    describe(orientation),
                    crate::align_verify::plate_scale_arcsec_per_px(
                        site.focal_length_mm,
                        site.pixel_size_um
                    ),
                ))
                .size(10.0)
                .color(state.theme.text_dim),
            );
            ScrollArea::vertical().show(ui, |ui| {
                egui::Grid::new("star_tracker_residuals")
                    .num_columns(7)
                    .spacing([8.0, 2.0])
                    .show(ui, |ui| {
                        for h in &["asset", "Δt (s)", "Δx_meas", "Δy_meas", "Δx_pred", "Δy_pred", "|r| (px)"] {
                            ui.label(
                                egui::RichText::new(*h)
                                    .size(10.0)
                                    .color(state.theme.text_dim),
                            );
                        }
                        ui.end_row();
                        for r in &residuals {
                            let res = r.residual_px();
                            let res_color = if res.is_nan() {
                                state.theme.text_faint
                            } else if res < 2.0 {
                                state.theme.success
                            } else if res < 10.0 {
                                state.theme.warn
                            } else {
                                state.theme.error
                            };
                            ui.label(
                                egui::RichText::new(trim(&r.asset_id, 8))
                                    .size(10.0)
                                    .color(state.theme.text),
                            );
                            ui.label(opt_label(r.dt_seconds, state.theme.text));
                            ui.label(num_label(r.measured_dx, state.theme.text));
                            ui.label(num_label(r.measured_dy, state.theme.text));
                            ui.label(opt_label(r.predicted_dx, state.theme.text));
                            ui.label(opt_label(r.predicted_dy, state.theme.text));
                            ui.label(
                                egui::RichText::new(if res.is_nan() {
                                    "—".to_string()
                                } else {
                                    format!("{:.2}", res)
                                })
                                .size(10.0)
                                .color(res_color),
                            );
                            ui.end_row();
                        }
                    });
            });
        });
}

fn describe(o: ImageOrientation) -> String {
    let mut parts = Vec::new();
    if o.flip_x {
        parts.push("flipX");
    }
    if o.flip_y {
        parts.push("flipY");
    }
    if o.transpose {
        parts.push("transpose");
    }
    if parts.is_empty() {
        "identity".into()
    } else {
        parts.join("+")
    }
}

fn num_label(v: f32, color: Color32) -> egui::RichText {
    egui::RichText::new(format!("{:.2}", v)).size(10.0).color(color)
}

fn opt_label(v: Option<f32>, color: Color32) -> egui::RichText {
    match v {
        Some(x) => egui::RichText::new(format!("{:.2}", x)).size(10.0).color(color),
        None => egui::RichText::new("—").size(10.0).color(color),
    }
}

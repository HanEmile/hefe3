/// RGB + luminance histogram for the current image.
///
/// Computes per-channel histograms (256 bins each for R, G, B) from the
/// currently selected image's pixel data, and draws them as semi-transparent
/// overlaid curves. An optional luminance curve (0.2126R + 0.7152G + 0.0722B)
/// is overlaid in white.
///
/// Histogram data is cached and only recomputed when the selected asset changes.
///
/// Last reviewed: 2026-04-30 | Review frequency: monthly

use egui::{
    Align2, Color32, FontId, Pos2, Rect, Stroke, Ui,
};

use crate::state::AppState;

/// Number of bins per channel.
const NUM_BINS: usize = 256;

/// Padding inside the histogram panel.
const PADDING: f32 = 8.0;

/// Height reserved for the axis label area below the histogram.
const AXIS_HEIGHT: f32 = 16.0;

/// Cached histogram data, keyed by asset ID.
pub struct HistogramData {
    /// The asset ID this histogram was computed for.
    pub asset_id: String,
    /// Red channel histogram (256 bins).
    pub red: [u32; NUM_BINS],
    /// Green channel histogram (256 bins).
    pub green: [u32; NUM_BINS],
    /// Blue channel histogram (256 bins).
    pub blue: [u32; NUM_BINS],
    /// Luminance histogram (256 bins).
    pub luminance: [u32; NUM_BINS],
    /// Maximum bin value across all channels, for normalization.
    pub max_value: u32,
}

impl HistogramData {
    /// Compute histogram data from raw RGBA pixel data.
    pub fn from_rgba(asset_id: &str, pixels: &[u8]) -> Self {
        let mut red = [0u32; NUM_BINS];
        let mut green = [0u32; NUM_BINS];
        let mut blue = [0u32; NUM_BINS];
        let mut luminance = [0u32; NUM_BINS];

        for pixel in pixels.chunks_exact(4) {
            let r = pixel[0] as usize;
            let g = pixel[1] as usize;
            let b = pixel[2] as usize;

            red[r] += 1;
            green[g] += 1;
            blue[b] += 1;

            // ITU-R BT.709 luminance coefficients.
            let lum = (0.2126 * pixel[0] as f64
                + 0.7152 * pixel[1] as f64
                + 0.0722 * pixel[2] as f64) as usize;
            let lum = lum.min(NUM_BINS - 1);
            luminance[lum] += 1;
        }

        let max_value = red
            .iter()
            .chain(green.iter())
            .chain(blue.iter())
            .copied()
            .max()
            .unwrap_or(1);

        Self {
            asset_id: asset_id.to_string(),
            red,
            green,
            blue,
            luminance,
            max_value,
        }
    }
}

/// Render the histogram panel.
///
/// Displays overlaid R/G/B/luminance histogram curves for the currently
/// selected image (or the live stacking preview when no asset is
/// selected), or a placeholder when no pixels are available yet.
pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);

    // Background fill.
    painter.rect_filled(rect, 0.0, state.theme.canvas);

    // Resolve the source pixels:
    //  - If an asset is selected, read from `stacking_preview.frame_pixels`
    //    (populated on import alongside the GPU thumbnail texture).
    //  - Otherwise, fall back to the live stacking preview output.
    //
    // We store the asset id + a *cloned* pixel buffer so we can mutate
    // `state.histogram_data` afterwards without holding a borrow into
    // the pixel storage.
    let resolved: Option<(String, Vec<u8>)> = match &state.selected_asset {
        Some(asset) => state
            .stacking_preview
            .frame_pixels
            .get(&asset.id)
            .map(|f| (asset.id.clone(), f.rgba.clone())),
        None => state
            .stacking_preview
            .output
            .as_ref()
            .map(|img| (crate::stacking::preview_asset_id().to_string(), img.rgba.clone())),
    };

    let Some((asset_id, pixels)) = resolved else {
        let msg = if state.selected_asset.is_some() {
            "Waiting for thumbnail decode…"
        } else {
            "No image loaded"
        };
        painter.text(
            rect.center(),
            Align2::CENTER_CENTER,
            msg,
            FontId::proportional(14.0),
            state.theme.text_faint,
        );
        return;
    };

    // Compute or retrieve cached histogram.
    let needs_recompute = state
        .histogram_data
        .as_ref()
        .map_or(true, |h| h.asset_id != asset_id);

    if needs_recompute {
        state.histogram_data = Some(HistogramData::from_rgba(&asset_id, &pixels));
    }

    let Some(histogram) = &state.histogram_data else {
        return;
    };

    // Drawable area for the histogram curves.
    let draw_rect = Rect::from_min_max(
        Pos2::new(rect.min.x + PADDING, rect.min.y + PADDING),
        Pos2::new(rect.max.x - PADDING, rect.max.y - PADDING - AXIS_HEIGHT),
    );

    if draw_rect.width() <= 0.0 || draw_rect.height() <= 0.0 {
        return;
    }

    // Draw subtle grid lines.
    for i in 1..4 {
        let y = draw_rect.min.y + draw_rect.height() * (i as f32 / 4.0);
        painter.line_segment(
            [
                Pos2::new(draw_rect.min.x, y),
                Pos2::new(draw_rect.max.x, y),
            ],
            Stroke::new(0.5, state.theme.border),
        );
    }

    let max_val = histogram.max_value.max(1) as f32;

    // Draw each channel as a filled polygon.
    draw_channel_curve(
        &painter,
        draw_rect,
        &histogram.red,
        max_val,
        Color32::from_rgba_unmultiplied(220, 50, 50, 100),
        Color32::from_rgba_unmultiplied(220, 50, 50, 160),
    );
    draw_channel_curve(
        &painter,
        draw_rect,
        &histogram.green,
        max_val,
        Color32::from_rgba_unmultiplied(50, 200, 50, 100),
        Color32::from_rgba_unmultiplied(50, 200, 50, 160),
    );
    draw_channel_curve(
        &painter,
        draw_rect,
        &histogram.blue,
        max_val,
        Color32::from_rgba_unmultiplied(60, 100, 240, 100),
        Color32::from_rgba_unmultiplied(60, 100, 240, 160),
    );

    // Luminance curve — white, semi-transparent, line only (no fill).
    draw_channel_line(
        &painter,
        draw_rect,
        &histogram.luminance,
        max_val,
        Color32::from_rgba_unmultiplied(220, 220, 220, 140),
    );

    // Axis labels.
    painter.text(
        Pos2::new(draw_rect.min.x, rect.max.y - 4.0),
        Align2::LEFT_BOTTOM,
        "0",
        FontId::monospace(9.0),
        state.theme.text_faint,
    );
    painter.text(
        Pos2::new(draw_rect.max.x, rect.max.y - 4.0),
        Align2::RIGHT_BOTTOM,
        "255",
        FontId::monospace(9.0),
        state.theme.text_faint,
    );
}

/// Draw a single histogram channel as a filled polygon with a top stroke.
fn draw_channel_curve(
    painter: &egui::Painter,
    rect: Rect,
    bins: &[u32; NUM_BINS],
    max_val: f32,
    fill_color: Color32,
    stroke_color: Color32,
) {
    let bin_width = rect.width() / NUM_BINS as f32;

    // Build top points of the curve.
    let mut top_points: Vec<Pos2> = Vec::with_capacity(NUM_BINS);
    for (i, &count) in bins.iter().enumerate() {
        let x = rect.min.x + (i as f32 + 0.5) * bin_width;
        let normalized = (count as f32 / max_val).sqrt(); // sqrt scale for better visibility
        let y = rect.max.y - normalized * rect.height();
        top_points.push(Pos2::new(x, y));
    }

    // Build the filled polygon: top curve + bottom edge.
    let mut polygon_points = Vec::with_capacity(NUM_BINS + 2);
    polygon_points.push(Pos2::new(rect.min.x, rect.max.y));
    polygon_points.extend_from_slice(&top_points);
    polygon_points.push(Pos2::new(rect.max.x, rect.max.y));

    // Draw filled area.
    painter.add(egui::Shape::convex_polygon(
        polygon_points,
        fill_color,
        Stroke::NONE,
    ));

    // Draw the top stroke.
    for window in top_points.windows(2) {
        painter.line_segment(
            [window[0], window[1]],
            Stroke::new(1.0, stroke_color),
        );
    }
}

/// Draw a single histogram channel as a line only (no fill).
fn draw_channel_line(
    painter: &egui::Painter,
    rect: Rect,
    bins: &[u32; NUM_BINS],
    max_val: f32,
    stroke_color: Color32,
) {
    let bin_width = rect.width() / NUM_BINS as f32;

    let mut prev = None;
    for (i, &count) in bins.iter().enumerate() {
        let x = rect.min.x + (i as f32 + 0.5) * bin_width;
        let normalized = (count as f32 / max_val).sqrt();
        let y = rect.max.y - normalized * rect.height();
        let point = Pos2::new(x, y);

        if let Some(prev_point) = prev {
            painter.line_segment(
                [prev_point, point],
                Stroke::new(1.2, stroke_color),
            );
        }
        prev = Some(point);
    }
}

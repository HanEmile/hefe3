/// Star detection — point-source extraction for visualization and
/// for the per-frame quality / FWHM metrics. **Not** used for
/// computing alignments anymore; that lives in `align.rs` and uses
/// FFT phase correlation, which is much more robust to gradients,
/// foreground objects, and missing detections.
///
/// Pipeline (single pass per frame):
///
/// 1. Compute luma.
/// 2. **High-pass** by subtracting a wide box-blur (the same trick
///    `align::align` uses). This zeros sky-glow, the orange roof,
///    and diffuse nebulae — leaving only point-source signal.
/// 3. Estimate σ via MAD on the high-passed image (one-sided, lower
///    half only — stars and any leftover bright artifacts skew the
///    upper half).
/// 4. Find local maxima above `5σ` in the high-passed image.
/// 5. 5×5 weighted centroid for sub-pixel position; horizontal +
///    vertical 1-D extent estimator for FWHM.
/// 6. Sort by peak, keep the brightest `max`.
///
/// The high-pass step alone replaces the entire previous mess of
/// tile-median bg models, foreground masks, p75 rules, and dilation —
/// all of which fought each other on real data. Subtracting a 64-px
/// box blur is robust to anything low-frequency (gradients, nebulae,
/// roofs) without distinguishing between them, because we don't need
/// to.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use crate::align;
use crate::state::FramePixels;

/// One detected star.
#[derive(Debug, Clone, Copy)]
pub struct DetectedStar {
    /// Sub-pixel x in image coordinates.
    pub x: f32,
    /// Sub-pixel y in image coordinates.
    pub y: f32,
    /// High-pass-filter signal at the centroid (arbitrary units —
    /// only used for ranking and the focus score).
    pub peak: f32,
    /// Approximate FWHM in pixels.
    pub fwhm: f32,
}

/// Detect up to `max` stars in `frame`. Returns the brightest first.
pub fn detect_stars(frame: &FramePixels, max: usize) -> Vec<DetectedStar> {
    let w = frame.width as usize;
    let h = frame.height as usize;
    if w < 32 || h < 32 {
        return Vec::new();
    }

    let luma = align::compute_luma(frame);
    let blur_radius = ((w.min(h) / 32).max(8)).min(64) as i32;
    let signal = align::high_pass(&luma, w, h, blur_radius);

    let sigma = mad_sigma_lower(&signal);
    let threshold = (5.0 * sigma).max(2.0);

    // Skip a small border — the high-pass filter is unreliable within
    // `blur_radius` of the image edge.
    let border = (blur_radius as usize).max(2);

    let mut candidates: Vec<(usize, usize, f32)> = Vec::new();
    for y in border..h.saturating_sub(border) {
        for x in border..w.saturating_sub(border) {
            let v = signal[y * w + x];
            if v < threshold {
                continue;
            }
            let mut is_max = true;
            'outer: for dy in -1..=1_i32 {
                for dx in -1..=1_i32 {
                    if dx == 0 && dy == 0 {
                        continue;
                    }
                    let n = signal[((y as i32 + dy) as usize) * w + (x as i32 + dx) as usize];
                    if n > v {
                        is_max = false;
                        break 'outer;
                    }
                }
            }
            if is_max {
                candidates.push((x, y, v));
            }
        }
    }

    let mut stars: Vec<DetectedStar> = candidates
        .into_iter()
        .filter_map(|(cx, cy, peak)| centroid_5x5(&signal, w, h, cx, cy, peak))
        .collect();

    stars.sort_by(|a, b| b.peak.partial_cmp(&a.peak).unwrap_or(std::cmp::Ordering::Equal));
    stars.truncate(max);
    stars
}

/// One-sided MAD: take only the half of the distribution at-or-below
/// the median (pure noise) so star peaks don't inflate the estimate.
/// Sample sparsely (every 17th pixel).
fn mad_sigma_lower(signal: &[f32]) -> f32 {
    let mut sample: Vec<f32> = signal.iter().copied().step_by(17).collect();
    if sample.is_empty() {
        return 1.0;
    }
    sample.sort_by(|a, b| a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal));
    let median = sample[sample.len() / 2];
    let mut deviations: Vec<f32> = sample
        .iter()
        .filter(|v| **v <= median)
        .map(|v| (v - median).abs())
        .collect();
    if deviations.is_empty() {
        return 1.0;
    }
    deviations.sort_by(|a, b| a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal));
    let mad = deviations[deviations.len() / 2].max(0.5);
    1.4826 * mad
}

fn centroid_5x5(
    signal: &[f32],
    w: usize,
    h: usize,
    cx: usize,
    cy: usize,
    peak: f32,
) -> Option<DetectedStar> {
    if cx < 2 || cy < 2 || cx + 2 >= w || cy + 2 >= h {
        return None;
    }
    let mut sum = 0.0_f32;
    let mut sx = 0.0_f32;
    let mut sy = 0.0_f32;
    for dy in -2..=2_i32 {
        for dx in -2..=2_i32 {
            let v =
                signal[((cy as i32 + dy) as usize) * w + (cx as i32 + dx) as usize].max(0.0);
            sum += v;
            sx += v * (cx as f32 + dx as f32);
            sy += v * (cy as f32 + dy as f32);
        }
    }
    if sum < 1.0 {
        return None;
    }
    let x = sx / sum;
    let y = sy / sum;
    let fwhm = estimate_fwhm(signal, w, h, cx, cy, peak);
    Some(DetectedStar { x, y, peak, fwhm })
}

fn estimate_fwhm(signal: &[f32], w: usize, h: usize, cx: usize, cy: usize, peak: f32) -> f32 {
    if peak <= 0.0 {
        return f32::NAN;
    }
    let half = peak * 0.5;
    let mut hx = 0;
    for d in 1..6_usize {
        let xl = cx.saturating_sub(d);
        let xr = (cx + d).min(w - 1);
        let vl = signal[cy * w + xl];
        let vr = signal[cy * w + xr];
        if vl <= half && vr <= half {
            hx = d;
            break;
        }
    }
    let mut hy = 0;
    for d in 1..6_usize {
        let yu = cy.saturating_sub(d);
        let yd = (cy + d).min(h - 1);
        let vu = signal[yu * w + cx];
        let vd = signal[yd * w + cx];
        if vu <= half && vd <= half {
            hy = d;
            break;
        }
    }
    ((hx + hy) as f32).max(1.0)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn synth_field(w: u32, h: u32, stars: &[(f32, f32, f32)], bg_grad: bool) -> FramePixels {
        let mut rgba = vec![0u8; (w * h * 4) as usize];
        for y in 0..h {
            for x in 0..w {
                let bg = if bg_grad {
                    20.0 + 60.0 * (y as f32 / h as f32)
                } else {
                    20.0
                };
                let mut v = bg;
                for &(cx, cy, peak) in stars {
                    let dx = x as f32 - cx;
                    let dy = y as f32 - cy;
                    v += peak * (-(dx * dx + dy * dy) / 4.0).exp();
                }
                let v = v.clamp(0.0, 255.0) as u8;
                let i = ((y * w + x) * 4) as usize;
                rgba[i] = v;
                rgba[i + 1] = v;
                rgba[i + 2] = v;
                rgba[i + 3] = 255;
            }
        }
        FramePixels { width: w, height: h, rgba }
    }

    #[test]
    fn detects_a_synthetic_star() {
        let frame = synth_field(128, 128, &[(64.5, 70.2, 200.0)], false);
        let stars = detect_stars(&frame, 5);
        assert!(!stars.is_empty(), "expected at least one detection");
        let s = stars[0];
        assert!((s.x - 64.5).abs() < 1.5, "x={} (truth 64.5)", s.x);
        assert!((s.y - 70.2).abs() < 1.5, "y={} (truth 70.2)", s.y);
    }

    #[test]
    fn detects_through_strong_gradient() {
        let stars_truth = &[
            (200.0, 50.0, 120.0),
            (200.0, 250.0, 120.0),
        ];
        let frame = synth_field(400, 300, stars_truth, /*bg_grad=*/ true);
        let stars = detect_stars(&frame, 10);
        assert!(stars.len() >= 2, "got {} stars (need 2)", stars.len());
    }

    #[test]
    fn detects_many_stars_does_not_crash() {
        let many: Vec<(f32, f32, f32)> = (0..50)
            .map(|i| {
                let cx = 30.0 + ((i * 37) % 200) as f32;
                let cy = 30.0 + ((i * 71) % 200) as f32;
                (cx, cy, 100.0 + (i % 50) as f32)
            })
            .collect();
        let frame = synth_field(256, 256, &many, false);
        let stars = detect_stars(&frame, 200);
        assert!(stars.len() >= 30, "got {} stars (expected ≥30)", stars.len());
    }
}

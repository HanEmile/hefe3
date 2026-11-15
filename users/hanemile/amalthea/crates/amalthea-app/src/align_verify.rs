/// Earth-rotation cross-check + auto-calibrated image orientation.
///
/// Given per-frame `(dx, dy)` measured by the auto-align solver and
/// per-frame UTC timestamps, we predict the *expected* drift due to
/// Earth's rotation alone (assuming an un-tracked mount) and compare.
///
/// The math:
///
///   Sidereal rate = 360° / 86164.0905s ≈ 15.0411 arcsec/s.
///   Apparent star motion at declination δ:
///     Δ_RA  = ω · (t - t_ref) · cos(δ)         (eastward)
///     Δ_Dec = 0                                  (no diurnal motion)
///
/// Converted to pixels using the camera's plate scale:
///
///   plate_scale = 206.265 × pixel_size_µm / focal_length_mm   [arcsec/px]
///
/// Image orientation discovery picks one of 8 candidate axis
/// mappings ({flipX, flipY, transpose}) by minimizing the residual
/// between predicted and measured drift across all frame pairs.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use chrono::{DateTime, Utc};

use crate::auto_align::SiteSnapshot;
use crate::state::{Alignment, ImageOrientation};

/// Sidereal rotation rate in arcseconds per second of UTC time.
const SIDEREAL_ARCSEC_PER_SEC: f64 = 15.041_067_178_637_3;

/// Plate scale in arcseconds per pixel for the given optics.
pub fn plate_scale_arcsec_per_px(focal_length_mm: f64, pixel_size_um: f64) -> f64 {
    if focal_length_mm <= 0.0 {
        return 0.0;
    }
    206.264_806 * pixel_size_um / focal_length_mm
}

/// Predicted drift in *sky* coordinates (RA, Dec) in pixels between
/// `t_ref` and `t`. Returns `(dRA_px, dDec_px)`. Dec is always 0 for
/// an un-tracked mount but kept in the signature so a future mount-
/// model upgrade can extend it.
pub fn predict_sky_drift(
    t: DateTime<Utc>,
    t_ref: DateTime<Utc>,
    target_dec_deg: f64,
    plate_scale: f64,
) -> (f64, f64) {
    if plate_scale <= 0.0 {
        return (0.0, 0.0);
    }
    let dt_sec = (t - t_ref).num_milliseconds() as f64 / 1000.0;
    let cos_dec = (target_dec_deg.to_radians()).cos();
    let drift_arcsec = SIDEREAL_ARCSEC_PER_SEC * dt_sec * cos_dec;
    (drift_arcsec / plate_scale, 0.0)
}

/// Apply an `ImageOrientation` to a sky-coordinate drift, producing
/// the corresponding image-pixel drift.
pub fn sky_to_image(d_ra_px: f64, d_dec_px: f64, o: ImageOrientation) -> (f64, f64) {
    let (mut x, mut y) = if o.transpose {
        (d_dec_px, d_ra_px)
    } else {
        (d_ra_px, d_dec_px)
    };
    if o.flip_x {
        x = -x;
    }
    if o.flip_y {
        y = -y;
    }
    (x, y)
}

/// Auto-calibrate the image orientation by trying all 8 candidate
/// `ImageOrientation` values and picking the one with the lowest
/// residual against the measured per-frame drifts. Returns `None`
/// when there isn't enough signal (no plate scale, no timestamps,
/// or fewer than 2 usable pairs).
pub fn auto_calibrate_orientation(
    offsets: &[(String, Alignment)],
    timestamps: &std::collections::HashMap<String, DateTime<Utc>>,
    site: &SiteSnapshot,
) -> Option<ImageOrientation> {
    let plate_scale = plate_scale_arcsec_per_px(site.focal_length_mm, site.pixel_size_um);
    if plate_scale <= 0.0 {
        return None;
    }
    if site.target_dec.abs() > 89.5 {
        // Too close to a pole — drift collapses to zero, no signal.
        return None;
    }

    // Use the first frame with a timestamp as the reference for
    // prediction. Measured offsets are already relative to the
    // collection's reference, but for a differential check we just
    // need *consistent* deltas.
    let mut samples: Vec<(f64, f64, f64)> = Vec::new(); // (dt_sec, dx_meas, dy_meas)
    let Some(ref_ts) = offsets
        .iter()
        .find_map(|(id, _)| timestamps.get(id).copied())
    else {
        return None;
    };
    for (id, alignment) in offsets {
        if alignment.confidence < 0.2 {
            continue;
        }
        let Some(ts) = timestamps.get(id).copied() else {
            continue;
        };
        let dt = (ts - ref_ts).num_milliseconds() as f64 / 1000.0;
        // Skip the reference frame and anything within 0.5s of it.
        if dt.abs() < 0.5 {
            continue;
        }
        samples.push((dt, alignment.dx as f64, alignment.dy as f64));
    }
    if samples.len() < 2 {
        return None;
    }

    let candidates = [
        ImageOrientation { flip_x: false, flip_y: false, transpose: false },
        ImageOrientation { flip_x: true, flip_y: false, transpose: false },
        ImageOrientation { flip_x: false, flip_y: true, transpose: false },
        ImageOrientation { flip_x: true, flip_y: true, transpose: false },
        ImageOrientation { flip_x: false, flip_y: false, transpose: true },
        ImageOrientation { flip_x: true, flip_y: false, transpose: true },
        ImageOrientation { flip_x: false, flip_y: true, transpose: true },
        ImageOrientation { flip_x: true, flip_y: true, transpose: true },
    ];

    let mut best: Option<(ImageOrientation, f64)> = None;
    for &o in &candidates {
        let mut sse = 0.0;
        for &(dt, mx, my) in &samples {
            let drift_arcsec = SIDEREAL_ARCSEC_PER_SEC * dt * site.target_dec.to_radians().cos();
            let pred_ra_px = drift_arcsec / plate_scale;
            let (px, py) = sky_to_image(pred_ra_px, 0.0, o);
            let ex = mx - px;
            let ey = my - py;
            sse += ex * ex + ey * ey;
        }
        match best {
            None => best = Some((o, sse)),
            Some((_, b)) if sse < b => best = Some((o, sse)),
            _ => {}
        }
    }
    best.map(|(o, _)| o)
}

/// Predicted vs measured drift for each frame, in image pixels. Used
/// by the star tracker panel to overlay the predicted path.
pub fn frame_residuals(
    offsets: &[(String, Alignment)],
    timestamps: &std::collections::HashMap<String, DateTime<Utc>>,
    site: &SiteSnapshot,
    orientation: ImageOrientation,
) -> Vec<FrameResidual> {
    let plate_scale = plate_scale_arcsec_per_px(site.focal_length_mm, site.pixel_size_um);
    let Some(ref_ts) = offsets
        .iter()
        .find_map(|(id, _)| timestamps.get(id).copied())
    else {
        return Vec::new();
    };
    offsets
        .iter()
        .map(|(id, alignment)| {
            let predicted = timestamps.get(id).copied().map(|ts| {
                let dt = (ts - ref_ts).num_milliseconds() as f64 / 1000.0;
                let (dra, ddec) = predict_sky_drift(ts, ref_ts, site.target_dec, plate_scale);
                let (px, py) = sky_to_image(dra, ddec, orientation);
                (dt, px as f32, py as f32)
            });
            FrameResidual {
                asset_id: id.clone(),
                measured_dx: alignment.dx,
                measured_dy: alignment.dy,
                predicted_dx: predicted.map(|(_, x, _)| x),
                predicted_dy: predicted.map(|(_, _, y)| y),
                dt_seconds: predicted.map(|(dt, _, _)| dt as f32),
            }
        })
        .collect()
}

/// One row in the residual table — measured vs predicted drift for a
/// single frame.
#[derive(Debug, Clone)]
pub struct FrameResidual {
    pub asset_id: String,
    pub measured_dx: f32,
    pub measured_dy: f32,
    pub predicted_dx: Option<f32>,
    pub predicted_dy: Option<f32>,
    pub dt_seconds: Option<f32>,
}

impl FrameResidual {
    /// Euclidean residual in pixels. NaN when prediction unavailable.
    pub fn residual_px(&self) -> f32 {
        match (self.predicted_dx, self.predicted_dy) {
            (Some(px), Some(py)) => {
                let ex = self.measured_dx - px;
                let ey = self.measured_dy - py;
                (ex * ex + ey * ey).sqrt()
            }
            _ => f32::NAN,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    #[test]
    fn plate_scale_typical_lens() {
        // 400 mm focal length, 4.6 µm pixel — typical APS-C kit.
        let s = plate_scale_arcsec_per_px(400.0, 4.6);
        // ~2.37 arcsec/px
        assert!((s - 2.372).abs() < 0.01, "got {s}");
    }

    #[test]
    fn drift_zero_at_celestial_pole() {
        let t0 = Utc.with_ymd_and_hms(2026, 5, 1, 22, 0, 0).unwrap();
        let t1 = Utc.with_ymd_and_hms(2026, 5, 1, 22, 0, 30).unwrap();
        let plate = plate_scale_arcsec_per_px(400.0, 4.6);
        let (dra, _) = predict_sky_drift(t1, t0, 90.0, plate);
        assert!(dra.abs() < 1e-6, "got {dra}");
    }

    #[test]
    fn drift_celestial_equator_30s() {
        let t0 = Utc.with_ymd_and_hms(2026, 5, 1, 22, 0, 0).unwrap();
        let t1 = Utc.with_ymd_and_hms(2026, 5, 1, 22, 0, 30).unwrap();
        let plate = plate_scale_arcsec_per_px(400.0, 4.6);
        let (dra, ddec) = predict_sky_drift(t1, t0, 0.0, plate);
        // 15.04"/s × 30s = 451.2"; / 2.37"/px ≈ 190 px
        assert!((dra - 190.3).abs() < 1.0, "got {dra}");
        assert_eq!(ddec, 0.0);
    }

    #[test]
    fn sky_to_image_flips() {
        let identity = ImageOrientation::default();
        assert_eq!(sky_to_image(10.0, 5.0, identity), (10.0, 5.0));
        let flip_x = ImageOrientation { flip_x: true, flip_y: false, transpose: false };
        assert_eq!(sky_to_image(10.0, 5.0, flip_x), (-10.0, 5.0));
        let xpose = ImageOrientation { flip_x: false, flip_y: false, transpose: true };
        assert_eq!(sky_to_image(10.0, 5.0, xpose), (5.0, 10.0));
    }
}

/// Auto-alignment job — FFT phase correlation for the actual transform,
/// star detection only for visualization (`TrackedStar` trails).
///
/// Pipeline:
///
/// 1. Detect stars in every frame (`stars::detect_stars`). Used to
///    pick the reference (frame with the most clean detections, which
///    is also the sharpest) and to populate `TrackedStar` trails.
///
/// 2. For each non-reference frame, run `align::align(reference, frame)`
///    — FFT phase correlation gives sub-pixel `(dx, dy)` directly,
///    immune to sky-glow gradients, foreground objects (orange roof in
///    our test set), and missing-star bookkeeping. Theta = 0; rotation
///    is below the noise floor for fixed-mount sub-second exposures.
///
/// 3. Project each reference star through the per-frame transform to
///    locate its expected position in every frame, then snap to the
///    nearest detection (within 9 px). That builds the trail
///    overlaid by the Star Tracker panel.
///
/// 4. Hand off to `align_verify::auto_calibrate_orientation` to pick
///    the image-pixel-to-sky-axis mapping that best matches the
///    measured drifts (only when site / target / focal data are set).
///
/// All results — alignments, trails, and orientation — are returned
/// in a single `AlignJobResult` and merged into state on the main
/// thread.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use crate::align;
use crate::stars::{detect_stars, DetectedStar};
use crate::state::{
    AlignJobResult, AlignJobs, Alignment, AppState, FramePixels, ImageOrientation, TrackedStar,
};

/// Spawn a background auto-align job for the given collection. The
/// reference frame is read from `state.alignments[id].reference` (or
/// the first accepted item if none is set yet).
pub fn spawn_auto_align(state: &mut AppState, collection_id: u64) {
    if state.align_jobs.is_none() {
        state.align_jobs = Some(AlignJobs::new());
    }
    let tx = state.align_jobs.as_ref().unwrap().tx.clone();

    let collection = match state.collection(collection_id) {
        Some(c) => c.clone(),
        None => return,
    };
    // Honour an explicit reference if the user picked one; otherwise
    // the worker auto-picks (frame with the most detections).
    let requested_reference: Option<String> = state
        .alignments
        .get(&collection_id)
        .and_then(|a| a.reference.clone());

    let mut frames: Vec<(String, FramePixels)> = Vec::new();
    for item in &collection.items {
        if !item.accepted {
            continue;
        }
        if let Some(px) = state.stacking_preview.frame_pixels.get(&item.asset_id) {
            frames.push((item.asset_id.clone(), px.clone()));
        }
    }
    if frames.is_empty() {
        state.add_log("WARN", "align", "no decoded frames yet — try again after they load");
        return;
    }

    state.align_running = true;
    state.align_status = Some(format!("aligning {} frames…", frames.len()));
    state.add_log(
        "INFO",
        "align",
        &format!(
            "auto-align: {} frames in collection {}",
            frames.len(),
            collection_id
        ),
    );

    // Snapshot what the orientation calibrator needs (timestamps + site
    // + target) so the worker can compute it without touching state.
    let timestamps = state.stacking_preview.frame_timestamps.clone();
    let site = SiteSnapshot {
        lat: state.trip_lat,
        lon: state.trip_lon,
        target_ra: state.trip_target_ra as f64,
        target_dec: state.trip_target_dec as f64,
        focal_length_mm: state.trip_focal_length as f64,
        pixel_size_um: state.trip_pixel_size as f64,
    };
    let n_tracked = state.alignment_ui.tracked_star_count.max(4).min(32) as usize;

    std::thread::spawn(move || {
        let result = run_auto_align(
            collection_id,
            requested_reference,
            frames,
            timestamps,
            site,
            n_tracked,
        );
        let _ = tx.send(result);
    });
}

/// Drain any completed auto-align jobs and merge their results.
pub fn pump_results(state: &mut AppState) {
    let Some(jobs) = state.align_jobs.as_ref() else {
        return;
    };
    let mut latest: Option<AlignJobResult> = None;
    while let Ok(msg) = jobs.rx.try_recv() {
        latest = Some(msg);
    }
    if let Some(result) = latest {
        let entry = state.alignments.entry(result.collection_id).or_default();
        for (asset_id, alignment) in result.offsets {
            entry.offsets.insert(asset_id, alignment);
        }
        entry.star_trails = result.star_trails;
        if let Some(o) = result.orientation {
            entry.orientation = Some(o);
        }
        if let Some(r) = result.chosen_reference {
            entry.reference = Some(r);
        }
        state.align_running = false;
        state.align_status = Some(result.status.clone());
        state.add_log("INFO", "align", &result.status);
        state.stacking_preview.dirty = true;
    }
}

/// Snapshot of the site / target / camera fields needed by the
/// rotation cross-check. Captured at job-spawn time so the worker
/// thread doesn't borrow `AppState`. Reused by the star tracker
/// panel when it computes per-frame residuals on the main thread.
#[derive(Clone, Copy)]
pub struct SiteSnapshot {
    /// Observer latitude in degrees (-90..90). Reserved — alt-az
    /// field-rotation prediction will need this; not yet used by the
    /// MVP equatorial drift check.
    pub lat: f64,
    /// Observer longitude in degrees (-180..180). Reserved — used by
    /// the alt-az / sidereal-time follow-up.
    pub lon: f64,
    /// Target right ascension in degrees (0..360). Reserved — see
    /// `lat`/`lon`.
    pub target_ra: f64,
    /// Target declination in degrees (-90..90). The translation
    /// cross-check scales by `cos(δ)`.
    pub target_dec: f64,
    /// Telescope focal length in mm. Drives `plate_scale`.
    pub focal_length_mm: f64,
    /// Camera pixel size in micrometres. Drives `plate_scale`.
    pub pixel_size_um: f64,
}

fn run_auto_align(
    collection_id: u64,
    requested_reference: Option<String>,
    frames: Vec<(String, FramePixels)>,
    timestamps: std::collections::HashMap<String, chrono::DateTime<chrono::Utc>>,
    site: SiteSnapshot,
    n_tracked_stars: usize,
) -> AlignJobResult {
    if frames.is_empty() {
        return AlignJobResult {
            collection_id,
            offsets: Vec::new(),
            star_trails: Vec::new(),
            orientation: None,
            chosen_reference: None,
            status: "auto-align: no frames".to_string(),
        };
    }

    // Detect stars in every frame. They drive: reference auto-pick,
    // tracked-star trails for the panel, and the image-viewer overlay.
    let per_frame_stars: Vec<(String, Vec<DetectedStar>)> = frames
        .iter()
        .map(|(id, frame)| (id.clone(), detect_stars(frame, 200)))
        .collect();

    // Pick the reference. Honour an explicit choice if it's in the
    // input set; otherwise auto-pick the frame with the most
    // detections (= sharpest + deepest in our test data).
    let reference_id: String = requested_reference
        .as_ref()
        .filter(|id| frames.iter().any(|(fid, _)| fid == *id))
        .cloned()
        .unwrap_or_else(|| {
            per_frame_stars
                .iter()
                .max_by_key(|(_, s)| s.len())
                .map(|(id, _)| id.clone())
                .unwrap_or_else(|| frames[0].0.clone())
        });

    let Some((_, ref_frame)) = frames.iter().find(|(id, _)| id == &reference_id) else {
        return AlignJobResult {
            collection_id,
            offsets: Vec::new(),
            star_trails: Vec::new(),
            orientation: None,
            chosen_reference: None,
            status: format!("auto-align: reference {reference_id} not in frames"),
        };
    };
    let ref_stars: &Vec<DetectedStar> = &per_frame_stars
        .iter()
        .find(|(id, _)| id == &reference_id)
        .map(|(_, s)| s.clone())
        .unwrap_or_default();

    // Solve per-frame Alignment via FFT phase correlation.
    let mut offsets: Vec<(String, Alignment)> = Vec::with_capacity(frames.len());
    for (id, frame) in &frames {
        if id == &reference_id {
            offsets.push((
                id.clone(),
                Alignment {
                    dx: 0.0,
                    dy: 0.0,
                    theta: 0.0,
                    anchor: ref_stars.first().map(|s| (s.x as u32, s.y as u32)),
                    confidence: 1.0,
                },
            ));
            continue;
        }
        if frame.width != ref_frame.width || frame.height != ref_frame.height {
            continue;
        }
        if let Some(mut alignment) = align::align(ref_frame, frame) {
            // Pick a per-frame anchor from the brightest matching star
            // for the alignment overlay (purely cosmetic — the
            // Alignment is already complete without it).
            let other_stars = per_frame_stars
                .iter()
                .find(|(fid, _)| fid == id)
                .map(|(_, s)| s.as_slice())
                .unwrap_or(&[]);
            alignment.anchor = other_stars.first().map(|s| (s.x as u32, s.y as u32));
            offsets.push((id.clone(), alignment));
        }
    }

    // Build TrackedStar trails. For each of the top-N reference stars,
    // project through the per-frame alignment and snap to the nearest
    // detection in that frame.
    let trail_count = ref_stars.len().min(n_tracked_stars.max(4).min(32));
    let mut trails: Vec<TrackedStar> = (0..trail_count)
        .map(|i| TrackedStar {
            id: i as u32,
            ref_x: ref_stars[i].x,
            ref_y: ref_stars[i].y,
            frames: Vec::with_capacity(per_frame_stars.len()),
        })
        .collect();

    for (id, stars) in &per_frame_stars {
        let alignment = offsets
            .iter()
            .find(|(aid, _)| aid == id)
            .map(|(_, a)| *a)
            .unwrap_or_default();
        for trail in trails.iter_mut() {
            // Project the reference star into this frame's coordinate
            // space. The transform `align::align` returns maps `other`
            // → `reference`: (x_ref, y_ref) = R·(x_o, y_o) + (dx, dy).
            // To go the other way (ref → other) with theta=0 we
            // simply subtract.
            let px = trail.ref_x - alignment.dx;
            let py = trail.ref_y - alignment.dy;

            let mut best: Option<(f32, f32, f32)> = None;
            for s in stars {
                let dx = s.x - px;
                let dy = s.y - py;
                let d2 = dx * dx + dy * dy;
                if best.map_or(true, |(_, _, bd)| d2 < bd) {
                    best = Some((s.x, s.y, d2));
                }
            }
            let detection = match best {
                Some((x, y, d2)) if d2 < 9.0 * 9.0 => Some((x, y)),
                _ => None,
            };
            trail.frames.push((id.clone(), detection));
        }
    }

    // Auto-calibrate image orientation if we have the inputs to do so.
    let orientation = crate::align_verify::auto_calibrate_orientation(
        &offsets,
        &timestamps,
        &site,
    );

    let n = offsets.len();
    AlignJobResult {
        collection_id,
        offsets,
        star_trails: trails,
        orientation,
        chosen_reference: Some(reference_id.clone()),
        status: format!(
            "auto-align: {n} frames against {reference_id}, {} stars tracked{}",
            trail_count,
            match orientation {
                Some(o) => format!(", orientation {}", describe_orientation(o)),
                None => String::new(),
            }
        ),
    }
}

fn describe_orientation(o: ImageOrientation) -> String {
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
        "identity".to_string()
    } else {
        parts.join("+")
    }
}

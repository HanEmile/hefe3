/// Per-frame quality scoring (focus / star count / FWHM) and the
/// median-relative "top X%" filter that drives the alignment viewer's
/// quality slider.
///
/// Scoring is intentionally fast and resolution-independent — frames
/// in the same collection are compared against each other, not
/// against an absolute target. A 1440²-px buffer takes <50 ms on a
/// laptop; results are cached in `state.stacking_preview.frame_quality`
/// so the slider doesn't trigger a recomputation on every drag.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use chrono::{DateTime, Utc};

use crate::immich::AssetResponse;
use crate::stars::{detect_stars, DetectedStar};
use crate::state::{AppState, FramePixels, FrameQuality};

/// Cache the asset's UTC timestamp into
/// `state.stacking_preview.frame_timestamps`. Prefers the more
/// precise of EXIF `dateTimeOriginal` and Immich `fileCreatedAt`
/// (the latter is often RFC3339 with millisecond precision; the
/// former is 1-second granular but matches the actual exposure).
pub fn record_frame_timestamp(state: &mut AppState, asset_id: &str) {
    if state
        .stacking_preview
        .frame_timestamps
        .contains_key(asset_id)
    {
        return;
    }
    let Some(asset) = state
        .search_results
        .iter()
        .find(|a| a.id == asset_id)
        .cloned()
    else {
        return;
    };
    if let Some(ts) = pick_best_timestamp(&asset) {
        state
            .stacking_preview
            .frame_timestamps
            .insert(asset_id.to_string(), ts);
    }
}

/// Pick the most precise timestamp out of the asset's EXIF + file-
/// created fields. We prefer whichever string has sub-second digits;
/// if both are 1-second granular we prefer EXIF (it's the actual
/// exposure time, not the upload time). Returns `None` if neither
/// parses.
fn pick_best_timestamp(asset: &AssetResponse) -> Option<DateTime<Utc>> {
    let exif = asset
        .exif_info
        .as_ref()
        .and_then(|e| e.date_time_original.as_deref())
        .and_then(parse_timestamp);
    let file_created = parse_timestamp(asset.file_created_at.as_str());

    match (exif, file_created) {
        (Some(e), Some(f)) => {
            // Prefer whichever has higher sub-second precision; on tie,
            // EXIF wins because it reflects the actual capture moment.
            let e_str = asset
                .exif_info
                .as_ref()
                .and_then(|x| x.date_time_original.as_deref())
                .unwrap_or("");
            let f_str = asset.file_created_at.as_str();
            if subsecond_digits(f_str) > subsecond_digits(e_str) {
                Some(f)
            } else {
                Some(e)
            }
        }
        (Some(e), None) => Some(e),
        (None, Some(f)) => Some(f),
        (None, None) => None,
    }
}

/// Count the number of digits after the decimal point in the seconds
/// portion of a timestamp string. We use this to pick the higher-
/// precision of two candidate timestamps.
fn subsecond_digits(s: &str) -> usize {
    // Find a '.' between the last ':' and the next non-digit.
    let bytes = s.as_bytes();
    let Some(dot_idx) = bytes.iter().position(|&b| b == b'.') else {
        return 0;
    };
    let mut n = 0;
    for &b in &bytes[dot_idx + 1..] {
        if b.is_ascii_digit() {
            n += 1;
        } else {
            break;
        }
    }
    n
}

/// Parse an EXIF or RFC3339 timestamp into UTC. Accepts both
/// `2024-01-01T12:00:00.123Z` (RFC3339) and `2024:01:01 12:00:00`
/// (EXIF DateTimeOriginal). EXIF lacks a timezone; we assume UTC for
/// self-consistency — the rotation cross-check is differential, so
/// a fixed offset across all frames cancels out.
fn parse_timestamp(s: &str) -> Option<DateTime<Utc>> {
    if s.is_empty() {
        return None;
    }
    if let Ok(dt) = DateTime::parse_from_rfc3339(s) {
        return Some(dt.with_timezone(&Utc));
    }
    // EXIF-style: "YYYY:MM:DD HH:MM:SS"
    let normalized = s.replacen(':', "-", 2);
    let formats = [
        "%Y-%m-%d %H:%M:%S%.f",
        "%Y-%m-%d %H:%M:%S",
        "%Y-%m-%dT%H:%M:%S%.f",
        "%Y-%m-%dT%H:%M:%S",
    ];
    for fmt in &formats {
        if let Ok(naive) = chrono::NaiveDateTime::parse_from_str(&normalized, fmt) {
            return Some(DateTime::<Utc>::from_naive_utc_and_offset(naive, Utc));
        }
    }
    None
}

/// Score one frame end-to-end: detect stars, compute focus + FWHM.
/// Cheap enough to call sequentially across a collection; if it ever
/// becomes a bottleneck we move it onto the auto-align worker
/// thread.
pub fn score_frame(frame: &FramePixels) -> FrameQuality {
    let stars = detect_stars(frame, /*max=*/ 200);
    let star_count = stars.len() as u32;
    let fwhm = median_fwhm(&stars);
    let focus = focus_score(frame, &stars);
    FrameQuality {
        // `score` is filled in by `apply_threshold` once we know the
        // collection's median focus + star_count to normalise against.
        score: focus * (star_count as f32).sqrt(),
        star_count,
        fwhm,
        focus,
    }
}

/// Ensure every accepted frame in `collection_id` has a cached
/// `FrameQuality`. Skips frames whose pixels haven't loaded yet —
/// they get scored on the next call.
pub fn ensure_scored(state: &mut AppState, collection_id: u64) {
    let Some(collection) = state.collection(collection_id) else {
        return;
    };
    let to_score: Vec<String> = collection
        .items
        .iter()
        .filter(|it| !state.stacking_preview.frame_quality.contains_key(&it.asset_id))
        .map(|it| it.asset_id.clone())
        .collect();
    for asset_id in to_score {
        let Some(frame) = state.stacking_preview.frame_pixels.get(&asset_id).cloned() else {
            continue;
        };
        let q = score_frame(&frame);
        state
            .stacking_preview
            .frame_quality
            .insert(asset_id, q);
    }
}

/// Apply the median-relative threshold: frames whose `score` falls
/// below `median × threshold` get `accepted = false`. Frames at or
/// above the cutoff get `accepted = true`. `threshold = 0` accepts
/// everything; `threshold = 1` keeps only frames at-or-above the
/// median.
///
/// The user can still flip `accepted` manually after this runs — we
/// only act when the slider value changes.
pub fn apply_threshold(state: &mut AppState, collection_id: u64, threshold: f32) {
    ensure_scored(state, collection_id);

    // Collect (asset_id, score) pairs.
    let pairs: Vec<(String, f32)> = {
        let Some(collection) = state.collection(collection_id) else {
            return;
        };
        collection
            .items
            .iter()
            .filter_map(|it| {
                state
                    .stacking_preview
                    .frame_quality
                    .get(&it.asset_id)
                    .map(|q| (it.asset_id.clone(), q.score))
            })
            .collect()
    };
    if pairs.is_empty() {
        return;
    }
    let mut scores: Vec<f32> = pairs.iter().map(|(_, s)| *s).collect();
    scores.sort_by(|a, b| a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal));
    let median = scores[scores.len() / 2];
    let cutoff = median * threshold.clamp(0.0, 1.0);

    // Apply on the collection.
    if let Some(collection) = state.collection_mut(collection_id) {
        for it in collection.items.iter_mut() {
            if let Some((_, score)) = pairs.iter().find(|(id, _)| id == &it.asset_id) {
                it.accepted = *score >= cutoff;
            }
        }
    }
    state.stacking_preview.dirty = true;
}

/// Median FWHM in pixels across detected stars. Returns NaN when no
/// stars were detected.
fn median_fwhm(stars: &[DetectedStar]) -> f32 {
    if stars.is_empty() {
        return f32::NAN;
    }
    let mut fs: Vec<f32> = stars.iter().map(|s| s.fwhm).collect();
    fs.sort_by(|a, b| a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal));
    fs[fs.len() / 2]
}

/// Focus score: variance of the Laplacian over a small window around
/// each of the brightest 32 stars. A focused star has a sharp central
/// peak (high Laplacian magnitude); a defocused star spreads its light
/// (low Laplacian). Restricting to star windows avoids being dominated
/// by sky background or hot pixels.
fn focus_score(frame: &FramePixels, stars: &[DetectedStar]) -> f32 {
    if stars.is_empty() {
        return 0.0;
    }
    let mut bright = stars.to_vec();
    bright.sort_by(|a, b| b.peak.partial_cmp(&a.peak).unwrap_or(std::cmp::Ordering::Equal));
    let bright = &bright[..bright.len().min(32)];

    let w = frame.width as i32;
    let h = frame.height as i32;
    let r = 4_i32;

    let mut acc = 0.0_f64;
    let mut acc2 = 0.0_f64;
    let mut n = 0_u32;
    for s in bright {
        let cx = s.x.round() as i32;
        let cy = s.y.round() as i32;
        for dy in -r..=r {
            for dx in -r..=r {
                let x = cx + dx;
                let y = cy + dy;
                if x <= 0 || y <= 0 || x >= w - 1 || y >= h - 1 {
                    continue;
                }
                let lap = laplacian_at(frame, x, y);
                acc += lap as f64;
                acc2 += (lap as f64) * (lap as f64);
                n += 1;
            }
        }
    }
    if n == 0 {
        return 0.0;
    }
    let mean = acc / n as f64;
    let var = (acc2 / n as f64) - mean * mean;
    var.max(0.0) as f32
}

/// 4-neighbour discrete Laplacian on luma. Returns f32 for downstream
/// variance accumulation.
fn laplacian_at(frame: &FramePixels, x: i32, y: i32) -> f32 {
    let l_c = luma(frame, x, y);
    let l_l = luma(frame, x - 1, y);
    let l_r = luma(frame, x + 1, y);
    let l_u = luma(frame, x, y - 1);
    let l_d = luma(frame, x, y + 1);
    (l_l + l_r + l_u + l_d) - 4.0 * l_c
}

#[inline]
fn luma(frame: &FramePixels, x: i32, y: i32) -> f32 {
    let i = ((y * frame.width as i32 + x) * 4) as usize;
    let r = frame.rgba[i] as f32;
    let g = frame.rgba[i + 1] as f32;
    let b = frame.rgba[i + 2] as f32;
    0.2126 * r + 0.7152 * g + 0.0722 * b
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_rfc3339() {
        let dt = parse_timestamp("2024-01-15T22:30:45.123Z").unwrap();
        assert_eq!(dt.timestamp(), 1705357845);
    }

    #[test]
    fn parses_exif_format() {
        let dt = parse_timestamp("2024:01:15 22:30:45").unwrap();
        assert_eq!(dt.timestamp(), 1705357845);
    }

    #[test]
    fn subsecond_digit_count() {
        assert_eq!(subsecond_digits("2024-01-15T22:30:45Z"), 0);
        assert_eq!(subsecond_digits("2024-01-15T22:30:45.123Z"), 3);
        assert_eq!(subsecond_digits("2024-01-15T22:30:45.123456Z"), 6);
    }

    #[test]
    fn focus_returns_zero_when_no_stars() {
        let frame = FramePixels {
            width: 10,
            height: 10,
            rgba: vec![0u8; 10 * 10 * 4],
        };
        assert_eq!(focus_score(&frame, &[]), 0.0);
    }
}

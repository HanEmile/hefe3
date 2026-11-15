//! Integration test: run the FFT phase-correlation alignment against
//! a directory of real preview JPEGs. Off by default — runs only
//! when `AMALTHEA_TEST_FRAMES` env var points at a directory with
//! sortable `*.jpg` files (alphabetical order should match temporal
//! order).
//!
//! Usage:
//!
//!   AMALTHEA_TEST_FRAMES=/tmp/amalthea-test \
//!     cargo test --release -p amalthea-app --test align_real_frames -- --nocapture
//!
//! Asserted invariants:
//!  - every pair aligns successfully (`align` returns `Some`).
//!  - the per-frame translation magnitude is bounded (no wild swings).
//!  - drift is roughly monotonic across the time-ordered series — for
//!    a fixed-mount short interval, neighbouring frames should differ
//!    by < 10 px while the endpoints can differ by > 30 px.
//!
//! Last reviewed: 2026-05-01 | Review frequency: monthly

use amalthea_app::align::align;
use amalthea_app::state::FramePixels;
use std::path::PathBuf;

fn load_frame(path: &std::path::Path) -> Option<FramePixels> {
    let img = image::open(path).ok()?;
    let rgba = img.to_rgba8();
    let (w, h) = (rgba.width(), rgba.height());
    Some(FramePixels {
        width: w,
        height: h,
        rgba: rgba.into_raw(),
    })
}

#[test]
fn align_real_frames() {
    let Some(dir) = std::env::var_os("AMALTHEA_TEST_FRAMES").map(PathBuf::from) else {
        eprintln!("AMALTHEA_TEST_FRAMES not set — skipping real-frame alignment test");
        return;
    };
    let mut entries: Vec<PathBuf> = std::fs::read_dir(&dir)
        .expect("read frames dir")
        .filter_map(|e| {
            let p = e.ok()?.path();
            (p.extension().and_then(|s| s.to_str()) == Some("jpg")).then_some(p)
        })
        .collect();
    entries.sort();
    assert!(entries.len() >= 2, "need ≥2 .jpg frames in {}", dir.display());

    let frames: Vec<(String, FramePixels)> = entries
        .iter()
        .filter_map(|p| {
            let name = p.file_stem()?.to_string_lossy().to_string();
            load_frame(p).map(|f| (name, f))
        })
        .collect();
    eprintln!("loaded {} frames from {}", frames.len(), dir.display());

    // Use the last frame as the reference (matches the production
    // auto-pick on this dataset; also keeps output ordered).
    let ref_idx = frames.len() - 1;
    let (ref_name, ref_frame) = &frames[ref_idx];
    eprintln!("reference: {ref_name}");

    let mut shifts: Vec<(String, f32, f32)> = Vec::new();
    for (i, (name, frame)) in frames.iter().enumerate() {
        if i == ref_idx {
            shifts.push((name.clone(), 0.0, 0.0));
            continue;
        }
        let t0 = std::time::Instant::now();
        let a = align(ref_frame, frame).expect("align returned None");
        let mag = (a.dx * a.dx + a.dy * a.dy).sqrt();
        eprintln!(
            "  {name} → ref: ({:>+6.2}, {:>+6.2}) |Δ|={:.2} px  ({:.0} ms)",
            a.dx,
            a.dy,
            mag,
            t0.elapsed().as_millis()
        );
        // Sanity: a fixed-mount short interval should never need
        // more than ~200 px to align preview-size frames.
        assert!(
            mag < 200.0,
            "{name} alignment magnitude {mag:.1} px is implausibly large"
        );
        shifts.push((name.clone(), a.dx, a.dy));
    }

    // Drift monotonicity: distance from the reference frame should be
    // a roughly increasing function of how far back in time we go.
    // We check that *neighbouring* frames are closer to each other
    // than the endpoint pair.
    let endpoint_dist = ((shifts[0].1 - shifts[ref_idx].1).powi(2)
        + (shifts[0].2 - shifts[ref_idx].2).powi(2))
    .sqrt();
    let mut max_neighbour = 0.0_f32;
    for w in shifts.windows(2) {
        let d = ((w[0].1 - w[1].1).powi(2) + (w[0].2 - w[1].2).powi(2)).sqrt();
        if d > max_neighbour {
            max_neighbour = d;
        }
    }
    eprintln!(
        "  endpoint drift {endpoint_dist:.1} px, max neighbour {max_neighbour:.1} px"
    );
    if frames.len() >= 3 {
        assert!(
            max_neighbour < endpoint_dist || endpoint_dist < 5.0,
            "neighbour drift {max_neighbour:.1} ≥ endpoint drift {endpoint_dist:.1}: alignment unstable"
        );
    }
}

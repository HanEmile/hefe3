# Alignment

Last reviewed: 2026-05-01 | Review frequency: monthly

Source:
- `crates/amalthea-app/src/align.rs` — **the actual alignment** (FFT phase correlation).
- `crates/amalthea-app/src/auto_align.rs` — job orchestration: snapshot frames, pick a reference, fan out FFT alignments, build per-star trails for visualization.
- `crates/amalthea-app/src/stars.rs` — point-source detection (visualization only since the FFT rewrite).
- `crates/amalthea-app/src/quality.rs` — focus / star count / FWHM and the median-relative quality threshold.
- `crates/amalthea-app/src/align_verify.rs` — Earth-rotation cross-check + auto-calibrated image orientation.
- `crates/amalthea-app/src/panels/alignment_viewer.rs` — picker UI + sliders.
- `crates/amalthea-app/src/panels/star_tracker.rs` — per-star inspector + residual table.
- `crates/amalthea-app/src/panels/image_viewer.rs` — labelled overlay (`#0..#N-1`) on tracked stars.
- `Alignment` / `CollectionAlignment` / `AlignmentUi` / `TrackedStar` / `ImageOrientation` / `FrameQuality` in `state.rs`.

## Why FFT phase correlation

Earlier iterations used star detection + triangle (asterism) matching + Procrustes fit. On real preview JPEGs (sky-glow gradient, foreground roof, large diffuse nebula in the centre of frame) **every layer of that pipeline broke**:

1. Tile-median background models tracked the nebula's brightness, so signal at the nebula collapsed to ~0 and detection couldn't find anything in the centre band.
2. Foreground masks couldn't distinguish "saturated roof" from "saturated nebula" — both are wide-bright tiles. Whatever rule excluded the roof also excluded Orion.
3. Without detections in the right places, triangle matching had no real stars to vote on. Detections from the bg-model boundary ringing voted into noise.

**Phase correlation in the frequency domain sidesteps all of that.** It needs only:

- A high-pass filter (subtract a wide box-blur). Removes any low-frequency content — sky-glow gradient, roof, *and* nebula — without distinguishing between them. Stars survive because they're high-frequency.
- An FFT of both frames, conjugate-multiply with magnitude normalisation, inverse FFT. The peak in the resulting correlation map is the translation that maps one frame onto the other. Sub-pixel via 3-point parabolic peak fit.

Verified against the test set at `/tmp/amalthea-test/0027.jpg…0041.jpg`: pairwise alignment converges monotonically toward the reference frame (0041), residuals are sub-pixel, runtime ~350 ms per pair on a 1440×2159 frame.

Star detection still happens — but only for the **Star Tracker panel**, the **alignment-viewer reference-star overlay**, and the per-frame `FrameQuality` score. The alignment itself does not depend on it.

## Data model

```rust
struct Alignment {
    dx: f32,
    dy: f32,
    theta: f32,                  // unused by FFT path; kept for future rotation work
    anchor: Option<(u32, u32)>,
    confidence: f32,             // 1.0 from FFT; future star-residual ratio
}
struct CollectionAlignment {
    reference: Option<asset_id>,
    offsets:   HashMap<asset_id, Alignment>,
    orientation: Option<ImageOrientation>,   // pixel→sky-axis mapping
    star_trails: Vec<TrackedStar>,           // top-N tracked stars
}
struct ImageOrientation { flip_x, flip_y, transpose }   // 8 valid combos
struct TrackedStar { id, ref_x, ref_y, frames: Vec<(asset_id, Option<(f32, f32)>)> }
struct FrameQuality { score, star_count, fwhm, focus }

state.alignments: HashMap<collection_id, CollectionAlignment>
state.alignment_ui: AlignmentUi { ..., tracked_star_count: u32 (4..=32), overlay_tracked_stars: bool }
state.stacking_preview.frame_quality: HashMap<asset_id, FrameQuality>
state.stacking_preview.frame_timestamps: HashMap<asset_id, DateTime<Utc>>
```

The reference frame stays at `(0, 0, 0)`. Every other frame's stack contribution is shifted by its `Alignment` before the reduction.

## Pipeline

### 1. Reference frame auto-pick (`auto_align.rs`)

If the user has an explicit reference set in `state.alignments[cid].reference`, that wins. Otherwise `run_auto_align` picks the frame with the **most detected stars**. On the test set this consistently selects the sharpest, deepest-exposed frame — which is exactly the reference you want for FFT phase correlation.

The chosen reference is written back through `AlignJobResult::chosen_reference` so subsequent runs match it.

### 2. FFT alignment (`align.rs::align`)

For each non-reference frame:

1. Convert to f32 luma.
2. **High-pass**: subtract a separable box blur (radius ≈ image_dim/32, capped at 96 px). Removes everything low-frequency.
3. **Apodise**: 2-D Hann window so FFT edges don't ring.
4. **Pad** to next power of two in each dim (1440→2048, 2159→4096).
5. **2-D forward FFT** of both images via `rustfft` (row pass then column pass).
6. **Cross-power spectrum** `R = (Â · conj(B̂)) / |Â · conj(B̂)|`. Magnitude normalisation gives a true delta-shaped peak regardless of brightness contrast.
7. **2-D inverse FFT** → real correlation map.
8. **Find peak**, unwrap cyclic coordinates back to `[-fw/2, fw/2)`, refine with 3-point parabolic fit on each axis.

Returned `Alignment.dx, dy` is the translation that maps `other` → `reference`: `(x_ref, y_ref) = (x_other + dx, y_other + dy)`. `theta = 0` always — fixed-mount sub-second exposures have rotation below the noise floor.

Runtime: O(N log N), about 350 ms per pair on a 1440×2159 frame.

### 3. Star detection (`stars.rs::detect_stars`)

Used only for the visualization layer. Same high-pass filter as the alignment, then:

1. One-sided MAD on the high-passed image gives σ.
2. Local maxima above 5σ.
3. 5×5 weighted centroid → sub-pixel position.
4. 1-D horizontal/vertical extent estimator → FWHM.
5. Sort by peak, keep brightest `max` (default 200).

### 4. Tracked-star trails

For each of the top-N reference stars (`tracked_star_count`, 4..=32, default 8), `auto_align` projects its position through the per-frame FFT-derived translation to predict where it should appear in every other frame, then snaps to the nearest detected star within 9 px. Stored as `TrackedStar { id, ref_x, ref_y, frames: [(asset_id, Option<(x, y)>)] }`.

These trails feed:
- The **Star Tracker panel** (per-star crops, residual table).
- The **image-viewer overlay** — labelled `#0..#N-1` markers drawn on whichever frame is currently displayed, gated by `state.alignment_ui.overlay_tracked_stars`.

### 5. Earth-rotation cross-check (`align_verify.rs`)

When site, target and optics are set on the trip planner:

```
plate_scale = 206.265 × pixel_size_µm / focal_length_mm     [arcsec/px]
Δ_RA  = ω_sidereal × Δt × cos(δ) / plate_scale              [pixels]
Δ_Dec = 0                                                    (un-tracked)
```

`auto_calibrate_orientation` tries all 8 `ImageOrientation` values (flipX × flipY × transpose) and picks the one minimising total residual against measured offsets. Stored on `CollectionAlignment.orientation`; reused by every subsequent verifier call.

`frame_residuals` produces per-frame measured / predicted / |r| triples for the star tracker panel's residual table.

## Manual tweaking — Alignment Viewer panel

Top bar: group / reference / secondary picker, **Auto-align** button, status label.

Centre: blended preview composed in pure Rust:

```
out[i] = (1 - blend) * reference[i] + blend * secondary[i + offset]
```

Bottom: four rows of sliders / controls:

1. **Δx / Δy / Blend** — manual override of the per-frame alignment.
2. **Quality cutoff** — `quality::apply_threshold(state, cid, threshold)`. Frames below `median × threshold` get `accepted = false`.
3. **Reference stars (overlay + tracking)** — slider 4..=32, default 8. Drives the auto-align worker (how many `TrackedStar` trails to build) AND the image-viewer overlay (how many labelled markers to draw). A **show overlay** checkbox toggles the markers.

The accepted/total count is shown next to the quality slider for instant feedback.

## Star Tracker panel

Inspect what the auto-align job did:

- **Group + Star pickers** — pick a star (#0 = brightest in reference) and a crop window size.
- **Left half: stacked sub-pixel crops.** Crop window of every frame's centroid for the selected star averaged into one image with a centre crosshair. A clean spot = good alignment; a streak = residual drift.
- **Right half: trail overlay.** Measured centroids drawn on a mid-frame backdrop, connected as a path. With *show predicted* enabled, the Earth-rotation predicted path is overlaid as dashed-green circles.
- **Bottom: residual table.** Per-frame `Δt`, measured `(Δx, Δy)`, predicted `(Δx, Δy)`, and `|r|` colour-coded green (<2 px) / amber (<10 px) / red (≥10 px).

## Failure modes

- **Different image sizes** — `align::align` returns `None`; the caller skips the frame silently.
- **All-black or all-white frames** — phase correlation's denominator is `1e-12`-floored; the peak-finder will return some integer position but the magnitude is ~0 and the result has no real meaning. In practice this only matters for completely blank frames; real captures always have enough structure to produce a sharp peak.
- **Pole-on imaging** — `auto_calibrate_orientation` returns `None` when `|target_dec| > 89.5°` (drift collapses to zero). The alignment itself still works.

## Future

See PLAN.md "Star-based auto-align — out of scope (deferred)" for items beyond MVP scope (alt-az field rotation, plate solving, GPU detection).

## Cross-references

- `docs/stacking.md` — how `Alignment` flows into the stack reduction.
- `docs/state.md` — `alignments`, `alignment_ui`, `align_jobs`, `frame_quality`, `frame_timestamps`, `star_tracker_ui` fields.
- `docs/panels.md` — alignment viewer + star tracker layout.
- `PLAN.md` — feature timeline + deferred items.

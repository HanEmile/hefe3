/// FFT phase-correlation alignment between two frames.
///
/// For a fixed-camera astro setup the dominant motion between exposures
/// is **translation**: stars drift across the sensor due to Earth
/// rotation, plus tiny camera bumps. Phase correlation in the
/// frequency domain finds that translation directly without any star
/// detection — robust to noise, gradients, and missing stars, with
/// sub-pixel accuracy via parabolic refinement of the correlation peak.
///
/// Algorithm:
///
/// 1. Convert both frames to f32 luma.
/// 2. **High-pass** by subtracting a wide-radius box blur. This
///    removes sky-glow gradients, the orange-roof foreground, and
///    diffuse nebulae in one step — anything low-frequency. Stars,
///    being high-frequency, survive.
/// 3. **Apodize** with a 2-D Hann window so the FFT doesn't see the
///    image edges as a discontinuity (which would produce a cross-
///    shaped artifact in the correlation map).
/// 4. **Pad** to the next power of two in each dimension. `rustfft`
///    accepts any size but power-of-two is several × faster.
/// 5. **2-D FFT** of both images: row-FFTs, transpose, "row-FFTs" of
///    the transposed buffer (= column FFTs of the original).
/// 6. **Cross-power spectrum** `R = (Â · conj(B̂)) / |Â · conj(B̂)|`.
///    Normalising by magnitude gives "phase correlation" — the inverse
///    transform is a sharp delta at the translation offset, regardless
///    of brightness contrast.
/// 7. **Inverse 2-D FFT** → real-valued correlation map.
/// 8. **Find peak** (integer position).
/// 9. **Sub-pixel refinement** via 3-point parabola fit on the peak.
///
/// Returned `(dx, dy)` is the translation that maps coordinates in
/// `other` back to `reference`: `(x_ref, y_ref) = (x_other + dx, y_other + dy)`.
/// `theta = 0` always — phase correlation does not handle rotation.
/// For our use case (fixed mount, sub-second drift) rotation is well
/// below the noise floor.
///
/// # Performance
///
/// Hot-path optimisations layered on top of the rustfft kernel:
///
/// - **Process-wide planner + plan caches** behind a `LazyLock<Mutex<…>>`
///   keyed by transform length. Each `align()` only touches the mutex
///   to look up two cached plan handles (one for the row length, one
///   for the column length); no re-planning per call.
/// - **Hann window cache** — the 2-D apodization window only depends on
///   `(w, h)` and is `Arc`-shared across calls.
/// - **`process_with_scratch`** instead of rustfft's allocating
///   `process()`. Each rayon worker keeps its own scratch buffer via
///   `for_each_init`.
/// - **Independent passes run in parallel via `rayon::join`**: the two
///   luma extractions, the two high-passes, the two windowed copies,
///   and the two forward 2-D FFTs.
/// - **Row-major FFT pass** for both row and column directions: row
///   pass → transpose → row pass on transposed buffer → transpose
///   back. Both row passes operate on contiguous memory and parallel
///   trivially.
/// - **Wasm fallback** — rayon is host-only (`cfg(not(target_arch = "wasm32"))`),
///   wasm falls through to the serial path.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use std::sync::{Arc, LazyLock, Mutex};

use rustfft::{num_complex::Complex, Fft, FftPlanner};

#[cfg(not(target_arch = "wasm32"))]
use rayon::prelude::*;

use crate::state::{Alignment, FramePixels};

/// Solve for the translation that maps `other` onto `reference`.
/// Returns `None` only if the input dimensions disagree or are zero.
pub fn align(reference: &FramePixels, other: &FramePixels) -> Option<Alignment> {
    if reference.width == 0 || reference.height == 0 {
        return None;
    }
    if reference.width != other.width || reference.height != other.height {
        return None;
    }
    let w = reference.width as usize;
    let h = reference.height as usize;

    // Stage timing — enabled by setting `AMALTHEA_ALIGN_TRACE=1` in
    // the environment. Off by default; the env lookup only fires once
    // per call, which is fine.
    let trace = std::env::var_os("AMALTHEA_ALIGN_TRACE").is_some();
    let elapsed = |stage: std::time::Instant, name: &str| {
        if trace {
            eprintln!("    align: {name:<14} {:>4} ms", stage.elapsed().as_millis());
        }
    };

    // 1. Luma — runs the two extractions in parallel since they don't
    //    share any state.
    let s = std::time::Instant::now();
    #[cfg(not(target_arch = "wasm32"))]
    let (ref_luma, other_luma) =
        rayon::join(|| compute_luma(reference), || compute_luma(other));
    #[cfg(target_arch = "wasm32")]
    let (ref_luma, other_luma) = (compute_luma(reference), compute_luma(other));
    elapsed(s, "luma");

    // 2. High-pass.
    let s = std::time::Instant::now();
    let blur_radius = ((w.min(h) / 32).max(16)).min(96) as i32;
    #[cfg(not(target_arch = "wasm32"))]
    let (ref_hp, other_hp) = rayon::join(
        || high_pass(&ref_luma, w, h, blur_radius),
        || high_pass(&other_luma, w, h, blur_radius),
    );
    #[cfg(target_arch = "wasm32")]
    let (ref_hp, other_hp) = (
        high_pass(&ref_luma, w, h, blur_radius),
        high_pass(&other_luma, w, h, blur_radius),
    );
    elapsed(s, "high_pass");

    // 3. Apodize with a 2-D Hann window (cached by size). The window
    //    is the same for both frames, so we only build it once and
    //    Arc-share it.
    let s = std::time::Instant::now();
    let window = hann_window_2d_cached(w, h);
    #[cfg(not(target_arch = "wasm32"))]
    let (ref_win, other_win) = rayon::join(
        || apply_window(&ref_hp, &window),
        || apply_window(&other_hp, &window),
    );
    #[cfg(target_arch = "wasm32")]
    let (ref_win, other_win) = (apply_window(&ref_hp, &window), apply_window(&other_hp, &window));
    elapsed(s, "window");

    // 4. Pad to power-of-two FFT size.
    let fw = next_pow2(w);
    let fh = next_pow2(h);

    // 5–7. Phase-correlate via FFT.
    let s = std::time::Instant::now();
    let correlation = phase_correlation(&ref_win, &other_win, w, h, fw, fh);
    elapsed(s, "fft");

    // 8. Find the peak in the correlation map. The peak position is
    //    in *cyclic* coordinates: a peak at (px, py) means the shift
    //    is (px, py) modulo (fw, fh). Wrap large values back into
    //    the [-fw/2, fw/2) range.
    let s = std::time::Instant::now();
    let (px, py, _peak_val) = find_peak(&correlation, fw, fh);
    elapsed(s, "peak");

    let cyclic_dx = if px >= fw / 2 { px as f32 - fw as f32 } else { px as f32 };
    let cyclic_dy = if py >= fh / 2 { py as f32 - fh as f32 } else { py as f32 };

    // 9. Sub-pixel parabolic refinement.
    let (sub_dx, sub_dy) = subpixel_parabola(&correlation, fw, fh, px, py);

    let dx = cyclic_dx + sub_dx;
    let dy = cyclic_dy + sub_dy;

    Some(Alignment {
        dx,
        dy,
        theta: 0.0,
        anchor: None,
        confidence: 1.0,
    })
}

// ---------------------------------------------------------------------------
// Caches
// ---------------------------------------------------------------------------

/// Forward + inverse complex FFT pair for a fixed transform length,
/// with the scratch sizes the `process_with_scratch` calls expect.
#[derive(Clone)]
struct ComplexFftPair {
    fwd: Arc<dyn Fft<f32>>,
    inv: Arc<dyn Fft<f32>>,
    fwd_scratch: usize,
    inv_scratch: usize,
}

/// Process-wide caches. Building planners and individual plans is
/// non-trivial — we want to do it once per transform length and
/// then keep clones cheap. The mutex is held only for the lookup;
/// the FFTs themselves run unsynchronised.
static FFT_CACHE: LazyLock<Mutex<std::collections::HashMap<usize, ComplexFftPair>>> =
    LazyLock::new(|| Mutex::new(std::collections::HashMap::new()));
static FFT_PLANNER: LazyLock<Mutex<FftPlanner<f32>>> =
    LazyLock::new(|| Mutex::new(FftPlanner::new()));

fn fft_pair(len: usize) -> ComplexFftPair {
    let mut cache = FFT_CACHE.lock().unwrap();
    if let Some(e) = cache.get(&len) {
        return e.clone();
    }
    let mut planner = FFT_PLANNER.lock().unwrap();
    let fwd = planner.plan_fft_forward(len);
    let inv = planner.plan_fft_inverse(len);
    let pair = ComplexFftPair {
        fwd_scratch: fwd.get_inplace_scratch_len(),
        inv_scratch: inv.get_inplace_scratch_len(),
        fwd,
        inv,
    };
    cache.insert(len, pair.clone());
    pair
}

/// Hann window cache. `(w, h) → Arc<Vec<f32>>`. Keeps a small set of
/// commonly-seen sizes hot.
static HANN_CACHE: LazyLock<Mutex<std::collections::HashMap<(usize, usize), Arc<Vec<f32>>>>> =
    LazyLock::new(|| Mutex::new(std::collections::HashMap::new()));

fn hann_window_2d_cached(w: usize, h: usize) -> Arc<Vec<f32>> {
    let mut cache = HANN_CACHE.lock().unwrap();
    if let Some(v) = cache.get(&(w, h)) {
        return v.clone();
    }
    let v = Arc::new(hann_window_2d(w, h));
    cache.insert((w, h), v.clone());
    v
}

// ---------------------------------------------------------------------------
// Pixel ops (parallel where it matters)
// ---------------------------------------------------------------------------

/// Public so `stars.rs` can reuse the same luma extraction.
pub(crate) fn compute_luma(frame: &FramePixels) -> Vec<f32> {
    let n = (frame.width as usize) * (frame.height as usize);
    let mut out = vec![0.0_f32; n];
    let src = &frame.rgba;

    #[cfg(not(target_arch = "wasm32"))]
    {
        // BT.709 luma in parallel chunks. Each chunk owns its slice of
        // the output, so there's no synchronisation overhead.
        out.par_chunks_mut(8192)
            .zip(src.par_chunks(8192 * 4))
            .for_each(|(dst, src_chunk)| {
                for (o, p) in dst.iter_mut().zip(src_chunk.chunks_exact(4)) {
                    *o = 0.2126 * p[0] as f32 + 0.7152 * p[1] as f32 + 0.0722 * p[2] as f32;
                }
            });
        return out;
    }
    #[cfg(target_arch = "wasm32")]
    {
        for i in 0..n {
            let p = i * 4;
            out[i] = 0.2126 * src[p] as f32 + 0.7152 * src[p + 1] as f32 + 0.0722 * src[p + 2] as f32;
        }
        out
    }
}

/// Subtract a wide box-blur from the image. Result has zero local
/// mean — sky-glow gradients, foreground patches, and nebulae all
/// vanish; only sub-pixel features (stars, edges) remain.
pub(crate) fn high_pass(src: &[f32], w: usize, h: usize, radius: i32) -> Vec<f32> {
    let blurred = box_blur(src, w, h, radius);

    #[cfg(not(target_arch = "wasm32"))]
    {
        let mut out = vec![0.0_f32; w * h];
        out.par_iter_mut()
            .zip(src.par_iter().zip(blurred.par_iter()))
            .for_each(|(o, (s, b))| *o = *s - *b);
        return out;
    }
    #[cfg(target_arch = "wasm32")]
    {
        let mut out = vec![0.0_f32; w * h];
        for i in 0..(w * h) {
            out[i] = src[i] - blurred[i];
        }
        out
    }
}

/// Separable box blur with running-sum optimisation.
///
/// Horizontal pass: rows are independent → parallel over rows.
/// Vertical pass: classically column-strided. We transpose to rows,
/// run the same horizontal-pass routine in parallel, then transpose
/// back. Transposing twice is faster than fighting the cache on a
/// column-strided pass for our sizes.
fn box_blur(src: &[f32], w: usize, h: usize, radius: i32) -> Vec<f32> {
    let r = radius.max(1) as usize;

    let mut tmp = vec![0.0_f32; w * h];
    blur_rows(src, &mut tmp, w, h, r);

    let mut transposed = vec![0.0_f32; w * h];
    transpose(&tmp, &mut transposed, w, h);
    let mut blurred_t = vec![0.0_f32; w * h];
    blur_rows(&transposed, &mut blurred_t, h, w, r);

    let mut out = vec![0.0_f32; w * h];
    transpose(&blurred_t, &mut out, h, w);
    out
}

fn blur_rows(src: &[f32], dst: &mut [f32], w: usize, h: usize, r: usize) {
    let kernel = move |y: usize, dst_row: &mut [f32]| {
        let row = &src[y * w..(y + 1) * w];
        let mut acc = 0.0_f32;
        for x in 0..(r + 1).min(w) {
            acc += row[x];
        }
        let mut left: i32 = -(r as i32);
        let mut right: i32 = r as i32;
        for x in 0..w {
            let count = ((right.min(w as i32 - 1)) - (left.max(0)) + 1).max(1) as f32;
            dst_row[x] = acc / count;
            left += 1;
            right += 1;
            if right < w as i32 {
                acc += row[right as usize];
            }
            if left > 0 {
                acc -= row[(left - 1) as usize];
            }
        }
    };

    #[cfg(not(target_arch = "wasm32"))]
    {
        dst.par_chunks_mut(w)
            .enumerate()
            .for_each(|(y, dst_row)| kernel(y, dst_row));
        let _ = h;
    }
    #[cfg(target_arch = "wasm32")]
    {
        for y in 0..h {
            let dst_row = &mut dst[y * w..(y + 1) * w];
            kernel(y, dst_row);
        }
    }
}

/// Transpose a `(h × w)` matrix into a `(w × h)` matrix. Naming
/// follows the *source* dimensions: src has `h` rows of `w` columns;
/// dst has `w` rows of `h` columns.
///
/// On host targets we parallelise over destination rows. Each
/// destination row reads a strided column from src; the strided read
/// is the cache cost, but spreading across cores hides most of it.
fn transpose(src: &[f32], dst: &mut [f32], w: usize, h: usize) {
    #[cfg(not(target_arch = "wasm32"))]
    {
        dst.par_chunks_mut(h).enumerate().for_each(|(x, col)| {
            for y in 0..h {
                col[y] = src[y * w + x];
            }
        });
        return;
    }
    #[cfg(target_arch = "wasm32")]
    {
        for y in 0..h {
            for x in 0..w {
                dst[x * h + y] = src[y * w + x];
            }
        }
    }
}

/// 2-D Hann window: `w(x, y) = 0.5·(1 - cos(2π·x/(W-1))) · 0.5·(1 - cos(2π·y/(H-1)))`.
fn hann_window_2d(w: usize, h: usize) -> Vec<f32> {
    let two_pi = std::f32::consts::TAU;
    let mut wx = vec![0.0_f32; w];
    let mut wy = vec![0.0_f32; h];
    for x in 0..w {
        wx[x] = 0.5 * (1.0 - (two_pi * x as f32 / (w as f32 - 1.0)).cos());
    }
    for y in 0..h {
        wy[y] = 0.5 * (1.0 - (two_pi * y as f32 / (h as f32 - 1.0)).cos());
    }
    let mut out = vec![0.0_f32; w * h];

    #[cfg(not(target_arch = "wasm32"))]
    {
        out.par_chunks_mut(w).enumerate().for_each(|(y, row)| {
            let wyv = wy[y];
            for x in 0..w {
                row[x] = wx[x] * wyv;
            }
        });
        return out;
    }
    #[cfg(target_arch = "wasm32")]
    {
        for y in 0..h {
            for x in 0..w {
                out[y * w + x] = wx[x] * wy[y];
            }
        }
        out
    }
}

fn apply_window(src: &[f32], window: &[f32]) -> Vec<f32> {
    #[cfg(not(target_arch = "wasm32"))]
    {
        return src
            .par_iter()
            .zip(window.par_iter())
            .map(|(s, w)| s * w)
            .collect();
    }
    #[cfg(target_arch = "wasm32")]
    {
        src.iter().zip(window.iter()).map(|(s, w)| s * w).collect()
    }
}

fn next_pow2(n: usize) -> usize {
    let mut p = 1;
    while p < n {
        p <<= 1;
    }
    p.max(2)
}

// ---------------------------------------------------------------------------
// FFT path
// ---------------------------------------------------------------------------

/// Compute the inverse-FFT of the normalised cross-power spectrum of
/// the two windowed images. Output has size `fw × fh` (row-major).
fn phase_correlation(
    a: &[f32],
    b: &[f32],
    w: usize,
    h: usize,
    fw: usize,
    fh: usize,
) -> Vec<f32> {
    // Look up plans once per call; avoids repeating the cache mutex
    // round-trip inside the FFT helpers.
    let row = fft_pair(fw);
    let col = fft_pair(fh);

    // Pad inputs and run the two forward 2-D FFTs in parallel —
    // they're completely independent.
    #[cfg(not(target_arch = "wasm32"))]
    let (mut buf_a, buf_b) = rayon::join(
        || {
            let mut buf = pad_to_complex(a, w, h, fw, fh);
            fft_2d_pass(&mut buf, fw, fh, &row.fwd, row.fwd_scratch, &col.fwd, col.fwd_scratch);
            buf
        },
        || {
            let mut buf = pad_to_complex(b, w, h, fw, fh);
            fft_2d_pass(&mut buf, fw, fh, &row.fwd, row.fwd_scratch, &col.fwd, col.fwd_scratch);
            buf
        },
    );
    #[cfg(target_arch = "wasm32")]
    let (mut buf_a, buf_b) = {
        let mut a_buf = pad_to_complex(a, w, h, fw, fh);
        fft_2d_pass(&mut a_buf, fw, fh, &row.fwd, row.fwd_scratch, &col.fwd, col.fwd_scratch);
        let mut b_buf = pad_to_complex(b, w, h, fw, fh);
        fft_2d_pass(&mut b_buf, fw, fh, &row.fwd, row.fwd_scratch, &col.fwd, col.fwd_scratch);
        (a_buf, b_buf)
    };

    // Cross-power on the half-spectrum.
    cross_power_in_place(&mut buf_a, &buf_b);
    drop(buf_b);

    // Inverse 2-D FFT.
    fft_2d_pass(&mut buf_a, fw, fh, &row.inv, row.inv_scratch, &col.inv, col.inv_scratch);

    // Real part. rustfft does not normalise but we only care about
    // peak position, not magnitude.
    #[cfg(not(target_arch = "wasm32"))]
    {
        return buf_a.par_iter().map(|c| c.re).collect();
    }
    #[cfg(target_arch = "wasm32")]
    {
        buf_a.iter().map(|c| c.re).collect()
    }
}

fn pad_to_complex(src: &[f32], w: usize, h: usize, fw: usize, fh: usize) -> Vec<Complex<f32>> {
    let mut out = vec![Complex::new(0.0_f32, 0.0); fw * fh];

    #[cfg(not(target_arch = "wasm32"))]
    {
        out.par_chunks_mut(fw).enumerate().for_each(|(y, row)| {
            if y < h {
                let s = &src[y * w..(y + 1) * w];
                for x in 0..w {
                    row[x] = Complex::new(s[x], 0.0);
                }
            }
        });
        return out;
    }
    #[cfg(target_arch = "wasm32")]
    {
        for y in 0..h {
            for x in 0..w {
                out[y * fw + x] = Complex::new(src[y * w + x], 0.0);
            }
        }
        out
    }
}

/// `A[i] := normalize(A[i] · conj(B[i]))`. In-place on `a`.
fn cross_power_in_place(a: &mut [Complex<f32>], b: &[Complex<f32>]) {
    #[cfg(not(target_arch = "wasm32"))]
    {
        a.par_iter_mut().zip(b.par_iter()).for_each(|(av, bv)| {
            let prod = *av * bv.conj();
            let m = prod.norm();
            *av = if m > 1e-12 { prod / m } else { Complex::new(0.0, 0.0) };
        });
        return;
    }
    #[cfg(target_arch = "wasm32")]
    {
        for i in 0..a.len() {
            let prod = a[i] * b[i].conj();
            let m = prod.norm();
            a[i] = if m > 1e-12 { prod / m } else { Complex::new(0.0, 0.0) };
        }
    }
}

/// Run a 2-D FFT pass: row transforms, transpose, "row" transforms of
/// the transposed buffer, transpose back.
///
/// Going row → transpose → row → transpose runs both passes on
/// contiguous data, which is significantly faster than the previous
/// gather/scatter column-pass even before parallelisation. It also
/// makes the column pass naturally `par_chunks_mut`-able since each
/// row of the transposed buffer is independent.
fn fft_2d_pass(
    buf: &mut [Complex<f32>],
    fw: usize,
    fh: usize,
    row_plan: &Arc<dyn Fft<f32>>,
    row_scratch: usize,
    col_plan: &Arc<dyn Fft<f32>>,
    col_scratch: usize,
) {
    fft_rows_complex(buf, fw, row_plan, row_scratch);

    let mut t = vec![Complex::new(0.0_f32, 0.0); fw * fh];
    transpose_complex(buf, &mut t, fw, fh);

    fft_rows_complex(&mut t, fh, col_plan, col_scratch);

    transpose_complex(&t, buf, fh, fw);
}

/// In-place complex FFT (forward or inverse) over each contiguous
/// `row_len`-long slice of `buf`. Rows are independent so this is
/// naturally a `par_chunks_mut` over the buffer.
fn fft_rows_complex(
    buf: &mut [Complex<f32>],
    row_len: usize,
    plan: &Arc<dyn Fft<f32>>,
    scratch_len: usize,
) {
    #[cfg(not(target_arch = "wasm32"))]
    {
        // Each rayon worker keeps its own scratch — rustfft requires
        // that the scratch live as long as `process_with_scratch` is
        // running.
        buf.par_chunks_mut(row_len).for_each_init(
            || vec![Complex::new(0.0_f32, 0.0); scratch_len],
            |scratch, row| {
                plan.process_with_scratch(row, scratch);
            },
        );
        return;
    }
    #[cfg(target_arch = "wasm32")]
    {
        let mut scratch = vec![Complex::new(0.0_f32, 0.0); scratch_len];
        for row in buf.chunks_mut(row_len) {
            plan.process_with_scratch(row, &mut scratch);
        }
    }
}

/// Transpose a `(h × w)` complex matrix into a `(w × h)` complex matrix.
fn transpose_complex(src: &[Complex<f32>], dst: &mut [Complex<f32>], w: usize, h: usize) {
    #[cfg(not(target_arch = "wasm32"))]
    {
        dst.par_chunks_mut(h).enumerate().for_each(|(x, col)| {
            for y in 0..h {
                col[y] = src[y * w + x];
            }
        });
        return;
    }
    #[cfg(target_arch = "wasm32")]
    {
        for y in 0..h {
            for x in 0..w {
                dst[x * h + y] = src[y * w + x];
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Peak find + sub-pixel
// ---------------------------------------------------------------------------

fn find_peak(corr: &[f32], fw: usize, _fh: usize) -> (usize, usize, f32) {
    #[cfg(not(target_arch = "wasm32"))]
    {
        // Pixel-wide reduce. `enumerate()` over `par_iter()` yields
        // `(usize, &f32)`; we project to `(usize, f32)` so the
        // identity element and the reducer match cleanly.
        let (idx, val) = corr
            .par_iter()
            .copied()
            .enumerate()
            .reduce(
                || (0usize, f32::NEG_INFINITY),
                |a, b| if b.1 > a.1 { b } else { a },
            );
        return (idx % fw, idx / fw, val);
    }
    #[cfg(target_arch = "wasm32")]
    {
        let mut best = f32::NEG_INFINITY;
        let mut bi = 0usize;
        for (i, v) in corr.iter().enumerate() {
            if *v > best {
                best = *v;
                bi = i;
            }
        }
        (bi % fw, bi / fw, best)
    }
}

/// 3-point parabolic peak refinement around the integer peak.
/// For samples `c[-1], c[0], c[+1]` along an axis, the sub-pixel offset
/// of the parabola peak is `0.5·(c[-1] - c[+1]) / (c[-1] - 2·c[0] + c[+1])`,
/// clamped to ±1.
fn subpixel_parabola(corr: &[f32], fw: usize, fh: usize, px: usize, py: usize) -> (f32, f32) {
    let at = |x: usize, y: usize| corr[y * fw + x];
    // X axis (cyclic neighbours).
    let xm = (px + fw - 1) % fw;
    let xp = (px + 1) % fw;
    let cx_m = at(xm, py);
    let cx_0 = at(px, py);
    let cx_p = at(xp, py);
    let denom_x = cx_m - 2.0 * cx_0 + cx_p;
    let sub_x = if denom_x.abs() > 1e-12 {
        0.5 * (cx_m - cx_p) / denom_x
    } else {
        0.0
    };
    // Y axis.
    let ym = (py + fh - 1) % fh;
    let yp = (py + 1) % fh;
    let cy_m = at(px, ym);
    let cy_0 = at(px, py);
    let cy_p = at(px, yp);
    let denom_y = cy_m - 2.0 * cy_0 + cy_p;
    let sub_y = if denom_y.abs() > 1e-12 {
        0.5 * (cy_m - cy_p) / denom_y
    } else {
        0.0
    };
    (sub_x.clamp(-1.0, 1.0), sub_y.clamp(-1.0, 1.0))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn synth_field(w: u32, h: u32, stars: &[(f32, f32, f32)]) -> FramePixels {
        let mut rgba = vec![0u8; (w * h * 4) as usize];
        for y in 0..h {
            for x in 0..w {
                let mut v = 30.0_f32 + 20.0 * (y as f32 / h as f32);
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
    fn aligns_pure_translation() {
        let pos: Vec<(f32, f32, f32)> = vec![
            (60.0, 60.0, 200.0),
            (180.0, 100.0, 180.0),
            (250.0, 200.0, 150.0),
            (140.0, 220.0, 130.0),
            (90.0, 170.0, 140.0),
            (210.0, 170.0, 160.0),
        ];
        let a = synth_field(256, 256, &pos);
        let shifted: Vec<(f32, f32, f32)> = pos
            .iter()
            .map(|(x, y, p)| (*x - 7.0, *y - 4.0, *p))
            .collect();
        let b = synth_field(256, 256, &shifted);
        let r = align(&a, &b).unwrap();
        assert!((r.dx - 7.0).abs() < 0.5, "dx={}", r.dx);
        assert!((r.dy - 4.0).abs() < 0.5, "dy={}", r.dy);
    }

    #[test]
    fn aligns_handles_gradient() {
        // Same stars on top of dramatically different sky-glow
        // gradients — phase correlation should be insensitive.
        let pos: Vec<(f32, f32, f32)> = vec![
            (60.0, 60.0, 200.0),
            (180.0, 100.0, 180.0),
            (250.0, 200.0, 150.0),
            (140.0, 220.0, 130.0),
        ];
        let mut a = synth_field(256, 256, &pos);
        let mut b = synth_field(256, 256, &pos);
        for y in 0..256 {
            for x in 0..256 {
                let i = ((y * 256 + x) * 4) as usize;
                let bias_a = (x as f32 / 256.0 * 60.0) as u8;
                let bias_b = ((256 - x) as f32 / 256.0 * 60.0) as u8;
                a.rgba[i] = a.rgba[i].saturating_add(bias_a);
                a.rgba[i + 1] = a.rgba[i + 1].saturating_add(bias_a);
                a.rgba[i + 2] = a.rgba[i + 2].saturating_add(bias_a);
                b.rgba[i] = b.rgba[i].saturating_add(bias_b);
                b.rgba[i + 1] = b.rgba[i + 1].saturating_add(bias_b);
                b.rgba[i + 2] = b.rgba[i + 2].saturating_add(bias_b);
            }
        }
        let r = align(&a, &b).unwrap();
        assert!(r.dx.abs() < 0.5, "expected dx≈0, got {}", r.dx);
        assert!(r.dy.abs() < 0.5, "expected dy≈0, got {}", r.dy);
    }

    #[test]
    fn subpixel_accuracy() {
        let a = synth_field(256, 256, &[
            (100.0, 100.0, 200.0),
            (150.0, 60.0, 180.0),
            (200.0, 130.0, 150.0),
            (60.0, 190.0, 160.0),
        ]);
        let b = synth_field(256, 256, &[
            (100.0 - 3.4, 100.0 - 1.6, 200.0),
            (150.0 - 3.4, 60.0 - 1.6, 180.0),
            (200.0 - 3.4, 130.0 - 1.6, 150.0),
            (60.0 - 3.4, 190.0 - 1.6, 160.0),
        ]);
        let r = align(&a, &b).unwrap();
        assert!((r.dx - 3.4).abs() < 0.2, "dx={}", r.dx);
        assert!((r.dy - 1.6).abs() < 0.2, "dy={}", r.dy);
    }
}

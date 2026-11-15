/// Stacking pipeline — graph defaults and evaluator.
///
/// Two-tier evaluator:
///
/// 1. **Scrub pass** — synchronous, runs Stack at a downscaled
///    resolution (~480px wide) and Adjust on top. Always cheap enough
///    to complete inside an `update()` frame so the user sees instant
///    feedback while dragging an Adjust slider.
/// 2. **Full pass** — runs on a background thread at preview
///    resolution (~1440px); the result is delivered via mpsc and
///    swapped in on the next frame. Stack-output is cached keyed on
///    the input fingerprint so Adjust-only edits skip the slow
///    reduction altogether and just rerun Adjust on the cached image.
///
/// Job versioning: every dirty event bumps `state.stacking_preview
/// .job_version`; in-flight jobs carry the version they were spawned
/// with, and stale results are discarded.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use std::hash::{Hash, Hasher};

use crate::state::{
    AdjustNode, AppState, CollectionKind, DarkMeanCacheEntry, FramePixels, In,
    OutputNode, SourceNode, StackCacheEntry, StackJobKind, StackJobPayload, StackJobResult,
    StackMethod, StackNode, StackingEdge, StackingJobs, StackingNode, StackingNodeSlot,
};

/// Width threshold below which we already consider the buffer "scrub
/// resolution"; smaller frames don't get downscaled further.
const SCRUB_TARGET_W: u32 = 480;

const PREVIEW_TEXTURE_KEY: &str = "__stacking_preview__";

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

/// Seed a sane default graph if there isn't one yet and we have frames
/// to work with. Layout (left → right):
///
///   Source(Lights) ── Stack ── Adjust ── Output
///
/// The Source node binds to the first non-empty collection (preferring
/// kind=Lights). Adjust starts at neutral so the user immediately sees
/// the unaltered stack.
pub fn seed_default_graph_if_needed(state: &mut AppState) {
    // Re-seed when we go from "no collections" → "have a non-empty one"
    // so a fresh import gets a fresh chain.
    let any_non_empty = state.collections.iter().any(|c| !c.items.is_empty());
    if !any_non_empty {
        state.stacking_default_seeded = false;
        return;
    }
    if state.stacking_default_seeded || !state.stacking_nodes.is_empty() {
        return;
    }

    // Pick the collection to bind: first Lights with items, else first
    // collection with items.
    let bind_id = state
        .collections
        .iter()
        .find(|c| c.kind == CollectionKind::Lights && !c.items.is_empty())
        .or_else(|| state.collections.iter().find(|c| !c.items.is_empty()))
        .map(|c| c.id);

    let mut alloc_id = || {
        let id = state.stacking_next_node_id;
        state.stacking_next_node_id += 1;
        id
    };

    let src_id = alloc_id();
    let stack_id = alloc_id();
    let adjust_id = alloc_id();
    let output_id = alloc_id();

    let dy = 60.0;
    let x0 = 16.0;
    state.stacking_nodes.push(StackingNodeSlot {
        id: src_id,
        pos: egui::Pos2::new(x0, dy),
        node: StackingNode::Source(SourceNode { collection_id: bind_id }),
        measured_height: None,
    });
    state.stacking_nodes.push(StackingNodeSlot {
        id: stack_id,
        pos: egui::Pos2::new(x0 + 220.0, dy),
        node: StackingNode::Stack(StackNode {
            method: StackMethod::Mean,
            subtract_darks: true,
        }),
        measured_height: None,
    });
    state.stacking_nodes.push(StackingNodeSlot {
        id: adjust_id,
        pos: egui::Pos2::new(x0 + 440.0, dy),
        node: StackingNode::Adjust(AdjustNode::default()),
        measured_height: None,
    });
    state.stacking_nodes.push(StackingNodeSlot {
        id: output_id,
        pos: egui::Pos2::new(x0 + 660.0, dy),
        node: StackingNode::Output(OutputNode::default()),
        measured_height: None,
    });

    state.stacking_edges.push(StackingEdge {
        from: src_id, to: stack_id, to_port: In::Lights,
    });
    state.stacking_edges.push(StackingEdge {
        from: stack_id, to: adjust_id, to_port: In::Default,
    });
    state.stacking_edges.push(StackingEdge {
        from: adjust_id, to: output_id, to_port: In::Default,
    });

    // If there's also a Darks collection with items, drop in a second
    // Source bound to it and wire it to the Stack's Darks port.
    if let Some(darks) = state
        .collections
        .iter()
        .find(|c| c.kind == CollectionKind::Darks && !c.items.is_empty())
    {
        let darks_id = alloc_id();
        let darks_collection_id = darks.id;
        state.stacking_nodes.push(StackingNodeSlot {
            id: darks_id,
            pos: egui::Pos2::new(x0, dy + 90.0),
            node: StackingNode::Source(SourceNode {
                collection_id: Some(darks_collection_id),
            }),
            measured_height: None,
        });
        state.stacking_edges.push(StackingEdge {
            from: darks_id, to: stack_id, to_port: In::Darks,
        });
    }

    state.stacking_default_seeded = true;
    state.stacking_preview.dirty = true;
}

// ---------------------------------------------------------------------------
// Frame pixel capture
// ---------------------------------------------------------------------------

/// Stash a decoded thumbnail's raw RGBA buffer so the stacking evaluator
/// can read it without going back through the image cache (which only
/// keeps GPU textures). Called from the import message handler.
pub fn record_frame_pixels(state: &mut AppState, asset_id: &str, w: u32, h: u32, rgba: Vec<u8>) {
    state.stacking_preview.frame_pixels.insert(
        asset_id.to_string(),
        FramePixels { width: w, height: h, rgba },
    );
    // If this asset is part of any accepted collection item, restack.
    let used = state
        .collections
        .iter()
        .any(|c| c.items.iter().any(|it| it.asset_id == asset_id && it.accepted));
    if used {
        state.stacking_preview.dirty = true;
    }
}

// ---------------------------------------------------------------------------
// Graph evaluation
// ---------------------------------------------------------------------------

/// Evaluate the graph if `dirty`. Output goes into
/// `state.stacking_preview.output` and into the image_cache as a
/// special "preview" texture.
pub fn evaluate_if_dirty(state: &mut AppState, ctx: &egui::Context) {
    // Always pump background results, even if not dirty — a job from a
    // previous frame may have just landed.
    pump_background_results(state, ctx);

    if !state.stacking_preview.dirty {
        return;
    }
    state.stacking_preview.dirty = false;
    state.stacking_preview.error = None;

    if state.stacking_nodes.is_empty() {
        state.stacking_preview.output = None;
        return;
    }
    // Bump the version so any in-flight job that was spawned earlier
    // becomes stale.
    state.stacking_preview.job_version = state.stacking_preview.job_version.wrapping_add(1);

    // -- Synchronous "scrub" pass: run the whole pipeline at scrub
    //    resolution. This always finishes inside the frame so the user
    //    sees an immediate update while the high-res job grinds away
    //    in the background.
    match run_pipeline_scrub(state) {
        Ok(img) => {
            upload_preview_texture(state, ctx, &img);
            // Don't overwrite a real high-resolution `output` with the
            // scrub image — but if we don't have one yet, plant the
            // scrub image as a placeholder so the panel isn't empty.
            if state.stacking_preview.output.is_none() {
                state.stacking_preview.output = Some(img);
            }
        }
        Err(e) => {
            state.stacking_preview.error = Some(e);
        }
    }

    // -- Spawn the full-resolution job. If only Adjust changed since
    //    the last full run, the job will hit the stack-output cache
    //    inside the worker and finish almost instantly.
    spawn_full_job(state);
}

/// Drain any completed background jobs and apply the latest one whose
/// version still matches `state.stacking_preview.job_version`.
fn pump_background_results(state: &mut AppState, ctx: &egui::Context) {
    let Some(jobs) = state.stacking_jobs.as_ref() else { return };
    let mut latest_payload: Option<(u64, StackJobPayload)> = None;
    let mut latest_err: Option<(u64, String)> = None;
    while let Ok(msg) = jobs.rx.try_recv() {
        match msg.outcome {
            Ok(payload) => latest_payload = Some((msg.version, payload)),
            Err(e) => latest_err = Some((msg.version, e)),
        }
    }
    let current = state.stacking_preview.job_version;
    if let Some((v, e)) = latest_err {
        if v == current {
            state.stacking_preview.error = Some(e);
        }
    }
    if let Some((v, payload)) = latest_payload {
        if v == current {
            // Cache the pre-Adjust stack image and the dark master used
            // to produce it.
            state.stacking_preview.stack_cache = Some(StackCacheEntry {
                key: payload.stack_key,
                image: payload.stack_image.clone(),
            });
            let hot = payload.dark_mean.as_ref().map(|e| e.hot_pixels).unwrap_or(0);
            if let Some(entry) = payload.dark_mean {
                state.stacking_preview.dark_mean_cache = Some(entry);
            }
            // Run Adjust on top using the *current* params (the user
            // might have moved sliders since the job was spawned;
            // that's fine — Adjust is fast).
            let adjust = current_adjust_params(state);
            let final_img = match adjust {
                Some(a) => apply_adjust(&a, &payload.stack_image),
                None => payload.stack_image.clone(),
            };
            // Update sanity-check diagnostics with the full-resolution
            // numbers (overrides the scrub-pass diagnostics).
            state.stacking_preview.diagnostics = Some(crate::state::StackDiagnostics {
                lights: payload.lights_count,
                darks: payload.darks_count,
                hot_pixels: hot,
                width: payload.stack_image.width,
                height: payload.stack_image.height,
            });
            upload_preview_texture(state, ctx, &final_img);
            state.stacking_preview.output = Some(final_img);
            state.stacking_preview.error = None;
            state.add_log(
                "INFO",
                "stacking",
                &format!(
                    "stack ok: {}L+{}D, {} hot px, {}x{}",
                    payload.lights_count, payload.darks_count, hot,
                    payload.stack_image.width, payload.stack_image.height,
                ),
            );
        }
        // Job completed (whether we kept it or not); no longer running.
        state.stacking_preview.running = false;
    }
}

/// Spawn a background thread that re-evaluates the pipeline at full
/// preview resolution. Reuses the Stack-output cache when available.
fn spawn_full_job(state: &mut AppState) {
    if state.stacking_jobs.is_none() {
        state.stacking_jobs = Some(StackingJobs::new());
    }
    let tx = state.stacking_jobs.as_ref().unwrap().tx.clone();
    let version = state.stacking_preview.job_version;

    // Snapshot everything the worker needs. We deliberately clone
    // pixel buffers (cheap-ish on RGBA, single-shot) so the worker
    // doesn't need a borrow into `AppState` while it runs.
    let snapshot = match snapshot_for_full_job(state) {
        Some(s) => s,
        None => {
            // No work to do (graph too small or no inputs); just clear.
            state.stacking_preview.running = false;
            return;
        }
    };

    state.stacking_preview.running = true;

    let cache_hint = state.stacking_preview.stack_cache.clone();
    let dark_hint = state.stacking_preview.dark_mean_cache.clone();
    std::thread::spawn(move || {
        let result = match run_full_stack(snapshot, cache_hint, dark_hint) {
            Ok(payload) => StackJobResult {
                version,
                kind: StackJobKind::FullStack,
                outcome: Ok(payload),
            },
            Err(e) => StackJobResult {
                version,
                kind: StackJobKind::FullStack,
                outcome: Err(e),
            },
        };
        let _ = tx.send(result);
    });
}

/// What the worker thread needs to evaluate the pipeline. Built on
/// the main thread inside `spawn_full_job`.
struct FullJobSnapshot {
    /// Lights frames in source order (already alignment-shifted).
    lights: Vec<FramePixels>,
    /// Darks frames bag (already alignment-shifted).
    darks: Vec<FramePixels>,
    stack_method: StackMethod,
    subtract_darks: bool,
    /// Pre-computed fingerprint of the inputs + params; the worker
    /// just embeds it in the result so the main thread can update its
    /// cache without re-computing.
    stack_key: u64,
}

fn snapshot_for_full_job(state: &AppState) -> Option<FullJobSnapshot> {
    let stack_node = state
        .stacking_nodes
        .iter()
        .find_map(|slot| match &slot.node {
            StackingNode::Stack(s) => Some(s.clone()),
            _ => None,
        })?;

    let (lights, darks) = collect_lights_and_darks(state, /*scrub=*/ false);
    if lights.is_empty() {
        return None;
    }

    let stack_key = compute_stack_key(state, &stack_node, /*scrub=*/ false);

    Some(FullJobSnapshot {
        lights,
        darks,
        stack_method: stack_node.method,
        subtract_darks: stack_node.subtract_darks,
        stack_key,
    })
}

/// Worker-thread-side evaluator. Runs the Stack reduction (or pulls
/// from cache hint), returns the pre-Adjust stack image. Adjust is
/// applied later by the main thread so live slider edits don't
/// invalidate the result.
fn run_full_stack(
    s: FullJobSnapshot,
    cache_hint: Option<StackCacheEntry>,
    dark_hint: Option<DarkMeanCacheEntry>,
) -> Result<StackJobPayload, String> {
    if let Some(c) = cache_hint {
        if c.key == s.stack_key {
            // Stack-output cache hit — Adjust-only edit. Don't replay
            // the dark master either; just forward the existing one.
            return Ok(StackJobPayload {
                stack_image: c.image,
                stack_key: s.stack_key,
                dark_mean: dark_hint,
                lights_count: s.lights.len(),
                darks_count: s.darks.len(),
            });
        }
    }
    let lights_count = s.lights.len();
    let darks_count = s.darks.len();
    let (image, dark_mean) = stack_lights_with_darks(
        &s.lights,
        &s.darks,
        s.stack_method,
        s.subtract_darks,
        dark_hint.as_ref(),
    )?;
    Ok(StackJobPayload {
        stack_image: image,
        stack_key: s.stack_key,
        dark_mean,
        lights_count,
        darks_count,
    })
}

/// Synchronous low-resolution evaluation: build/lookup scrub-resolution
/// frames, run the pipeline at that resolution, return the final
/// (Adjust-applied) image.
fn run_pipeline_scrub(state: &mut AppState) -> Result<FramePixels, String> {
    // Make sure the scrub-resolution mirror is populated for every
    // frame we're about to consume.
    ensure_scrub_frames(state);

    let stack_node = state
        .stacking_nodes
        .iter()
        .find_map(|slot| match &slot.node {
            StackingNode::Stack(s) => Some(s.clone()),
            _ => None,
        })
        .ok_or_else(|| "no Stack node".to_string())?;
    let adjust = current_adjust_params(state);

    let (lights, darks) = collect_lights_and_darks(state, /*scrub=*/ true);
    if lights.is_empty() {
        return Err("Stack: no light frames".to_string());
    }
    let lights_count = lights.len();
    let darks_count = darks.len();

    let scrub_key = compute_stack_key(state, &stack_node, /*scrub=*/ true);
    let (stack_image, hot_pixels) = match &state.stacking_preview.stack_cache_low {
        Some(c) if c.key == scrub_key => {
            let hp = state
                .stacking_preview
                .dark_mean_cache_low
                .as_ref()
                .map(|e| e.hot_pixels)
                .unwrap_or(0);
            (c.image.clone(), hp)
        }
        _ => {
            let cached_dark = state.stacking_preview.dark_mean_cache_low.as_ref();
            let (img, dark_entry) = stack_lights_with_darks(
                &lights,
                &darks,
                stack_node.method,
                stack_node.subtract_darks,
                cached_dark,
            )?;
            state.stacking_preview.stack_cache_low =
                Some(StackCacheEntry { key: scrub_key, image: img.clone() });
            let hp = dark_entry.as_ref().map(|e| e.hot_pixels).unwrap_or(0);
            if let Some(entry) = dark_entry {
                state.stacking_preview.dark_mean_cache_low = Some(entry);
            }
            (img, hp)
        }
    };

    // Update diagnostics (overridden by the high-res job when it lands).
    state.stacking_preview.diagnostics = Some(crate::state::StackDiagnostics {
        lights: lights_count,
        darks: darks_count,
        hot_pixels,
        width: stack_image.width,
        height: stack_image.height,
    });

    let final_image = match adjust {
        Some(a) => apply_adjust(&a, &stack_image),
        None => stack_image,
    };
    Ok(final_image)
}

/// Collect light + dark FramePixels from the active Source nodes, with
/// alignment offsets applied. `scrub=true` reads from the downscaled
/// mirror (`frame_pixels_low`), otherwise from the full preview-size
/// buffers (`frame_pixels`).
fn collect_lights_and_darks(state: &AppState, scrub: bool) -> (Vec<FramePixels>, Vec<FramePixels>) {
    let mut lights = Vec::new();
    let mut darks = Vec::new();
    let scale_factor = if scrub {
        // Worst case: the source frames may not have an entry in
        // `frame_pixels_low` if `ensure_scrub_frames` hasn't run; in
        // that case we silently skip them on the scrub path.
        Some(())
    } else {
        None
    };
    let _ = scale_factor;

    // Walk the graph: each Source contributes to the bag of the port
    // its outgoing edge feeds into on a Stack node.
    for slot in &state.stacking_nodes {
        let StackingNode::Source(s) = &slot.node else { continue };
        let Some(collection_id) = s.collection_id else { continue };
        let Some(collection) = state.collection(collection_id) else { continue };
        let alignment_table = state.alignments.get(&collection_id);

        // Determine which Stack input port this Source feeds (default = lights).
        let target_port = state
            .stacking_edges
            .iter()
            .find(|e| {
                e.from == slot.id
                    && state
                        .stacking_nodes
                        .iter()
                        .any(|n| n.id == e.to && matches!(n.node, StackingNode::Stack(_)))
            })
            .map(|e| e.to_port)
            .unwrap_or(In::Default);

        for asset_id in collection.accepted_ids() {
            let frame = if scrub {
                state.stacking_preview.frame_pixels_low.get(asset_id).cloned()
            } else {
                state.stacking_preview.frame_pixels.get(asset_id).cloned()
            };
            let Some(mut frame) = frame else { continue };

            // Apply alignment. For scrub-res frames we have to scale
            // the offsets down by the same factor we downscaled the
            // pixel buffer.
            let mut offset = alignment_table
                .and_then(|a| a.offsets.get(asset_id).copied())
                .unwrap_or_default();
            if scrub {
                if let Some(full) = state.stacking_preview.frame_pixels.get(asset_id) {
                    if full.width > 0 {
                        let scale = frame.width as f32 / full.width as f32;
                        offset.dx *= scale;
                        offset.dy *= scale;
                    }
                }
            }
            // Translation OR rotation — go through the rotate-aware
            // path when theta is non-trivial. The fast row-memcpy
            // path inside `shift_frame` handles theta == 0.
            let needs_translate = offset.dx.abs() >= 0.5 || offset.dy.abs() >= 0.5;
            let needs_rotate = offset.theta.abs() > 1e-4;
            if needs_translate || needs_rotate {
                frame = if needs_rotate {
                    shift_rotate_frame(&frame, offset.dx, offset.dy, offset.theta)
                } else {
                    shift_frame(&frame, offset.dx, offset.dy)
                };
            }

            match target_port {
                In::Darks => darks.push(frame),
                _ => lights.push(frame),
            }
        }
    }

    (lights, darks)
}

/// Build / refresh the scrub-resolution mirror. We keep it lazy:
/// every full-size frame that doesn't have a scrub-size sibling gets
/// downscaled now. Frames that already have a sibling are left alone
/// so we don't redo the work each frame.
fn ensure_scrub_frames(state: &mut AppState) {
    let to_make: Vec<String> = state
        .stacking_preview
        .frame_pixels
        .iter()
        .filter(|(id, _)| !state.stacking_preview.frame_pixels_low.contains_key(id.as_str()))
        .map(|(id, _)| id.clone())
        .collect();

    for id in to_make {
        let Some(full) = state.stacking_preview.frame_pixels.get(&id).cloned() else { continue };
        let low = downscale(&full, SCRUB_TARGET_W);
        state.stacking_preview.frame_pixels_low.insert(id, low);
    }
}

/// Box-filter downscale to roughly `target_w` pixels wide. Cheap and
/// good enough for the scrub preview — sub-pixel quality doesn't
/// matter, the user's looking at lighting / colour, not detail.
fn downscale(src: &FramePixels, target_w: u32) -> FramePixels {
    if src.width <= target_w {
        return src.clone();
    }
    // Nearest-power-of-two decimation factor: simple and fast.
    let factor = (src.width / target_w).max(1);
    let new_w = src.width / factor;
    let new_h = src.height / factor;
    let mut rgba = vec![0u8; (new_w * new_h * 4) as usize];
    for y in 0..new_h {
        for x in 0..new_w {
            let sx = x * factor;
            let sy = y * factor;
            let si = ((sy * src.width + sx) * 4) as usize;
            let di = ((y * new_w + x) * 4) as usize;
            rgba[di] = src.rgba[si];
            rgba[di + 1] = src.rgba[si + 1];
            rgba[di + 2] = src.rgba[si + 2];
            rgba[di + 3] = src.rgba[si + 3];
        }
    }
    FramePixels { width: new_w, height: new_h, rgba }
}

/// Look up the (single) Adjust node's params, if any. We assume a
/// single Adjust node — the eval pipeline is currently linear, this
/// is the same assumption the rest of the system makes.
fn current_adjust_params(state: &AppState) -> Option<AdjustNode> {
    state.stacking_nodes.iter().find_map(|slot| match &slot.node {
        StackingNode::Adjust(a) => Some(a.clone()),
        _ => None,
    })
}

/// Hash everything that determines the Stack output: which inputs feed
/// in (asset id + alignment offset), the Stack params, and the dark
/// fingerprint. Adjust params are deliberately *not* in the key.
fn compute_stack_key(state: &AppState, stack: &StackNode, scrub: bool) -> u64 {
    let mut hasher = std::collections::hash_map::DefaultHasher::new();
    scrub.hash(&mut hasher);
    (stack.method as u8).hash(&mut hasher);
    stack.subtract_darks.hash(&mut hasher);

    // Walk Source nodes the way `collect_lights_and_darks` does.
    for slot in &state.stacking_nodes {
        let StackingNode::Source(s) = &slot.node else { continue };
        let Some(collection_id) = s.collection_id else { continue };
        collection_id.hash(&mut hasher);
        let Some(collection) = state.collection(collection_id) else { continue };
        let alignment_table = state.alignments.get(&collection_id);
        let port = state
            .stacking_edges
            .iter()
            .find(|e| {
                e.from == slot.id
                    && state
                        .stacking_nodes
                        .iter()
                        .any(|n| n.id == e.to && matches!(n.node, StackingNode::Stack(_)))
            })
            .map(|e| e.to_port)
            .unwrap_or(In::Default);
        format!("{port:?}").hash(&mut hasher);

        for asset_id in collection.accepted_ids() {
            asset_id.hash(&mut hasher);
            if let Some(a) = alignment_table.and_then(|a| a.offsets.get(asset_id)) {
                (a.dx.to_bits()).hash(&mut hasher);
                (a.dy.to_bits()).hash(&mut hasher);
                (a.theta.to_bits()).hash(&mut hasher);
            } else {
                0u32.hash(&mut hasher);
            }
        }
    }

    hasher.finish()
}

/// Common stack core used by both the synchronous scrub path and the
/// async full path. Caller is responsible for slot collection,
/// alignment, and (optionally) supplying a precomputed dark master so
/// repeated stack runs over the same darks don't redo the reduction.
fn stack_lights_with_darks(
    lights: &[FramePixels],
    darks: &[FramePixels],
    method: StackMethod,
    subtract_darks: bool,
    cached_dark_mean: Option<&DarkMeanCacheEntry>,
) -> Result<(FramePixels, Option<DarkMeanCacheEntry>), String> {
    if lights.is_empty() {
        return Err("Stack: no light frames on input".to_string());
    }
    let w = lights[0].width;
    let h = lights[0].height;
    let n_pixels = (w as usize) * (h as usize);

    // Build (or reuse) the dark master. The returned `Option<entry>` is
    // forwarded to the caller so it can refresh its cache.
    let (dark_mean, mut dark_entry): (Option<Vec<f32>>, Option<DarkMeanCacheEntry>) =
        if subtract_darks && !darks.is_empty() {
            let dark_key = compute_dark_key(darks, w, h);
            if let Some(entry) = cached_dark_mean
                && entry.key == dark_key
                && entry.width == w
                && entry.height == h
            {
                (Some(entry.mean.clone()), Some(entry.clone()))
            } else {
                let usable: Vec<&FramePixels> =
                    darks.iter().filter(|f| f.width == w && f.height == h).collect();
                if usable.is_empty() {
                    return Err("Stack: no dark frames matched the canonical size".to_string());
                }
                let n = usable.len() as f32;
                let mut acc = vec![0f32; n_pixels * 4];
                for f in &usable {
                    for i in 0..(n_pixels * 4) {
                        acc[i] += f.rgba[i] as f32;
                    }
                }
                for v in acc.iter_mut() {
                    *v /= n;
                }
                let entry = DarkMeanCacheEntry {
                    key: dark_key,
                    width: w,
                    height: h,
                    mean: acc.clone(),
                    hot_pixels: 0,
                };
                (Some(acc), Some(entry))
            }
        } else {
            (None, None)
        };

    // Dark subtraction strategy (hot-pixel only, not blanket subtract):
    //
    //  1. Compute global stats of the dark master (per-channel mean +
    //     stddev), giving us a baseline for "what's a normal dark
    //     pixel" vs "what's an outlier hot pixel".
    //  2. Tag every pixel where dark[i] - global_floor[c] > k·σ_c as
    //     "hot". `k=4` keeps the false-positive rate low.
    //  3. Light frames pass through unchanged at non-hot pixels. At
    //     hot pixels we subtract `dark[i] - global_floor[c]` so the
    //     hot reading is replaced by the noise-floor reading at that
    //     pixel — preserves overall signal while killing the hot
    //     spikes that lens-cap-on darks were taken to identify.
    //
    // We also stash the hot-pixel count on the entry so the UI can
    // surface it as a sanity-check.
    let lights_corrected: Vec<FramePixels> = match &dark_mean {
        Some(dark) => {
            let stats = dark_global_stats(dark, n_pixels);
            let hot_mask = build_hot_mask(dark, n_pixels, &stats, /*k=*/ 4.0);
            let hot_count = hot_mask.iter().filter(|b| **b).count();
            // Side effect: stash hot count on the entry we'll return
            // (caller pushes it into state for the UI). We do this
            // here to keep the math self-contained.
            if let Some(entry) = dark_entry.as_mut() {
                entry.hot_pixels = hot_count;
            }
            lights
                .iter()
                .filter(|f| f.width == w && f.height == h)
                .map(|f| {
                    let mut rgba = f.rgba.clone();
                    for p in 0..n_pixels {
                        if !hot_mask[p] {
                            continue;
                        }
                        for c in 0..3 {
                            let i = p * 4 + c;
                            let correction = (dark[i] - stats.floor[c]).max(0.0);
                            let v = rgba[i] as f32 - correction;
                            rgba[i] = v.clamp(0.0, 255.0) as u8;
                        }
                    }
                    FramePixels { width: w, height: h, rgba }
                })
                .collect()
        }
        None => lights.iter().filter(|f| f.width == w && f.height == h).cloned().collect(),
    };
    if lights_corrected.is_empty() {
        return Err("Stack: no frames matched the canonical size".to_string());
    }
    let usable: Vec<&FramePixels> = lights_corrected.iter().collect();

    let out_rgba = match method {
        StackMethod::Mean => stack_mean(&usable, n_pixels),
        StackMethod::Median => stack_median(&usable, n_pixels),
        StackMethod::Max => stack_max(&usable, n_pixels),
    };
    Ok((FramePixels { width: w, height: h, rgba: out_rgba }, dark_entry))
}

/// Stable hash of the dark frames' contents and canonical size. Two
/// runs with the same darks at the same resolution produce the same
/// key — so the dark-master is reusable across method/alignment edits
/// on the lights side.

/// Per-channel global stats of a dark master. `floor[c]` is a robust
/// estimate of the noise baseline (mean of pixels at-or-below the
/// channel mean) and `sigma[c]` is the per-channel stddev. Hot-pixel
/// detection treats anything sitting `> floor + k·sigma` as an outlier.
struct DarkStats {
    floor: [f32; 3],
    sigma: [f32; 3],
}

fn dark_global_stats(dark: &[f32], n_pixels: usize) -> DarkStats {
    let mut floor = [0f32; 3];
    let mut sigma = [0f32; 3];
    for c in 0..3 {
        let mut sum = 0.0_f64;
        for p in 0..n_pixels {
            sum += dark[p * 4 + c] as f64;
        }
        let mean = (sum / n_pixels as f64) as f32;
        let mut var = 0.0_f64;
        for p in 0..n_pixels {
            let d = dark[p * 4 + c] - mean;
            var += (d * d) as f64;
        }
        sigma[c] = ((var / n_pixels as f64).sqrt()) as f32;
        let mut below_sum = 0.0_f64;
        let mut below_n = 0usize;
        for p in 0..n_pixels {
            let v = dark[p * 4 + c];
            if v <= mean {
                below_sum += v as f64;
                below_n += 1;
            }
        }
        floor[c] = if below_n == 0 { mean } else { (below_sum / below_n as f64) as f32 };
    }
    DarkStats { floor, sigma }
}

fn build_hot_mask(dark: &[f32], n_pixels: usize, stats: &DarkStats, k: f32) -> Vec<bool> {
    let mut mask = vec![false; n_pixels];
    for p in 0..n_pixels {
        for c in 0..3 {
            let v = dark[p * 4 + c];
            let threshold = stats.floor[c] + k * stats.sigma[c].max(1.0);
            if v > threshold {
                mask[p] = true;
                break;
            }
        }
    }
    mask
}

fn compute_dark_key(darks: &[FramePixels], w: u32, h: u32) -> u64 {
    let mut hasher = std::collections::hash_map::DefaultHasher::new();
    w.hash(&mut hasher);
    h.hash(&mut hasher);
    for f in darks.iter().filter(|f| f.width == w && f.height == h) {
        // Hash the buffer pointer-equivalents: width, height, length,
        // and a coarse content fingerprint (first/middle/last bytes).
        // The full pixel contents are stable per asset for the lifetime
        // of the import, so a sparse content sample plus length is
        // enough to discriminate without scanning megabytes.
        f.width.hash(&mut hasher);
        f.height.hash(&mut hasher);
        f.rgba.len().hash(&mut hasher);
        if !f.rgba.is_empty() {
            f.rgba[0].hash(&mut hasher);
            f.rgba[f.rgba.len() / 2].hash(&mut hasher);
            f.rgba[f.rgba.len() - 1].hash(&mut hasher);
        }
    }
    hasher.finish()
}

/// Apply Adjust params to an image — pure CPU pass per pixel. Works in
/// place if we owned the buffer; we copy so callers can keep the
/// pre-Adjust image around (that's the whole point of caching).
fn apply_adjust(a: &AdjustNode, img: &FramePixels) -> FramePixels {
    let mut out = img.rgba.clone();
    let exp_mul = (2f32).powf(a.exposure);
    let inv_gamma = 1.0 / a.gamma.max(0.05);
    let sat = a.saturation;
    let hue_rad = a.hue_shift.to_radians();
    let cos_h = hue_rad.cos();
    let sin_h = hue_rad.sin();

    for px in out.chunks_exact_mut(4) {
        let r = px[0] as f32 / 255.0;
        let g = px[1] as f32 / 255.0;
        let b = px[2] as f32 / 255.0;

        let r = (r * exp_mul).clamp(0.0, 1.0);
        let g = (g * exp_mul).clamp(0.0, 1.0);
        let b = (b * exp_mul).clamp(0.0, 1.0);

        let y = 0.299 * r + 0.587 * g + 0.114 * b;
        let r = (y + sat * (r - y)).clamp(0.0, 1.0);
        let g = (y + sat * (g - y)).clamp(0.0, 1.0);
        let b = (y + sat * (b - y)).clamp(0.0, 1.0);

        let i_c = 0.596 * r - 0.275 * g - 0.321 * b;
        let q_c = 0.212 * r - 0.523 * g + 0.311 * b;
        let i_r = i_c * cos_h - q_c * sin_h;
        let q_r = i_c * sin_h + q_c * cos_h;
        let r2 = (y + 0.956 * i_r + 0.621 * q_r).clamp(0.0, 1.0);
        let g2 = (y - 0.272 * i_r - 0.647 * q_r).clamp(0.0, 1.0);
        let b2 = (y - 1.106 * i_r + 1.703 * q_r).clamp(0.0, 1.0);

        let r2 = r2.powf(inv_gamma);
        let g2 = g2.powf(inv_gamma);
        let b2 = b2.powf(inv_gamma);

        px[0] = (r2 * 255.0).round() as u8;
        px[1] = (g2 * 255.0).round() as u8;
        px[2] = (b2 * 255.0).round() as u8;
    }

    FramePixels { width: img.width, height: img.height, rgba: out }
}

/// Translate the pixel buffer by (`dx`, `dy`). Out-of-bounds areas are
/// left transparent black so they don't bias the stack.
///
/// The hot path computes the overlap rectangle once and copies each
/// destination row with `copy_from_slice` (lowered to `memcpy`),
/// instead of touching each byte individually. A no-op shift returns
/// a clone of the source.
fn shift_frame(src: &FramePixels, dx: f32, dy: f32) -> FramePixels {
    let w = src.width as i32;
    let h = src.height as i32;
    let dx = dx.round() as i32;
    let dy = dy.round() as i32;

    if dx == 0 && dy == 0 {
        return src.clone();
    }

    let stride = (w * 4) as usize;
    let mut out = vec![0u8; (w * h) as usize * 4];

    // Overlap rectangle in destination coordinates.
    let dy_start = dy.max(0);
    let dy_end = (h + dy).min(h);
    let dx_start = dx.max(0);
    let dx_end = (w + dx).min(w);
    if dy_start >= dy_end || dx_start >= dx_end {
        return FramePixels { width: src.width, height: src.height, rgba: out };
    }

    let copy_bytes = ((dx_end - dx_start) * 4) as usize;
    for y in dy_start..dy_end {
        let sy = y - dy;
        let sx = dx_start - dx;
        let dst_off = (y as usize) * stride + (dx_start as usize) * 4;
        let src_off = (sy as usize) * stride + (sx as usize) * 4;
        out[dst_off..dst_off + copy_bytes]
            .copy_from_slice(&src.rgba[src_off..src_off + copy_bytes]);
        for px in out[dst_off..dst_off + copy_bytes].chunks_exact_mut(4) {
            px[3] = 255;
        }
    }
    FramePixels { width: src.width, height: src.height, rgba: out }
}

/// Translate + rotate around the image centre using bilinear sampling.
/// Slower than `shift_frame` but only invoked when `theta` is
/// meaningful, so the common pure-translation case stays on the
/// memcpy path. Out-of-bounds pixels are left transparent.
fn shift_rotate_frame(src: &FramePixels, dx: f32, dy: f32, theta: f32) -> FramePixels {
    let w = src.width as i32;
    let h = src.height as i32;
    let cx = (w as f32 - 1.0) * 0.5;
    let cy = (h as f32 - 1.0) * 0.5;
    let cos_t = (-theta).cos();
    let sin_t = (-theta).sin();
    let mut out = vec![0u8; (w * h) as usize * 4];
    let stride = (w * 4) as usize;
    for y in 0..h {
        for x in 0..w {
            // Inverse map: dest (x,y) → source by undoing translate
            // then rotation around centre.
            let tx = x as f32 - dx - cx;
            let ty = y as f32 - dy - cy;
            let sx = cos_t * tx - sin_t * ty + cx;
            let sy = sin_t * tx + cos_t * ty + cy;
            if sx < 0.0 || sy < 0.0 || sx > (w - 1) as f32 || sy > (h - 1) as f32 {
                continue;
            }
            let x0 = sx.floor() as i32;
            let y0 = sy.floor() as i32;
            let fx = sx - x0 as f32;
            let fy = sy - y0 as f32;
            let x1 = (x0 + 1).min(w - 1);
            let y1 = (y0 + 1).min(h - 1);
            let dst = (y as usize) * stride + (x as usize) * 4;
            for c in 0..3 {
                let p00 = src.rgba[(y0 as usize) * stride + (x0 as usize) * 4 + c] as f32;
                let p10 = src.rgba[(y0 as usize) * stride + (x1 as usize) * 4 + c] as f32;
                let p01 = src.rgba[(y1 as usize) * stride + (x0 as usize) * 4 + c] as f32;
                let p11 = src.rgba[(y1 as usize) * stride + (x1 as usize) * 4 + c] as f32;
                let v = (1.0 - fx) * (1.0 - fy) * p00
                    + fx * (1.0 - fy) * p10
                    + (1.0 - fx) * fy * p01
                    + fx * fy * p11;
                out[dst + c] = v.clamp(0.0, 255.0) as u8;
            }
            out[dst + 3] = 255;
        }
    }
    FramePixels { width: src.width, height: src.height, rgba: out }
}

fn stack_mean(frames: &[&FramePixels], n_pixels: usize) -> Vec<u8> {
    // Accumulate in u32 to avoid overflow, then divide.
    let mut acc = vec![0u32; n_pixels * 4];
    for f in frames {
        for i in 0..(n_pixels * 4) {
            acc[i] += f.rgba[i] as u32;
        }
    }
    let n = frames.len() as u32;
    acc.into_iter().map(|x| (x / n) as u8).collect()
}

fn stack_max(frames: &[&FramePixels], n_pixels: usize) -> Vec<u8> {
    let mut out = vec![0u8; n_pixels * 4];
    for f in frames {
        for i in 0..(n_pixels * 4) {
            if f.rgba[i] > out[i] {
                out[i] = f.rgba[i];
            }
        }
    }
    // Force alpha to opaque — max alpha across opaque-ish JPEG frames is
    // already 255, but this nails it for safety.
    for p in 0..n_pixels {
        out[p * 4 + 3] = 255;
    }
    out
}

fn stack_median(frames: &[&FramePixels], n_pixels: usize) -> Vec<u8> {
    // Counting-sort over u8: O(m) per pixel/channel instead of O(m log m).
    // For m frames the median is the smallest value v such that the running
    // count of buckets [0..=v] exceeds m/2.
    let m = frames.len();
    let target = m / 2;
    let mut out = vec![0u8; n_pixels * 4];
    let mut hist = [0u32; 256];
    for c in 0..3 {
        for p in 0..n_pixels {
            hist.fill(0);
            for f in frames {
                hist[f.rgba[p * 4 + c] as usize] += 1;
            }
            let mut count: u32 = 0;
            for (val, &cnt) in hist.iter().enumerate() {
                count += cnt;
                if count as usize > target {
                    out[p * 4 + c] = val as u8;
                    break;
                }
            }
        }
    }
    for p in 0..n_pixels {
        out[p * 4 + 3] = 255;
    }
    out
}

// ---------------------------------------------------------------------------
// Texture upload
// ---------------------------------------------------------------------------

fn upload_preview_texture(state: &mut AppState, ctx: &egui::Context, img: &FramePixels) {
    let color_image = egui::ColorImage::from_rgba_unmultiplied(
        [img.width as usize, img.height as usize],
        &img.rgba,
    );
    let texture = ctx.load_texture(PREVIEW_TEXTURE_KEY, color_image, egui::TextureOptions::LINEAR);
    state.image_cache.insert(PREVIEW_TEXTURE_KEY.to_string(), texture);
}

/// Asset id used by the image viewer to display the stacking preview.
pub fn preview_asset_id() -> &'static str {
    PREVIEW_TEXTURE_KEY
}

// ---------------------------------------------------------------------------
// Output: save PNG
// ---------------------------------------------------------------------------

/// Encode the current preview as PNG and try to save it. Native: writes
/// to `~/Pictures/<filename>` if absolute path not given. WASM: triggers
/// a browser download via a data URL.
pub fn save_output(state: &mut AppState, filename: &str) {
    let img = match &state.stacking_preview.output {
        Some(img) => img.clone(),
        None => {
            state.add_log("WARN", "stacking", "Output: no preview to save yet — press Stack first.");
            return;
        }
    };

    let mut bytes: Vec<u8> = Vec::new();
    {
        let encoder = image::codecs::png::PngEncoder::new(&mut bytes);
        if let Err(e) = image::ImageEncoder::write_image(
            encoder,
            &img.rgba,
            img.width,
            img.height,
            image::ExtendedColorType::Rgba8,
        ) {
            state.add_log("ERROR", "stacking", &format!("PNG encode failed: {e}"));
            return;
        }
    }

    #[cfg(not(target_arch = "wasm32"))]
    {
        let path = if std::path::Path::new(filename).is_absolute() {
            std::path::PathBuf::from(filename)
        } else {
            let base = dirs_pictures().unwrap_or_else(|| std::path::PathBuf::from("."));
            base.join(filename)
        };
        if let Some(parent) = path.parent() {
            let _ = std::fs::create_dir_all(parent);
        }
        match std::fs::write(&path, &bytes) {
            Ok(_) => state.add_log("INFO", "stacking", &format!("Saved {}", path.display())),
            Err(e) => state.add_log("ERROR", "stacking", &format!("Save failed: {e}")),
        }
    }
    #[cfg(target_arch = "wasm32")]
    {
        use wasm_bindgen::JsCast;
        // Trigger a browser download by creating an <a> with a data: URL.
        let b64 = base64_encode(&bytes);
        let data_url = format!("data:image/png;base64,{b64}");
        if let Some(window) = web_sys::window() {
            if let Some(document) = window.document() {
                if let Ok(a) = document.create_element("a") {
                    let _ = a.set_attribute("href", &data_url);
                    let _ = a.set_attribute("download", filename);
                    if let Ok(html) = a.dyn_into::<web_sys::HtmlElement>() {
                        html.click();
                    }
                }
            }
        }
        state.add_log("INFO", "stacking", &format!("Triggered download: {filename}"));
    }
}

#[cfg(not(target_arch = "wasm32"))]
fn dirs_pictures() -> Option<std::path::PathBuf> {
    // Cheap: $HOME/Pictures (macOS + Linux), $USERPROFILE/Pictures on
    // Windows. We avoid the `dirs` crate to keep the dep tree small.
    if let Ok(home) = std::env::var("HOME") {
        return Some(std::path::PathBuf::from(home).join("Pictures"));
    }
    std::env::var("USERPROFILE").ok().map(|h| std::path::PathBuf::from(h).join("Pictures"))
}

#[cfg(target_arch = "wasm32")]
fn base64_encode(bytes: &[u8]) -> String {
    use wasm_bindgen::JsCast;
    // btoa wants a "binary string" (each char a byte 0..255).
    let s: String = bytes.iter().map(|&b| char::from(b)).collect();
    web_sys::window()
        .and_then(|w| w.btoa(&s).ok())
        .unwrap_or_default()
}

# Stacking pipeline

Last reviewed: 2026-05-01 | Review frequency: monthly

The stacking pipeline is a small DAG of typed nodes evaluated on the
cached preview-size pixel buffers. Two-tier evaluation gives instant
feedback during interactive edits and a higher-quality result soon
after.

Source: `crates/amalthea-app/src/stacking.rs` +
`crates/amalthea-app/src/panels/stacking_editor.rs`.

## Node kinds

```rust
enum StackingNode {
    Source(SourceNode),     // collection_id: Option<u64>
    Stack(StackNode),       // method: Mean|Median|Max, subtract_darks: bool
    Adjust(AdjustNode),     // exposure, gamma, saturation, hue_shift
    Output(OutputNode),     // filename: String
}
```

Edges carry a port: `enum In { Default, Lights, Darks }`. The Stack
node has two input ports; everything else has one.

The default chain `Source(Lights) → Stack(mean, subtract_darks=true)
→ Adjust(neutral) → Output("stacked.png")` is auto-seeded by
`seed_default_graph_if_needed` whenever a non-empty collection exists
and no graph is wired up yet. If a non-empty `Darks` collection is
also present, a second Source bound to it is dropped onto the Stack's
Darks port.

## Evaluator (two-tier, async)

```
dirty event
    │
    ├─ scrub pass (sync, in update())
    │     ├─ uses frame_pixels_low (~480px)
    │     ├─ checks stack_cache_low → reuse if key matches
    │     └─ apply Adjust → upload preview texture (instant)
    │
    └─ spawn full job (background thread)
          ├─ snapshot inputs + alignments
          ├─ compute stack_key
          ├─ checks cache_hint → reuse if key matches
          ├─ stack at full resolution (slow part)
          └─ send StackJobResult { stack_image, dark_mean, … }
```

`evaluate_if_dirty` always pumps `pump_background_results` first to
swap in any landed full-res result. When dirty, it bumps
`job_version` (stale jobs check this and self-discard), runs the
scrub pass, and spawns a fresh full job.

### Caches (3 layers)

| cache | scope | invalidation key |
|---|---|---|
| `stack_cache` (full) / `stack_cache_low` (scrub) | post-Stack image | inputs ⊕ method ⊕ subtract_darks ⊕ alignments |
| `dark_mean_cache` (full) / `_low` (scrub) | per-pixel dark master + hot count | dark inputs ⊕ size |
| `frame_pixels_low` | downscaled frame mirror | populated lazily, kept until clear |

When the user only edits Adjust params, the Stack key doesn't change
→ the slow reduction is skipped entirely, only `apply_adjust` runs.
This is what makes adjust sliders feel snappy.

### Snapshot for the worker

`snapshot_for_full_job` clones into `FullJobSnapshot { lights, darks,
stack_method, subtract_darks, stack_key }` so the worker thread owns
its inputs and the main thread is free to keep mutating `AppState`.

### Job versioning

`stacking_preview.job_version` is bumped on every dirty event. Each
spawned job carries the version at spawn time. `pump_background_results`
ignores any result whose version doesn't match the current version —
that's how we cancel in-flight work without true cancellation
plumbing.

## Dark-frame logic (hot-pixel only, not blanket subtract)

Naïve approach: subtract dark mean from every light. On 8-bit JPEG
previews this drives the entire image to zero because most "dark"
pixel readings are baseline noise + light's actual signal.

Current approach in `stack_lights_with_darks`:

1. Build the per-pixel dark master `dark[i]` (already cached).
2. `dark_global_stats(dark)` produces per-channel `floor` (mean of
   pixels at-or-below the channel mean — robust against the hot
   tail) and `sigma` (channel stddev).
3. `build_hot_mask(dark, stats, k=4)` flags any pixel whose dark
   reading on any channel exceeds `floor + k·σ`.
4. Lights pass through unchanged at non-hot pixels. At hot pixels we
   subtract `dark[i] - floor[c]` so the hot reading is replaced by
   the noise-floor reading at that pixel.

`hot_pixels: usize` is recorded on `DarkMeanCacheEntry` and surfaced
in:
- `Stack` node body: `12L · 8D · 137 hot · 1440×960`
- log line on every successful eval

## Alignment integration

`collect_lights_and_darks` walks Source nodes, looks up
`state.alignments[collection_id].offsets[asset_id]`, and shifts each
frame by its `Alignment` before adding it to the lights / darks bag.
At scrub resolution we scale offsets by the same factor we
downscaled the frame buffer.

The Stack key includes per-frame alignment, so editing the Δx/Δy
slider in the Alignment Viewer correctly invalidates the cache.

## Output node: save PNG

`Output { filename }`. Save button (in the node body or the editor
header) calls `stacking::save_output(state, filename)`:

- Native: `~/Pictures/<filename>` if relative, else absolute.
- WASM: data-URL `<a download>` click via `web_sys`.

## Cross-references

- `docs/state.md` — `StackingPreview`, `StackJobResult`, etc.
- `docs/alignment.md` — how per-frame `Alignment` flows in
- `docs/ui-conventions.md` — gestures in the node editor

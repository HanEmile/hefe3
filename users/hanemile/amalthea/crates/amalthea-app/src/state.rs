/// Central application state shared across all panels.
///
/// Holds the currently selected asset, search results, zoom/pan state for the
/// image viewer, authentication state, configuration, and caches for
/// thumbnails and full-resolution images.

use std::collections::HashSet;
use std::sync::mpsc;

use crate::auth::AuthState;
use crate::cache::LruCache;
use crate::config::AppConfig;
use crate::immich::AssetResponse;
use crate::theme::Theme;

/// Messages received from background import tasks.
pub enum ImportMessage {
    /// Album metadata arrived — populate the timeline immediately.
    AssetsLoaded(Vec<AssetResponse>),
    /// A single thumbnail has been fetched.
    ThumbnailReady { asset_id: String, data: Vec<u8> },
    /// A preview-size full image has been fetched for the image viewer.
    ImageReady { asset_id: String, data: Vec<u8> },
    /// Import status update.
    Status(String),
    /// Import error.
    Error(String),
}

/// Source of share-link-authenticated assets.
///
/// Captured at import time so subsequent on-demand fetches (full-resolution
/// previews) can reuse the same Immich base URL and share key.
#[derive(Clone, Default)]
pub struct ShareSource {
    pub immich_base: String,
    pub key: String,
}

// ---------------------------------------------------------------------------
// Caches
// ---------------------------------------------------------------------------

/// Maximum number of thumbnails held in memory.
const MAX_THUMBNAIL_ENTRIES: usize = 2000;

/// Maximum number of full-resolution images held in memory.
///
/// At ~1 MB average per decoded texture this keeps the image cache around
/// 500 MB.
const MAX_IMAGE_ENTRIES: usize = 500;

/// Cache for decoded thumbnail textures, keyed by asset ID.
pub struct ThumbnailCache {
    inner: LruCache<String, egui::TextureHandle>,
}

impl ThumbnailCache {
    /// Create an empty thumbnail cache.
    pub fn new() -> Self {
        Self {
            inner: LruCache::new(MAX_THUMBNAIL_ENTRIES),
        }
    }

    /// Insert a thumbnail texture.
    pub fn insert(&mut self, asset_id: String, texture: egui::TextureHandle) {
        self.inner.insert(asset_id, texture);
    }

    /// Retrieve a thumbnail, promoting it in the LRU order.
    pub fn get(&mut self, asset_id: &str) -> Option<&egui::TextureHandle> {
        self.inner.get(&asset_id.to_string())
    }

    /// Retrieve without promoting (read-only borrow).
    pub fn peek(&self, asset_id: &str) -> Option<&egui::TextureHandle> {
        self.inner.peek(&asset_id.to_string())
    }

    /// Check whether a thumbnail is cached (without promoting).
    pub fn contains(&self, asset_id: &str) -> bool {
        self.inner.contains(&asset_id.to_string())
    }

    /// Number of cached thumbnails.
    pub fn len(&self) -> usize {
        self.inner.len()
    }

    /// Returns `true` if the cache is empty.
    pub fn is_empty(&self) -> bool {
        self.inner.is_empty()
    }

    /// Drop all cached thumbnails.
    pub fn clear(&mut self) {
        self.inner.clear();
    }
}

impl Default for ThumbnailCache {
    fn default() -> Self {
        Self::new()
    }
}

/// Cache for decoded full-resolution image textures, keyed by asset ID.
pub struct ImageCache {
    inner: LruCache<String, egui::TextureHandle>,
}

impl ImageCache {
    /// Create an empty image cache.
    pub fn new() -> Self {
        Self {
            inner: LruCache::new(MAX_IMAGE_ENTRIES),
        }
    }

    /// Insert an image texture.
    pub fn insert(&mut self, asset_id: String, texture: egui::TextureHandle) {
        self.inner.insert(asset_id, texture);
    }

    /// Retrieve an image, promoting it in the LRU order.
    pub fn get(&mut self, asset_id: &str) -> Option<&egui::TextureHandle> {
        self.inner.get(&asset_id.to_string())
    }

    /// Check whether an image is cached (without promoting).
    pub fn contains(&self, asset_id: &str) -> bool {
        self.inner.contains(&asset_id.to_string())
    }

    /// Number of cached images.
    pub fn len(&self) -> usize {
        self.inner.len()
    }

    /// Returns `true` if the cache is empty.
    pub fn is_empty(&self) -> bool {
        self.inner.is_empty()
    }

    /// Drop all cached images.
    pub fn clear(&mut self) {
        self.inner.clear();
    }
}

impl Default for ImageCache {
    fn default() -> Self {
        Self::new()
    }
}

// ---------------------------------------------------------------------------
// Pending requests tracker
// ---------------------------------------------------------------------------

/// Tracks in-flight HTTP requests to prevent duplicate fetches.
///
/// Before issuing a request, check `is_pending(key)`. If `false`, call
/// `mark_pending(key)` and fire the request. When the response arrives
/// (success or failure), call `mark_done(key)`.
pub struct PendingRequests {
    keys: HashSet<String>,
}

impl PendingRequests {
    /// Create an empty tracker.
    pub fn new() -> Self {
        Self {
            keys: HashSet::new(),
        }
    }

    /// Returns `true` if a request for `key` is already in flight.
    pub fn is_pending(&self, key: &str) -> bool {
        self.keys.contains(key)
    }

    /// Mark a request as in-flight.
    pub fn mark_pending(&mut self, key: &str) {
        self.keys.insert(key.to_string());
    }

    /// Mark a request as completed (removes it from the pending set).
    pub fn mark_done(&mut self, key: &str) {
        self.keys.remove(key);
    }

    /// Clear all pending markers.
    pub fn clear(&mut self) {
        self.keys.clear();
    }
}

impl Default for PendingRequests {
    fn default() -> Self {
        Self::new()
    }
}

// ---------------------------------------------------------------------------
// Application state
// ---------------------------------------------------------------------------

/// Shared mutable state passed to every panel during rendering.
pub struct AppState {
    /// The asset currently displayed in the image viewer / inspected in the
    /// EXIF panel.
    pub selected_asset: Option<AssetResponse>,

    /// Assets returned by the most recent search or album listing.
    pub search_results: Vec<AssetResponse>,

    /// Current zoom level in the image viewer (1.0 = fit-to-panel).
    pub image_zoom: f32,

    /// Pan offset in the image viewer (pixels).
    pub image_offset: egui::Vec2,

    /// Authentication state.
    pub auth: AuthState,

    /// Application configuration.
    pub config: AppConfig,

    /// LRU cache of decoded thumbnail textures.
    pub thumbnail_cache: ThumbnailCache,

    /// LRU cache of decoded full-resolution image textures.
    pub image_cache: ImageCache,

    /// Tracker for in-flight HTTP requests.
    pub pending_requests: PendingRequests,

    /// Cached histogram data for the currently selected image.
    pub histogram_data: Option<crate::panels::histogram::HistogramData>,

    /// Centralized log entries (ring buffer, max 1000 entries).
    pub log_entries: Vec<LogEntry>,

    /// Active log level filters for the log viewer.
    pub log_level_filter: Vec<String>,

    /// Named, typed groups of asset IDs. Source nodes reference one by id.
    /// Seeded with empty Lights / Darks groups on first use.
    pub collections: Vec<Collection>,
    /// Monotonic id allocator for new collections.
    pub next_collection_id: u64,
    /// Last-used collection in the timeline "Add to..." menu — pre-selected
    /// for the next batch.
    pub last_collection_id: Option<u64>,

    /// Nodes in the stacking pipeline graph (node editor).
    pub stacking_nodes: Vec<StackingNodeSlot>,
    /// Edges connecting nodes in the stacking pipeline graph.
    pub stacking_edges: Vec<StackingEdge>,
    /// Monotonic id allocator for new graph nodes.
    pub stacking_next_node_id: u64,
    /// Currently dragged node (in node-editor space) and grab offset.
    pub stacking_drag: Option<(u64, egui::Vec2)>,
    /// In-progress connection drag: source node id + last pointer pos.
    pub stacking_pending_edge: Option<(u64, egui::Pos2)>,
    /// Set of nodes currently selected in the editor (for box-select +
    /// multi-drag). The last single-clicked node is always in here.
    pub stacking_selection: HashSet<u64>,
    /// In-progress box-select rectangle (graph-local coords). `None` =
    /// not currently dragging out a selection rect.
    pub stacking_box_drag: Option<BoxDrag>,
    /// Pan offset for the node editor canvas (graph-local origin in
    /// screen space). Updated by middle-drag.
    pub stacking_pan: egui::Vec2,
    /// Zoom factor for the node editor canvas. 1.0 = native pixels.
    pub stacking_zoom: f32,
    /// Source node id whose group preview popup is currently open
    /// (clicking the node header opens it; clicking outside or pressing
    /// Esc closes it).
    pub stacking_open_popup: Option<u64>,
    /// Which Source node, if any, was last assigned a fresh-import
    /// auto-binding to the most recently filled collection. Used so we
    /// only auto-bind once per session.
    pub stacking_autobound_collection: Option<u64>,
    /// Cached intermediates and the final preview from the last graph run.
    pub stacking_preview: StackingPreview,
    /// Whether we've seeded the default Source→Stack→Adjust→Output graph
    /// for the current set of frames. Reset whenever `stacking_frames`
    /// goes from empty → non-empty.
    pub stacking_default_seeded: bool,

    // -- Trip planner fields --

    /// Observer location name.
    pub trip_location_name: String,
    /// Observer latitude (-90..90).
    pub trip_lat: f64,
    /// Observer longitude (-180..180).
    pub trip_lon: f64,
    /// Target object name.
    pub trip_target_name: String,
    /// Target RA in degrees (0..360).
    pub trip_target_ra: f32,
    /// Target Dec in degrees (-90..90).
    pub trip_target_dec: f32,
    /// Telescope focal length in mm.
    pub trip_focal_length: f32,
    /// Telescope aperture (f-number).
    pub trip_aperture: f32,
    /// Camera pixel size in micrometers.
    pub trip_pixel_size: f32,

    // -- Sky viewer fields --

    /// Sky viewer: center RA in degrees (0..360) — used for equirectangular fallback.
    pub sky_center_ra: f32,
    /// Sky viewer: center Dec in degrees (-90..90).
    pub sky_center_dec: f32,
    /// Sky viewer: zoom level (1.0 = default).
    pub sky_zoom: f32,
    /// Sky viewer: observation hour (0.0..24.0 UTC).
    pub sky_hour: f64,
    /// Sky viewer: observation year.
    pub sky_year: i32,
    /// Sky viewer: observation month (1..12).
    pub sky_month: u32,
    /// Sky viewer: observation day (1..31).
    pub sky_day: u32,
    /// Sky viewer: sync zoom across multiple sky viewer panels.
    pub sky_sync_zoom: bool,
    /// Per-panel zoom overrides (when sync is off). Keyed by PanelId.
    pub sky_panel_zooms: std::collections::HashMap<u64, f32>,

    /// Channel for receiving messages from background import tasks.
    pub import_rx: Option<mpsc::Receiver<ImportMessage>>,
    /// Sender retained so on-demand fetches can deliver into the same channel.
    pub import_tx: Option<mpsc::Sender<ImportMessage>>,
    /// Number of thumbnails loaded so far (for progress display).
    pub thumbnails_loaded: usize,
    /// Total thumbnails expected.
    pub thumbnails_total: usize,

    /// Origin of the currently imported share link (Immich base URL + share key).
    pub share_source: ShareSource,
    /// Asset IDs for which a full-image fetch is already in-flight or completed.
    pub requested_images: HashSet<String>,

    /// Active theme. Mutated live by the settings panel and re-applied
    /// to the egui context every frame.
    pub theme: Theme,

    /// Whether the theme has unsaved local changes (the settings panel
    /// pushes them to the backend on the next idle frame).
    pub theme_dirty: bool,

    /// Whether the persistence layer (lib.rs) has loaded the theme from
    /// the backend yet. Skipped on WASM builds.
    pub theme_loaded: bool,

    /// Per-image-viewer pick (keyed on PanelId.0). Lets two image
    /// viewer panels show different images side-by-side.
    pub image_viewer_picks: std::collections::HashMap<u64, ImageViewerPick>,

    /// Per-collection alignment metadata, keyed by collection id.
    pub alignments: std::collections::HashMap<u64, CollectionAlignment>,
    /// UI state for the alignment viewer panel.
    pub alignment_ui: AlignmentUi,
    /// UI state for the star tracker panel.
    pub star_tracker_ui: StarTrackerUi,

    /// Background-job channel for hi-res stacking. Set up lazily on the
    /// first dirty event; receives `StackJobResult`s.
    pub stacking_jobs: Option<StackingJobs>,

    /// Background-job channel for the auto-align worker.
    pub align_jobs: Option<AlignJobs>,
    /// True while an auto-align job is currently running. Surfaced in
    /// the alignment viewer header.
    pub align_running: bool,
    /// Last status message from the auto-align worker.
    pub align_status: Option<String>,

    /// Multi-selection in the timeline (asset IDs). The image viewer continues
    /// to use `selected_asset` (the most recently clicked / "primary" asset);
    /// batch operations use this set.
    pub timeline_selection: HashSet<String>,
    /// Anchor for shift-click range selection in the timeline.
    pub timeline_anchor: Option<String>,

    /// Collection currently being inline-renamed in the gallery sidebar.
    pub gallery_renaming: Option<u64>,
    /// Buffer holding the in-progress rename text.
    pub gallery_rename_buffer: String,

    /// Asset IDs whose alignment reference-point should be drawn as a
    /// crosshair in image-viewer panels. Toggled from the Stacking
    /// Inspector sidebar.
    pub alignment_overlay_show: HashSet<String>,
    /// Stacking-inspector sidebar's currently selected collection
    /// (drives which frames the sidebar lists).
    pub inspector_collection_id: Option<u64>,
}

/// A log entry for the centralized log viewer.
pub struct LogEntry {
    pub timestamp: String,
    pub level: String,
    pub source: String,
    pub message: String,
}

/// One asset in a collection, with an accept/reject toggle.
#[derive(Debug, Clone)]
pub struct CollectionItem {
    pub asset_id: String,
    pub filename: String,
    pub accepted: bool,
}

/// What a collection is for. Used to pre-fill the right Source/Darks
/// hookup in the node editor and to colour the chip in the timeline.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CollectionKind {
    /// "Light" frames — the actual signal photos.
    Lights,
    /// Black frames (lens cap on) — sensor noise reference.
    Darks,
    /// Bias frames (zero-exposure dark) — read-noise reference.
    Bias,
    /// Frames that are themselves the result of a previous stack.
    Stacked,
    /// Anything else.
    Custom,
}

impl CollectionKind {
    pub fn label(&self) -> &'static str {
        match self {
            Self::Lights => "Lights",
            Self::Darks => "Darks",
            Self::Bias => "Bias",
            Self::Stacked => "Stacked",
            Self::Custom => "Custom",
        }
    }
    pub const ALL: &[CollectionKind] = &[
        CollectionKind::Lights,
        CollectionKind::Darks,
        CollectionKind::Bias,
        CollectionKind::Stacked,
        CollectionKind::Custom,
    ];
}

/// Where a collection's items came from. Drives the "Sync" UI in the
/// gallery sidebar — Manual collections have no remote, ImmichAlbum
/// collections can be re-fetched from the same shared link.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CollectionSource {
    /// User-curated: items added manually from the gallery.
    Manual,
    /// Bound to an Immich shared link / album. The album_id and key
    /// are captured at sync time so we can re-pull on demand.
    ImmichAlbum {
        album_id: String,
        share_key: String,
    },
}

impl Default for CollectionSource {
    fn default() -> Self {
        Self::Manual
    }
}

/// A named, typed group of asset IDs. Source nodes in the stacking
/// graph reference one by id.
#[derive(Debug, Clone)]
pub struct Collection {
    pub id: u64,
    pub name: String,
    pub kind: CollectionKind,
    pub items: Vec<CollectionItem>,
    /// Where the items came from. Defaults to Manual so existing flows
    /// keep working without touching this field.
    pub source: CollectionSource,
}

impl Collection {
    /// Iter accepted asset IDs.
    pub fn accepted_ids(&self) -> impl Iterator<Item = &str> {
        self.items
            .iter()
            .filter(|it| it.accepted)
            .map(|it| it.asset_id.as_str())
    }
}

// ---------------------------------------------------------------------------
// Stacking graph (Blender-style nodes)
// ---------------------------------------------------------------------------
//
// Nodes carry typed parameters. The pipeline currently runs on the cached
// preview-size JPEGs (RGBA, ~1440px) so it stays interactive — no raw
// processing path yet. Each node kind has its own struct of params; the
// `StackingNode` enum bundles them.

/// Pixel methods for combining frames into a single image.
#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub enum StackMethod {
    /// Per-pixel arithmetic mean.
    Mean,
    /// Per-pixel median (rejects outliers; slower).
    Median,
    /// Per-pixel maximum (good for star trails).
    Max,
}

impl StackMethod {
    pub fn label(&self) -> &'static str {
        match self {
            Self::Mean => "mean",
            Self::Median => "median",
            Self::Max => "max",
        }
    }
}

/// Per-node parameter blocks. Kept separate per kind so the editor can
/// render different widgets per node body.
#[derive(Debug, Clone)]
pub enum StackingNode {
    /// Group of light frames. By default sources every accepted frame in
    /// `state.stacking_frames`; clearing `selection` falls back to
    /// "all accepted".
    Source(SourceNode),
    /// Combine frames into a single image.
    Stack(StackNode),
    /// Adjust hue / saturation / exposure / gamma on a single image.
    Adjust(AdjustNode),
    /// Sink — exposes a save button + filename.
    Output(OutputNode),
}

#[derive(Debug, Clone)]
pub struct SourceNode {
    /// Collection this Source draws from. `None` = no group bound yet.
    pub collection_id: Option<u64>,
}

#[derive(Debug, Clone)]
pub struct StackNode {
    pub method: StackMethod,
    /// Subtract a per-pixel mean of any frames received on the "darks"
    /// input port from each light before reducing.
    pub subtract_darks: bool,
}

#[derive(Debug, Clone)]
pub struct AdjustNode {
    /// Stops; multiplied as 2^exposure on linear RGB.
    pub exposure: f32,
    /// Gamma exponent applied to each channel (>1 brightens midtones).
    pub gamma: f32,
    /// 0 = grayscale, 1 = original, >1 = boosted.
    pub saturation: f32,
    /// Hue rotation in degrees.
    pub hue_shift: f32,
}

impl Default for AdjustNode {
    fn default() -> Self {
        Self { exposure: 0.0, gamma: 1.0, saturation: 1.0, hue_shift: 0.0 }
    }
}

#[derive(Debug, Clone)]
pub struct OutputNode {
    pub filename: String,
}

impl Default for OutputNode {
    fn default() -> Self {
        Self { filename: "stacked.png".to_string() }
    }
}

impl StackingNode {
    /// Short label shown in the title bar.
    pub fn label(&self) -> &'static str {
        match self {
            Self::Source(_) => "Source",
            Self::Stack(_) => "Stack",
            Self::Adjust(_) => "Adjust",
            Self::Output(_) => "Output",
        }
    }

    /// Subtitle shown under the title — surfaces the most useful param.
    pub fn summary(&self) -> String {
        match self {
            Self::Source(s) => match s.collection_id {
                Some(_) => "bound".to_string(),
                None => "(no group)".to_string(),
            },
            Self::Stack(s) => s.method.label().to_string(),
            Self::Adjust(a) => format!(
                "ev{:+.1} γ{:.2}",
                a.exposure, a.gamma,
            ),
            Self::Output(o) => o.filename.clone(),
        }
    }
}

/// One slot in the graph: position + node payload + measured height.
#[derive(Debug, Clone)]
pub struct StackingNodeSlot {
    pub id: u64,
    /// Editor display position in panel-local coordinates.
    pub pos: egui::Pos2,
    pub node: StackingNode,
    /// Height (in panel-local pixels) needed to display the node body
    /// without clipping. Updated on each render from the egui measurement
    /// pass; the next frame uses the new value. `None` = use the kind's
    /// fallback height.
    pub measured_height: Option<f32>,
}

/// Which input port on the destination node this edge feeds.
///
/// Most nodes only have one input — those use `In::Default`. The Stack
/// node has two: `In::Lights` (top, the signal frames to combine) and
/// `In::Darks` (bottom, the calibration frames whose mean is subtracted
/// before reduction).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum In {
    Default,
    Lights,
    Darks,
}

/// A directed connection between two stacking nodes (`from` → `to`).
#[derive(Debug, Clone, Copy)]
pub struct StackingEdge {
    pub from: u64,
    pub to: u64,
    pub to_port: In,
}

/// In-progress box selection / slice in the node editor. Both ends
/// in graph-local coordinates so the gesture stays put while panning.
#[derive(Debug, Clone, Copy)]
pub struct BoxDrag {
    pub start: egui::Pos2,
    pub current: egui::Pos2,
    /// Whether to add to the existing selection (Shift held) or replace.
    pub additive: bool,
    /// True when the gesture is a "slice" (Shift held at drag start) —
    /// any edge the slice line crosses gets deleted on release.
    pub slice: bool,
}

/// Per-panel state for image-viewer instances. Each viewer panel keeps
/// its own pick — that's how we get side-by-side comparison.
#[derive(Debug, Clone)]
pub enum ImageViewerPick {
    /// Show whatever the global "primary" asset is (default behaviour
    /// before per-panel state was added).
    Primary,
    /// Show the live stacking preview.
    StackingPreview,
    /// Show a specific asset by id.
    Asset(String),
}

impl Default for ImageViewerPick {
    fn default() -> Self {
        Self::Primary
    }
}

/// Cached intermediate / final results of a graph evaluation.
#[derive(Default)]
pub struct StackingPreview {
    /// Per-frame raw RGBA buffers at preview resolution (~1440px),
    /// keyed by asset id. Populated alongside the egui texture.
    pub frame_pixels: std::collections::HashMap<String, FramePixels>,
    /// Same frames as above, downscaled to ~480px wide. Used by the
    /// scrub-resolution evaluator that runs synchronously every frame
    /// while a slider is being dragged so the user gets instant
    /// feedback. Populated lazily on first scrub need.
    pub frame_pixels_low: std::collections::HashMap<String, FramePixels>,

    /// Per-frame quality scores (focus, star count, FWHM). Populated
    /// once per frame on first need by `quality::ensure_scored`. Used
    /// by the alignment viewer's "top X%" slider to flip the
    /// `accepted` bit on poor frames.
    pub frame_quality: std::collections::HashMap<String, FrameQuality>,
    /// Per-frame UTC timestamp parsed from EXIF
    /// (`exif_info.date_time_original`) at import time, falling back
    /// to `file_created_at`. Cached as `chrono::DateTime` so the
    /// rotation cross-check doesn't reparse on every job.
    pub frame_timestamps: std::collections::HashMap<String, chrono::DateTime<chrono::Utc>>,

    /// Last successful preview output (RGBA, premultiplied=false).
    /// This is the *high-resolution* result — what gets saved.
    pub output: Option<FramePixels>,

    /// Cached output of the last Stack reduction (per-input fingerprint
    /// + method + dark-subtract). When only Adjust params change, the
    /// evaluator skips the slow reduction and re-runs Adjust on this
    /// image instead.
    pub stack_cache: Option<StackCacheEntry>,
    /// Same idea at scrub resolution.
    pub stack_cache_low: Option<StackCacheEntry>,

    /// Cached dark-master (per-pixel mean of the dark frames) keyed by
    /// the dark inputs' fingerprint. Reused across light-only edits so
    /// dragging a stack-method toggle or alignment doesn't redo the
    /// dark reduction.
    pub dark_mean_cache: Option<DarkMeanCacheEntry>,
    /// Same idea at scrub resolution.
    pub dark_mean_cache_low: Option<DarkMeanCacheEntry>,

    /// True while a background full-resolution job is running. The UI
    /// renders a "stacking…" badge over the preview when this is set.
    pub running: bool,
    /// True if a parameter changed since the last run; the update loop
    /// will re-evaluate on the next frame.
    pub dirty: bool,
    /// Last error from the evaluator, if any.
    pub error: Option<String>,

    /// Monotonic version of the most recent dirty event. Bumped each
    /// time something flips `dirty`; the background worker checks the
    /// counter to decide whether its result is still relevant.
    pub job_version: u64,

    /// Diagnostics from the last successful evaluation — surfaced in
    /// the Stack node body and the editor header bar so the user can
    /// see at a glance how many lights / darks went in and how many
    /// pixels were flagged hot.
    pub diagnostics: Option<StackDiagnostics>,
}

#[derive(Debug, Clone, Default)]
pub struct StackDiagnostics {
    pub lights: usize,
    pub darks: usize,
    pub hot_pixels: usize,
    pub width: u32,
    pub height: u32,
}

/// Cached output of a Stack reduction along with the fingerprint that
/// produced it. Used so Adjust-only edits skip the slow per-frame
/// reduction.
#[derive(Clone)]
pub struct StackCacheEntry {
    /// Stable hash of (input asset ids, alignments, stack method,
    /// subtract_darks toggle, dark-frame fingerprint). Mismatch → drop.
    pub key: u64,
    pub image: FramePixels,
}

/// Cached dark-master (per-pixel mean of dark frames as f32 RGBA) keyed
/// by the dark inputs' fingerprint. The buffer is `width * height * 4`
/// floats long; storing as f32 avoids re-quantising on each light frame.
#[derive(Clone)]
pub struct DarkMeanCacheEntry {
    /// Stable hash of (dark asset ids, alignments, canonical width/height).
    pub key: u64,
    pub width: u32,
    pub height: u32,
    pub mean: Vec<f32>,
    /// Number of pixels flagged as "hot" by the last hot-pixel pass.
    /// Surfaced in the Stack node body as a sanity check.
    pub hot_pixels: usize,
}

/// Result message from a background stacking job back to the main
/// thread.
pub struct StackJobResult {
    /// `state.stacking_preview.job_version` at the time the job was
    /// spawned. The UI ignores results whose version is older than the
    /// current one — the user must have changed something since.
    pub version: u64,
    pub kind: StackJobKind,
    pub outcome: Result<StackJobPayload, String>,
}

pub enum StackJobKind {
    /// Full Stack reduction at preview resolution.
    FullStack,
}

pub struct StackJobPayload {
    /// Pre-Adjust image (i.e. the Stack-node output). The main thread
    /// will run Adjust on this and store the result as `output`.
    pub stack_image: FramePixels,
    /// Stable hash that produced the stack image. The main thread
    /// stores this in `stack_cache` so future Adjust-only edits skip
    /// the reduction.
    pub stack_key: u64,
    /// The dark-master used for this run, returned so the main thread
    /// can refresh `dark_mean_cache` and skip the reduction next time.
    pub dark_mean: Option<DarkMeanCacheEntry>,
    /// How many lights / darks went in. Drives the diagnostics text.
    pub lights_count: usize,
    pub darks_count: usize,
}

/// Background stacking-job plumbing. Created lazily on the first
/// dirty event; survives across runs. The receiver is checked every
/// frame so completed jobs swap their result in.
pub struct StackingJobs {
    pub tx: std::sync::mpsc::Sender<StackJobResult>,
    pub rx: std::sync::mpsc::Receiver<StackJobResult>,
}

impl StackingJobs {
    pub fn new() -> Self {
        let (tx, rx) = std::sync::mpsc::channel();
        Self { tx, rx }
    }
}

/// Result of an auto-align job. Each entry is one (collection, asset)
/// → offset assignment that the main thread merges into
/// `state.alignments`.
pub struct AlignJobResult {
    pub collection_id: u64,
    pub offsets: Vec<(String, Alignment)>,
    /// Per-star trail across the collection's accepted frames, in
    /// detection order (brightest in reference first). The star
    /// tracker panel reads this back; the rotation verifier also
    /// uses it to predict drift.
    pub star_trails: Vec<TrackedStar>,
    /// Auto-discovered image-pixel-to-sky-axis orientation, if the
    /// verifier had enough signal to solve for it. `None` ⇒ keep the
    /// previous orientation (or fall back to identity).
    pub orientation: Option<ImageOrientation>,
    /// The reference frame the worker actually used (auto-picked or
    /// honoured an explicit request). Persisted into
    /// `CollectionAlignment.reference` so subsequent runs match.
    pub chosen_reference: Option<String>,
    /// One-line summary the alignment viewer surfaces in its header.
    pub status: String,
}

pub struct AlignJobs {
    pub tx: std::sync::mpsc::Sender<AlignJobResult>,
    pub rx: std::sync::mpsc::Receiver<AlignJobResult>,
}

impl AlignJobs {
    pub fn new() -> Self {
        let (tx, rx) = std::sync::mpsc::channel();
        Self { tx, rx }
    }
}

/// A decoded RGBA8 buffer.
#[derive(Clone)]
pub struct FramePixels {
    pub width: u32,
    pub height: u32,
    pub rgba: Vec<u8>,
}

/// Per-frame alignment relative to a reference frame, applied when
/// stacking. Positive `dx` shifts the frame to the right before the
/// reduction; positive `dy` shifts it down. Values are in preview-size
/// pixels (the same buffer the stacker runs on).
///
/// `theta` is the in-plane rotation in radians, applied around the
/// image centre. Old code paths that only set `dx`/`dy` keep working
/// because the default is 0 and the stacker's `shift_frame` falls
/// back to the fast row-memcpy path when `theta == 0`.
///
/// `anchor` is the (x, y) point in the frame's own pixel space that
/// the auto-align job locked onto — typically the brightest star /
/// feature. Drawn as a crosshair in the image viewer when the
/// stacking-inspector "show ref point" toggle is on.
///
/// `confidence` is a 0..1 score from the auto-align solver — the
/// fraction of detected stars that ended up as inliers in the
/// least-squares fit. Surfaced in the alignment viewer; consumed by
/// the verifier to weight per-frame residuals.
#[derive(Debug, Clone, Copy, Default)]
pub struct Alignment {
    pub dx: f32,
    pub dy: f32,
    pub theta: f32,
    pub anchor: Option<(u32, u32)>,
    pub confidence: f32,
}

/// Per-collection alignment metadata. The reference frame stays at the
/// origin; every other frame's `Alignment` is its offset from there.
#[derive(Debug, Clone, Default)]
pub struct CollectionAlignment {
    /// Asset id of the frame that defines the origin. `None` = first
    /// accepted frame.
    pub reference: Option<String>,
    /// Per-asset offsets.
    pub offsets: std::collections::HashMap<String, Alignment>,
    /// How image-pixel axes map to sky-coordinate axes for this
    /// collection. Discovered automatically by
    /// `align_verify::auto_calibrate_orientation` from a single frame
    /// pair, then reused for the rotation-rate cross-check on every
    /// other pair. `None` until calibration has run.
    pub orientation: Option<ImageOrientation>,
    /// Per-frame star detections in image-pixel coordinates of each
    /// frame's own buffer (NOT shifted by the alignment). Populated by
    /// the auto-align worker; the star tracker panel reads this back.
    pub star_trails: Vec<TrackedStar>,
}

/// Mapping from image-pixel axes to sky-coordinate axes. The 8 valid
/// values cover {flip-x?} × {flip-y?} × {transpose?} — the symmetry
/// group of axis-aligned reflections / 90° rotations. Anything else
/// (a 30° camera tilt, say) is rolled into `Alignment::theta` per
/// frame, not into the orientation.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct ImageOrientation {
    /// If true, +RA on sky maps to -x in image (mirror flip).
    pub flip_x: bool,
    /// If true, +Dec on sky maps to -y in image.
    pub flip_y: bool,
    /// If true, swap (x, y) — i.e. the camera was rotated 90°.
    pub transpose: bool,
}

/// A single star tracked across the frames of a collection. Indices
/// into `frames` line up with the auto-align worker's input order
/// (which is the collection's accepted-items order at job-start time).
/// Missing detections in a given frame are encoded as `None`.
#[derive(Debug, Clone)]
pub struct TrackedStar {
    /// Stable id within the collection. 0 is the brightest at the
    /// reference frame.
    pub id: u32,
    /// Star's (x, y) in the *reference* frame, sub-pixel.
    pub ref_x: f32,
    pub ref_y: f32,
    /// (asset_id, x, y) per frame in detection order.
    pub frames: Vec<(String, Option<(f32, f32)>)>,
}

/// Transient state for the alignment viewer panel.
#[derive(Debug, Clone)]
pub struct AlignmentUi {
    /// Collection currently being aligned.
    pub collection_id: Option<u64>,
    /// Asset id of the secondary frame being tweaked.
    pub secondary: Option<String>,
    /// 0.0 = show only reference; 1.0 = show only secondary; 0.5 = blend.
    pub blend: f32,
    /// Quality cutoff (0..1). Frames whose `FrameQuality::score` is
    /// below the median × this fraction get auto-rejected. 0 disables
    /// the filter, 1 keeps only frames at-or-above the median.
    pub quality_threshold: f32,
    /// How many reference stars to track across the collection's
    /// frames (4..=32). Surfaced in the alignment viewer + image
    /// viewer overlay; the auto-align worker reads this when building
    /// `TrackedStar` trails.
    pub tracked_star_count: u32,
    /// True ⇒ the image viewer panel overlays labelled markers on the
    /// tracked reference stars.
    pub overlay_tracked_stars: bool,
}

impl Default for AlignmentUi {
    fn default() -> Self {
        Self {
            collection_id: None,
            secondary: None,
            blend: 0.5,
            quality_threshold: 0.0,
            tracked_star_count: 8,
            overlay_tracked_stars: true,
        }
    }
}

/// Per-frame quality score. Populated by `quality::ensure_scored`
/// on first need (typically when the alignment viewer's threshold
/// slider is touched). All scores are intentionally simple, fast,
/// and resolution-independent — comparing two frames in the same
/// collection is what matters, not absolute values.
#[derive(Debug, Clone, Copy, Default)]
pub struct FrameQuality {
    /// Composite focus + star-count score, normalised so the highest
    /// frame in its collection ends up around 1.0. The "top X%" slider
    /// compares per-frame `score` against the collection median.
    pub score: f32,
    /// Number of star-like blobs detected.
    pub star_count: u32,
    /// Median full-width-half-max of detected stars, in pixels. Lower =
    /// sharper. NaN when no stars were detected.
    pub fwhm: f32,
    /// Variance-of-Laplacian over the brightest 32 stars only. High =
    /// crisp focus, low = soft / out-of-focus.
    pub focus: f32,
}

/// Transient state for the star tracker panel.
#[derive(Debug, Clone)]
pub struct StarTrackerUi {
    /// Collection whose star trails we're inspecting.
    pub collection_id: Option<u64>,
    /// Selected `TrackedStar::id` (0 = brightest). `None` shows trail
    /// overview (all stars).
    pub selected_star: Option<u32>,
    /// Crop window size (pixels) around each star's trajectory.
    pub crop_size: u32,
    /// Show predicted Earth-rotation path overlay alongside measured.
    pub show_predicted: bool,
}

impl Default for StarTrackerUi {
    fn default() -> Self {
        Self {
            collection_id: None,
            selected_star: None,
            crop_size: 64,
            show_predicted: true,
        }
    }
}

const MAX_LOG_ENTRIES: usize = 1000;

impl AppState {
    /// Find or create a collection of the given kind with `name`. Idempotent.
    pub fn ensure_collection(&mut self, name: &str, kind: CollectionKind) -> u64 {
        if let Some(c) = self.collections.iter().find(|c| c.name == name && c.kind == kind) {
            return c.id;
        }
        let id = self.next_collection_id;
        self.next_collection_id += 1;
        self.collections.push(Collection {
            id,
            name: name.to_string(),
            kind,
            items: Vec::new(),
            source: CollectionSource::Manual,
        });
        id
    }

    /// Add the given assets (already filtered to non-duplicates) to a collection.
    /// Returns the count actually appended.
    pub fn add_to_collection(&mut self, collection_id: u64, items: Vec<CollectionItem>) -> usize {
        let collection = match self.collections.iter_mut().find(|c| c.id == collection_id) {
            Some(c) => c,
            None => return 0,
        };
        let mut added = 0;
        for it in items {
            if collection.items.iter().any(|x| x.asset_id == it.asset_id) {
                continue;
            }
            collection.items.push(it);
            added += 1;
        }
        added
    }

    pub fn collection(&self, id: u64) -> Option<&Collection> {
        self.collections.iter().find(|c| c.id == id)
    }

    pub fn collection_mut(&mut self, id: u64) -> Option<&mut Collection> {
        self.collections.iter_mut().find(|c| c.id == id)
    }

    /// Add a log entry to the centralized log.
    pub fn add_log(&mut self, level: &str, source: &str, message: &str) {
        if self.log_entries.len() >= MAX_LOG_ENTRIES {
            self.log_entries.remove(0);
        }
        self.log_entries.push(LogEntry {
            timestamp: chrono::Utc::now().format("%H:%M:%S%.3f").to_string(),
            level: level.to_string(),
            source: source.to_string(),
            message: message.to_string(),
        });
    }
}

impl Default for AppState {
    fn default() -> Self {
        Self {
            selected_asset: None,
            search_results: Vec::new(),
            image_zoom: 1.0,
            image_offset: egui::Vec2::ZERO,
            auth: AuthState::default(),
            config: AppConfig::default(),
            thumbnail_cache: ThumbnailCache::default(),
            image_cache: ImageCache::default(),
            pending_requests: PendingRequests::default(),
            histogram_data: None,
            log_entries: Vec::new(),
            log_level_filter: vec![
                "ERROR".to_string(),
                "WARN".to_string(),
                "INFO".to_string(),
                "DEBUG".to_string(),
            ],
            collections: Vec::new(),
            next_collection_id: 1,
            last_collection_id: None,
            stacking_nodes: Vec::new(),
            stacking_edges: Vec::new(),
            stacking_next_node_id: 1,
            stacking_drag: None,
            stacking_pending_edge: None,
            stacking_selection: HashSet::new(),
            stacking_box_drag: None,
            stacking_pan: egui::Vec2::ZERO,
            stacking_zoom: 1.0,
            stacking_open_popup: None,
            stacking_autobound_collection: None,
            stacking_preview: StackingPreview::default(),
            stacking_default_seeded: false,
            image_viewer_picks: std::collections::HashMap::new(),
            alignments: std::collections::HashMap::new(),
            alignment_ui: AlignmentUi::default(),
            star_tracker_ui: StarTrackerUi::default(),
            stacking_jobs: None,
            align_jobs: None,
            align_running: false,
            align_status: None,
            trip_location_name: String::new(),
            trip_lat: 28.1, // Canary Islands default
            trip_lon: -15.4,
            trip_target_name: String::new(),
            trip_target_ra: 0.0,
            trip_target_dec: 0.0,
            trip_focal_length: 400.0,
            trip_aperture: 5.6,
            trip_pixel_size: 4.6,
            sky_center_ra: 90.0,
            sky_center_dec: 0.0,
            sky_zoom: 1.0,
            sky_hour: 22.0,   // 22:00 UTC — typical night observation time
            sky_year: 2026,
            sky_month: 4,
            sky_day: 30,
            // Zoom is per-panel by default — RA/Dec/time stay synced, but
            // each sky viewer keeps its own zoom level. The user can toggle
            // sync on from the panel if they want all viewers locked.
            sky_sync_zoom: false,
            sky_panel_zooms: std::collections::HashMap::new(),
            import_rx: None,
            import_tx: None,
            thumbnails_loaded: 0,
            thumbnails_total: 0,
            share_source: ShareSource::default(),
            requested_images: HashSet::new(),
            theme: Theme::dark(),
            theme_dirty: false,
            theme_loaded: false,
            timeline_selection: HashSet::new(),
            timeline_anchor: None,
            gallery_renaming: None,
            gallery_rename_buffer: String::new(),
            alignment_overlay_show: HashSet::new(),
            inspector_collection_id: None,
        }
    }
}

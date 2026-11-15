/// Amalthea — astrophotography ecosystem application.
///
/// Shared application logic for both native and WASM targets.
/// Uses egui with a Blender-style BSP panel layout, global header bar
/// with workspace tabs, and a footer status bar.

pub mod bsp;
pub mod panels;
pub mod immich;
pub mod auth;
pub mod sky;
pub mod state;
pub mod config;
pub mod cache;
pub mod workspace;
pub mod api;
pub mod import;
pub mod theme;
pub mod persist;
pub mod stacking;
pub mod auto_align;
pub mod align;
pub mod quality;
pub mod stars;
pub mod align_verify;

use bsp::BspNode;
use panels::PanelKind;
use state::AppState;
use workspace::WorkspaceManager;

/// The main application struct implementing `eframe::App`.
pub struct AmaltheaApp {
    workspaces: WorkspaceManager,
    state: AppState,
    status_message: String,
    /// Text field for the Immich shared link import.
    import_url: String,
    /// Index of the workspace tab currently being renamed inline.
    renaming_workspace: Option<usize>,
    /// Buffer holding the in-progress rename text.
    rename_buffer: String,
}

impl AmaltheaApp {
    pub fn new(cc: &eframe::CreationContext<'_>) -> Self {
        let state = AppState::default();
        theme::apply(&cc.egui_ctx, &state.theme);

        Self {
            workspaces: WorkspaceManager::default(),
            state,
            status_message: "Ready".to_string(),
            import_url: "https://photo.emile.space/share/MPLS9uHP-Fl2Ux92OkiJMOOKGh1oVqja4MbfKd_mhxFEWZgQe3FqsqJENGKvr0c8qXk".to_string(),
            renaming_workspace: None,
            rename_buffer: String::new(),
        }
    }
}

impl eframe::App for AmaltheaApp {
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        // Lazy-load the persisted theme on first frame. Done after
        // construction so the UI shows defaults instantly while we wait
        // on the network round-trip.
        persist::load_initial(&mut self.state);

        // Persist any pending theme changes (no-op when not dirty).
        persist::flush(&mut self.state);

        // Re-apply the theme each frame so settings-panel edits propagate
        // immediately. `apply` is cheap (a Visuals struct copy + ctx
        // pointer write).
        theme::apply(ctx, &self.state.theme);

        // Snapshot the theme into locals so the closures below don't have
        // to borrow `self` (which is also borrowed mutably by event
        // handlers / state mutators inside the same scope).
        let theme = self.state.theme;

        // Header bar.
        egui::TopBottomPanel::top("header_bar")
            .exact_height(28.0)
            .frame(egui::Frame::NONE
                .fill(theme.chrome)
                .stroke(egui::Stroke::new(1.0, theme.border))
                .inner_margin(egui::Margin::symmetric(6, 0)))
            .show(ctx, |ui| {
                ui.horizontal_centered(|ui| {
                    // Application title.
                    ui.label(
                        egui::RichText::new("Amalthea")
                            .strong()
                            .size(14.0)
                            .color(theme.accent),
                    );

                    ui.separator();

                    // Workspace tabs.
                    let active = self.workspaces.active_index;
                    let mut delete_index: Option<usize> = None;
                    let mut activate_index: Option<usize> = None;
                    let mut start_rename: Option<usize> = None;
                    let names: Vec<String> = self
                        .workspaces
                        .workspaces
                        .iter()
                        .map(|ws| ws.name.clone())
                        .collect();
                    for (i, name) in names.iter().enumerate() {
                        let is_active = i == active;
                        let editing = self.renaming_workspace == Some(i);
                        if editing {
                            // Inline rename: TextEdit replaces the label until
                            // Enter or focus loss commits.
                            let edit_resp = ui.add(
                                egui::TextEdit::singleline(&mut self.rename_buffer)
                                    .desired_width(110.0)
                                    .font(egui::FontId::proportional(12.0)),
                            );
                            if edit_resp.lost_focus()
                                || ui.input(|i| i.key_pressed(egui::Key::Enter))
                            {
                                let new_name = std::mem::take(&mut self.rename_buffer);
                                if !new_name.trim().is_empty() {
                                    self.workspaces
                                        .rename_workspace(i, new_name.trim().to_string());
                                }
                                self.renaming_workspace = None;
                            }
                            // Auto-focus the field on the frame the rename starts.
                            if !edit_resp.has_focus() {
                                edit_resp.request_focus();
                            }
                            continue;
                        }

                        let text = egui::RichText::new(name)
                            .size(12.0)
                            .color(if is_active { theme.accent } else { theme.text_dim });
                        let btn = ui.selectable_label(is_active, text);
                        if btn.double_clicked() {
                            start_rename = Some(i);
                        } else if btn.clicked() && !is_active {
                            activate_index = Some(i);
                        }
                        btn.context_menu(|ui| {
                            if ui.button("Rename").clicked() {
                                start_rename = Some(i);
                                ui.close_menu();
                            }
                            if ui.button("Delete").clicked() {
                                delete_index = Some(i);
                                ui.close_menu();
                            }
                        });
                    }
                    if let Some(idx) = activate_index {
                        self.workspaces.active_index = idx;
                    }
                    if let Some(idx) = start_rename {
                        self.rename_buffer = self.workspaces.workspaces[idx].name.clone();
                        self.renaming_workspace = Some(idx);
                    }
                    if let Some(idx) = delete_index {
                        self.workspaces.remove_workspace(idx);
                        if self.renaming_workspace == Some(idx) {
                            self.renaming_workspace = None;
                        }
                    }

                    // Add workspace button.
                    if ui.small_button("+").on_hover_text("New workspace").clicked() {
                        self.workspaces.add_workspace(
                            format!("Workspace {}", self.workspaces.workspaces.len() + 1),
                            BspNode::default_layout(),
                        );
                    }

                    ui.separator();

                    // Import from Immich shared link.
                    ui.label(egui::RichText::new("Import:").size(10.0).color(theme.text_dim));
                    let import_resp = ui.add(
                        egui::TextEdit::singleline(&mut self.import_url)
                            .desired_width(200.0)
                            .hint_text("Immich shared link URL")
                            .font(egui::FontId::monospace(9.0)),
                    );
                    if ui.small_button("Fetch").clicked() || (import_resp.lost_focus() && ui.input(|i| i.key_pressed(egui::Key::Enter))) {
                        self.status_message = "Importing from shared link...".to_string();
                        self.state.add_log("INFO", "import", &format!("Fetching shared link: {}", self.import_url));
                        #[cfg(not(target_arch = "wasm32"))]
                        {
                            // Capture the share source up-front so on-demand
                            // image fetches can reuse the same base URL + key.
                            self.state.share_source = state::ShareSource {
                                immich_base: import::extract_base_url(&self.import_url)
                                    .unwrap_or_else(|| "https://photo.emile.space".to_string()),
                                key: import::extract_key(&self.import_url),
                            };
                            self.state.requested_images.clear();

                            let (tx, rx) = std::sync::mpsc::channel();
                            self.state.import_rx = Some(rx);
                            self.state.import_tx = Some(tx.clone());
                            import::spawn_import(
                                tx,
                                self.import_url.clone(),
                                ctx.clone(),
                            );
                        }
                    }

                    // Right-aligned controls.
                    ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                        // Auth status indicator.
                        let (auth_color, auth_text) = match &self.state.auth {
                            auth::AuthState::Authenticated { .. } => {
                                (theme.success, "Authenticated")
                            }
                            _ => (theme.text_faint, "Not signed in"),
                        };
                        ui.colored_label(auth_color, egui::RichText::new(auth_text).size(10.0));

                        ui.separator();

                        // Connection indicator.
                        ui.colored_label(
                            theme.text_faint,
                            egui::RichText::new("Backend: --").size(10.0),
                        );
                    });
                });
            });

        // Footer / status bar.
        egui::TopBottomPanel::bottom("footer_bar")
            .exact_height(22.0)
            .frame(egui::Frame::NONE
                .fill(theme.chrome)
                .stroke(egui::Stroke::new(1.0, theme.border))
                .inner_margin(egui::Margin::symmetric(8, 0)))
            .show(ctx, |ui| {
                ui.horizontal_centered(|ui| {
                    ui.label(
                        egui::RichText::new(&self.status_message)
                            .size(10.0)
                            .color(theme.text),
                    );

                    ui.separator();

                    if let Some(asset) = &self.state.selected_asset {
                        ui.label(
                            egui::RichText::new(format!(
                                "{} ({}x{})",
                                asset.original_file_name, asset.width, asset.height
                            ))
                            .size(10.0)
                            .color(theme.text_dim),
                        );
                        ui.separator();
                    }

                    ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                        ui.label(
                            egui::RichText::new(format!(
                                "Cache: {} thumbs, {} images",
                                self.state.thumbnail_cache.len(),
                                self.state.image_cache.len(),
                            ))
                            .size(10.0)
                            .color(theme.text_faint),
                        );
                    });
                });
            });

        // Process incoming import messages (non-blocking poll).
        process_import_messages(ctx, &mut self.state, &mut self.status_message);

        // Seed the default Source→Stack→Adjust→Output graph if there are
        // frames to process and nothing has been wired up yet, then run
        // any pending evaluation. Both calls are cheap when there's
        // nothing to do.
        stacking::seed_default_graph_if_needed(&mut self.state);
        stacking::evaluate_if_dirty(&mut self.state, ctx);
        // Drain any completed auto-align job results.
        auto_align::pump_results(&mut self.state);

        // If the user has selected an asset whose full image is not cached,
        // kick off a background fetch (preview-size, JPEG).
        #[cfg(not(target_arch = "wasm32"))]
        request_full_image_if_needed(ctx, &mut self.state);

        // Central panel with BSP layout.
        egui::CentralPanel::default()
            .frame(egui::Frame::NONE.fill(theme.canvas))
            .show(ctx, |ui| {
                let rect = ui.available_rect_before_wrap();
                let state = &mut self.state;

                if let Some(layout) = self.workspaces.active_layout_mut() {
                    let action = layout.show(
                        ui,
                        rect,
                        &theme,
                        &mut |ui, id, panel, header_extra, content| {
                            render_panel(ui, id.0, panel, header_extra, content, state);
                        },
                    );
                    layout.apply_action(action);
                }
            });

        ctx.request_repaint();
    }
}

/// Dispatch panel rendering to the appropriate implementation.
fn render_panel(
    ui: &mut egui::Ui,
    panel_id: u64,
    panel: &PanelKind,
    header_extra: egui::Rect,
    rect: egui::Rect,
    state: &mut AppState,
) {
    match panel {
        // Image viewer is the only panel that uses the header extras
        // (asset picker dropdown).
        PanelKind::ImageViewer => panels::image_viewer::show(ui, panel_id, header_extra, rect, state),
        PanelKind::Histogram => panels::histogram::show(ui, rect, state),
        PanelKind::ExifInspector => panels::exif_inspector::show(ui, rect, state),
        PanelKind::Timeline => panels::timeline::show(ui, rect, state),
        PanelKind::SkyViewer => panels::sky_viewer::show(ui, rect, state, panel_id),
        PanelKind::StackingEditor => panels::stacking_editor::show(ui, rect, state),
        PanelKind::StackingInspector => panels::stacking_inspector::show(ui, rect, state),
        PanelKind::AlignmentViewer => panels::alignment_viewer::show(ui, rect, state),
        PanelKind::StarTracker => panels::star_tracker::show(ui, rect, state),
        PanelKind::Settings => panels::settings::show(ui, rect, state),
        PanelKind::LogViewer => panels::log_viewer::show(ui, rect, state),
        PanelKind::TripPlanner => panels::trip_planner::show(ui, rect, state),
    }
}

/// Poll the import channel and process any pending messages.
fn process_import_messages(
    ctx: &egui::Context,
    state: &mut AppState,
    status: &mut String,
) {
    // Take the receiver out temporarily to avoid borrow conflicts.
    let rx = match state.import_rx.take() {
        Some(rx) => rx,
        None => return,
    };

    // Process all available messages this frame (non-blocking).
    let mut got_message = false;
    while let Ok(msg) = rx.try_recv() {
        got_message = true;
        match msg {
            state::ImportMessage::AssetsLoaded(assets) => {
                state.thumbnails_total = assets.len();
                state.thumbnails_loaded = 0;
                *status = format!("Loaded {} assets, fetching thumbnails...", assets.len());
                state.add_log("INFO", "import", &format!("{} assets loaded", assets.len()));
                state.search_results = assets;
            }
            state::ImportMessage::ThumbnailReady { asset_id, data } => {
                // Decode the image bytes and create a texture.
                if let Ok(img) = image::load_from_memory(&data) {
                    let rgba = img.to_rgba8();
                    let w = rgba.width();
                    let h = rgba.height();
                    let size = [w as usize, h as usize];
                    let pixels = rgba.into_raw();

                    // Stash the raw RGBA buffer for the stacking pipeline.
                    stacking::record_frame_pixels(state, &asset_id, w, h, pixels.clone());
                    // Parse the asset's EXIF / file timestamp once and
                    // cache it. The auto-align verifier needs it to
                    // predict Earth-rotation drift between frames.
                    quality::record_frame_timestamp(state, &asset_id);

                    let color_image = egui::ColorImage::from_rgba_unmultiplied(size, &pixels);
                    let texture = ctx.load_texture(
                        format!("thumb_{asset_id}"),
                        color_image,
                        egui::TextureOptions::LINEAR,
                    );
                    state.thumbnail_cache.insert(asset_id, texture);
                    state.thumbnails_loaded += 1;
                    if state.thumbnails_loaded % 5 == 0 || state.thumbnails_loaded == state.thumbnails_total {
                        *status = format!(
                            "Thumbnails: {}/{}",
                            state.thumbnails_loaded, state.thumbnails_total,
                        );
                    }
                } else {
                    state.add_log("WARN", "import", &format!("decode failed for thumbnail {asset_id}"));
                }
            }
            state::ImportMessage::ImageReady { asset_id, data } => {
                match image::load_from_memory(&data) {
                    Ok(img) => {
                        let rgba = img.to_rgba8();
                        let size = [rgba.width() as usize, rgba.height() as usize];
                        let pixels = rgba.into_raw();
                        let color_image = egui::ColorImage::from_rgba_unmultiplied(size, &pixels);
                        let texture = ctx.load_texture(
                            format!("img_{asset_id}"),
                            color_image,
                            egui::TextureOptions::LINEAR,
                        );
                        state.image_cache.insert(asset_id, texture);
                    }
                    Err(e) => {
                        state.add_log("ERROR", "import", &format!("decode failed for preview {asset_id}: {e}"));
                    }
                }
            }
            state::ImportMessage::Status(msg) => {
                *status = msg;
            }
            state::ImportMessage::Error(err) => {
                *status = format!("Import error: {err}");
                state.add_log("ERROR", "import", &err);
            }
        }
    }

    if got_message {
        ctx.request_repaint();
    }

    // Keep the receiver alive — on-demand image fetches reuse it.
    if got_message
        && state.thumbnails_total > 0
        && state.thumbnails_loaded == state.thumbnails_total
    {
        *status = format!("Import complete: {} thumbnails", state.thumbnails_loaded);
    }
    state.import_rx = Some(rx);
}

/// Trigger a background fetch for the selected asset's preview image if it
/// is not already cached or in flight.
#[cfg(not(target_arch = "wasm32"))]
fn request_full_image_if_needed(ctx: &egui::Context, state: &mut AppState) {
    let Some(asset) = state.selected_asset.as_ref() else { return };
    let asset_id = asset.id.clone();

    if state.image_cache.contains(&asset_id) || state.requested_images.contains(&asset_id) {
        return;
    }
    if state.share_source.key.is_empty() || state.share_source.immich_base.is_empty() {
        return;
    }
    let Some(tx) = state.import_tx.as_ref().cloned() else { return };

    state.requested_images.insert(asset_id.clone());
    import::spawn_image_fetch(
        tx,
        state.share_source.immich_base.clone(),
        state.share_source.key.clone(),
        asset_id,
        ctx.clone(),
    );
}


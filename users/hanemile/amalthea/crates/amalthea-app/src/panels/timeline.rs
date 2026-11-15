/// Gallery panel — scrollable thumbnail grid + collections sidebar.
///
/// Left: list of collections (Lights / Darks / …) with item counts,
/// rename inline, sync placeholder.
/// Right: thumbnail grid for `state.search_results` with a per-thumb
/// badge showing which collections each image is already in.
///
/// The internal module name is still `timeline` (and PanelKind::Timeline)
/// for source-stability; the user-visible label is "Gallery".
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use egui::{
    Align2, Color32, FontId, Pos2, Rect, ScrollArea, Sense, Stroke, StrokeKind, Ui, Vec2,
};

use crate::state::{
    AppState, CollectionItem, CollectionKind, CollectionSource,
};

/// What the user picked from the per-thumb context menu (deferred to
/// after the menu closes so we don't borrow `state` twice).
enum AddTo {
    Existing(u64),
    NewKind(CollectionKind),
}

/// Target thumbnail size in pixels (width and height for the grid cell).
const THUMB_SIZE: f32 = 120.0;
/// Spacing between thumbnails in the grid.
const GRID_SPACING: f32 = 8.0;
/// Height reserved for the filename label below each thumbnail.
const LABEL_HEIGHT: f32 = 18.0;
/// Padding inside the panel.
const PADDING: f32 = 8.0;
/// Width of the collections sidebar.
const SIDEBAR_W: f32 = 220.0;

pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.panel);

    // Layout: sidebar (left) + grid (right).
    let sidebar_w = SIDEBAR_W.min(rect.width() * 0.4);
    let sidebar_rect = Rect::from_min_size(rect.min, Vec2::new(sidebar_w, rect.height()));
    let grid_rect = Rect::from_min_max(
        Pos2::new(sidebar_rect.max.x, rect.min.y),
        rect.max,
    );

    show_sidebar(ui, sidebar_rect, state);
    show_grid(ui, grid_rect, state);
}

// ---------------------------------------------------------------------------
// Sidebar — collections list, rename, sync, delete
// ---------------------------------------------------------------------------

fn show_sidebar(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.chrome);
    painter.line_segment(
        [Pos2::new(rect.max.x, rect.min.y), Pos2::new(rect.max.x, rect.max.y)],
        Stroke::new(1.0, state.theme.border),
    );

    let area_id = egui::Id::new("gallery_sidebar_area");
    egui::Area::new(area_id)
        .fixed_pos(rect.min)
        .show(ui.ctx(), |ui| {
            ui.set_clip_rect(rect);
            ui.set_min_size(rect.size());
            ui.set_max_size(rect.size());
            ui.add_space(6.0);

            ui.horizontal(|ui| {
                ui.add_space(8.0);
                ui.label(
                    egui::RichText::new("Groups")
                        .size(12.0)
                        .strong()
                        .color(state.theme.text),
                );
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    ui.add_space(8.0);
                    ui.menu_button("New", |ui| {
                        let mut to_create: Option<CollectionKind> = None;
                        for kind in CollectionKind::ALL {
                            if ui.button(kind.label()).clicked() {
                                to_create = Some(*kind);
                                ui.close_menu();
                            }
                        }
                        if let Some(kind) = to_create {
                            let mut n = 1;
                            let label = kind.label();
                            let name = loop {
                                let candidate = format!("{label} {n}");
                                if !state.collections.iter().any(|c| c.name == candidate) {
                                    break candidate;
                                }
                                n += 1;
                            };
                            state.ensure_collection(&name, kind);
                        }
                    });
                });
            });
            ui.separator();

            ScrollArea::vertical()
                .id_salt("gallery_groups_scroll")
                .max_height(rect.height() - 40.0)
                .show(ui, |ui| {
                    show_groups_list(ui, state);
                });
        });
}

fn show_groups_list(ui: &mut Ui, state: &mut AppState) {
    if state.collections.is_empty() {
        ui.add_space(8.0);
        ui.label(
            egui::RichText::new("No groups yet.")
                .size(10.0)
                .color(state.theme.text_faint),
        );
        ui.label(
            egui::RichText::new("Right-click an image in the gallery → Add to / New group.")
                .size(10.0)
                .color(state.theme.text_faint),
        );
        return;
    }

    // Snapshot ids first so the iteration doesn't fight the mutable
    // edits below.
    let ids: Vec<u64> = state.collections.iter().map(|c| c.id).collect();
    let mut delete_id: Option<u64> = None;
    let mut sync_id: Option<u64> = None;
    let mut start_rename: Option<u64> = None;

    for id in ids {
        let (kind, name, count, source, is_renaming, is_active) = {
            let c = match state.collections.iter().find(|c| c.id == id) {
                Some(c) => c,
                None => continue,
            };
            (
                c.kind,
                c.name.clone(),
                c.items.len(),
                c.source.clone(),
                state.gallery_renaming == Some(c.id),
                state.last_collection_id == Some(c.id),
            )
        };

        ui.horizontal(|ui| {
            ui.add_space(6.0);
            // Coloured chip for the kind.
            let chip_color = kind_color(kind, &state.theme);
            let (chip_rect, _) = ui.allocate_exact_size(Vec2::new(8.0, 14.0), Sense::hover());
            ui.painter().rect_filled(chip_rect, 1.0, chip_color);

            ui.add_space(4.0);

            if is_renaming {
                let resp = ui.add(
                    egui::TextEdit::singleline(&mut state.gallery_rename_buffer)
                        .desired_width(120.0)
                        .font(FontId::proportional(11.0)),
                );
                if resp.lost_focus() || ui.input(|i| i.key_pressed(egui::Key::Enter)) {
                    let new_name = std::mem::take(&mut state.gallery_rename_buffer);
                    if !new_name.trim().is_empty() {
                        if let Some(c) = state.collections.iter_mut().find(|c| c.id == id) {
                            c.name = new_name.trim().to_string();
                        }
                    }
                    state.gallery_renaming = None;
                }
                if !resp.has_focus() {
                    resp.request_focus();
                }
            } else {
                let label = egui::RichText::new(format!("{name}  ·  {count}"))
                    .size(11.0)
                    .color(if is_active { state.theme.accent } else { state.theme.text });
                let row_resp = ui.selectable_label(is_active, label);
                if row_resp.clicked() {
                    state.last_collection_id = Some(id);
                }
                if row_resp.double_clicked() {
                    start_rename = Some(id);
                }
                row_resp.context_menu(|ui| {
                    if ui.button("Rename").clicked() {
                        start_rename = Some(id);
                        ui.close_menu();
                    }
                    if ui
                        .button(if matches!(source, CollectionSource::ImmichAlbum { .. }) {
                            "Sync from Immich"
                        } else {
                            "Bind to current album"
                        })
                        .clicked()
                    {
                        sync_id = Some(id);
                        ui.close_menu();
                    }
                    ui.separator();
                    if ui.button("Delete group").clicked() {
                        delete_id = Some(id);
                        ui.close_menu();
                    }
                });
            }
        });

        // Source-binding indicator.
        if let CollectionSource::ImmichAlbum { album_id, .. } = &source {
            ui.horizontal(|ui| {
                ui.add_space(20.0);
                ui.label(
                    egui::RichText::new(format!(
                        "↻ {}",
                        truncate(album_id, 18)
                    ))
                    .size(9.0)
                    .color(state.theme.text_faint)
                    .monospace(),
                );
            });
        }
    }

    if let Some(id) = start_rename {
        if let Some(c) = state.collections.iter().find(|c| c.id == id) {
            state.gallery_rename_buffer = c.name.clone();
            state.gallery_renaming = Some(id);
        }
    }
    if let Some(id) = delete_id {
        state.collections.retain(|c| c.id != id);
        // Also drop any Source nodes pointing at the now-missing collection.
        for slot in state.stacking_nodes.iter_mut() {
            if let crate::state::StackingNode::Source(s) = &mut slot.node {
                if s.collection_id == Some(id) {
                    s.collection_id = None;
                }
            }
        }
        if state.last_collection_id == Some(id) {
            state.last_collection_id = None;
        }
        state.alignments.remove(&id);
        state.stacking_preview.dirty = true;
        state.add_log("INFO", "gallery", "Deleted group");
    }
    if let Some(id) = sync_id {
        sync_or_bind_collection(state, id);
    }
}

/// Bind the collection to the currently-imported Immich album (or
/// re-pull the items from the bound album).
fn sync_or_bind_collection(state: &mut AppState, id: u64) {
    let share = state.share_source.clone();
    if share.key.is_empty() {
        state.add_log(
            "WARN",
            "gallery",
            "No Immich share is currently loaded — open an Immich link first.",
        );
        return;
    }

    // Pull each currently-loaded asset id from search_results into the
    // collection. This is the simple version: "everything visible".
    // A future iteration can do a real /api/albums/{id} round-trip
    // and detect adds/removes.
    let to_add: Vec<CollectionItem> = state
        .search_results
        .iter()
        .map(|a| CollectionItem {
            asset_id: a.id.clone(),
            filename: a.original_file_name.clone(),
            accepted: true,
        })
        .collect();
    let added = state.add_to_collection(id, to_add);
    if let Some(c) = state.collections.iter_mut().find(|c| c.id == id) {
        c.source = CollectionSource::ImmichAlbum {
            album_id: share.immich_base.clone(),
            share_key: share.key.clone(),
        };
    }
    state.add_log(
        "INFO",
        "gallery",
        &format!("Synced {added} new image(s) into group"),
    );
    state.stacking_preview.dirty = true;
}

fn kind_color(kind: CollectionKind, t: &crate::theme::Theme) -> Color32 {
    match kind {
        CollectionKind::Lights => t.accent,
        CollectionKind::Darks => t.error,
        CollectionKind::Bias => t.info,
        CollectionKind::Stacked => t.success,
        CollectionKind::Custom => t.text_dim,
    }
}

fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        s.to_string()
    } else {
        let cut: String = s.chars().take(max - 1).collect();
        format!("{cut}…")
    }
}

// ---------------------------------------------------------------------------
// Grid — thumbnail grid with per-thumb group badges
// ---------------------------------------------------------------------------

fn show_grid(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    if state.search_results.is_empty() {
        ui.painter_at(rect).text(
            rect.center(),
            Align2::CENTER_CENTER,
            "No images",
            FontId::proportional(14.0),
            state.theme.text_faint,
        );
        return;
    }

    let content_width = rect.width() - 2.0 * PADDING;
    let cell_size = THUMB_SIZE + GRID_SPACING;
    let columns = ((content_width + GRID_SPACING) / cell_size).floor().max(1.0) as usize;

    // Pre-compute each asset's group memberships so we don't redo the
    // scan inside the inner loop. Also captures kind for the badge.
    let memberships = compute_memberships(state);

    let area_id = egui::Id::new("gallery_grid_area");
    egui::Area::new(area_id)
        .fixed_pos(rect.min)
        .show(ui.ctx(), |ui| {
            ui.set_clip_rect(rect);
            ui.set_min_size(rect.size());
            ui.set_max_size(rect.size());

            ScrollArea::vertical()
                .id_salt("gallery_grid_scroll")
                .max_height(rect.height())
                .max_width(rect.width())
                .show(ui, |ui| {
                    ui.add_space(PADDING);

                    enum ClickKind { Plain, Toggle, Range }
                    let mut click: Option<(usize, ClickKind)> = None;
                    let mut add_target: Option<AddTo> = None;
                    let mut clear_selection = false;
                    let mut promote_clicked: Option<String> = None;

                    let row_count = (state.search_results.len() + columns - 1) / columns;
                    for row in 0..row_count {
                        ui.horizontal(|ui| {
                            ui.add_space(PADDING);
                            for col in 0..columns {
                                let idx = row * columns + col;
                                if idx >= state.search_results.len() {
                                    break;
                                }
                                let asset = &state.search_results[idx];
                                let asset_id = asset.id.clone();
                                let is_primary = state
                                    .selected_asset
                                    .as_ref()
                                    .map_or(false, |s| s.id == asset_id);
                                let is_in_selection =
                                    state.timeline_selection.contains(&asset_id);
                                let badges = memberships
                                    .get(&asset_id)
                                    .cloned()
                                    .unwrap_or_default();

                                let total_height = THUMB_SIZE + LABEL_HEIGHT;
                                let (cell_rect, response) = ui.allocate_exact_size(
                                    Vec2::new(THUMB_SIZE, total_height),
                                    Sense::click(),
                                );
                                let response = response.interact(Sense::click_and_drag());
                                let thumb_rect = Rect::from_min_size(
                                    cell_rect.min,
                                    Vec2::splat(THUMB_SIZE),
                                );

                                if is_in_selection {
                                    let tint = state.theme.accent;
                                    ui.painter().rect_filled(
                                        thumb_rect.expand(2.0),
                                        2.0,
                                        Color32::from_rgba_unmultiplied(
                                            tint.r(), tint.g(), tint.b(), 30,
                                        ),
                                    );
                                }
                                if is_primary {
                                    ui.painter().rect_stroke(
                                        thumb_rect.expand(2.0),
                                        2.0,
                                        Stroke::new(2.0, state.theme.accent),
                                        StrokeKind::Outside,
                                    );
                                } else if is_in_selection {
                                    ui.painter().rect_stroke(
                                        thumb_rect.expand(2.0),
                                        2.0,
                                        Stroke::new(1.0, state.theme.accent_dim),
                                        StrokeKind::Outside,
                                    );
                                }

                                if response.hovered() {
                                    ui.painter().rect_filled(
                                        thumb_rect,
                                        2.0,
                                        Color32::from_white_alpha(15),
                                    );
                                }

                                if let Some(texture) =
                                    state.thumbnail_cache.get(&asset_id)
                                {
                                    let tex_size = texture.size_vec2();
                                    let scale = (THUMB_SIZE / tex_size.x)
                                        .min(THUMB_SIZE / tex_size.y);
                                    let display_size = tex_size * scale;
                                    let centered_rect = Rect::from_center_size(
                                        thumb_rect.center(),
                                        display_size,
                                    );
                                    ui.painter().image(
                                        texture.id(),
                                        centered_rect,
                                        Rect::from_min_max(
                                            Pos2::ZERO,
                                            Pos2::new(1.0, 1.0),
                                        ),
                                        Color32::WHITE,
                                    );
                                } else {
                                    ui.painter().rect_filled(
                                        thumb_rect,
                                        2.0,
                                        state.theme.canvas,
                                    );
                                    ui.painter().text(
                                        thumb_rect.center(),
                                        Align2::CENTER_CENTER,
                                        "...",
                                        FontId::proportional(11.0),
                                        state.theme.text_faint,
                                    );
                                }

                                // Per-group badges (kind chips at the top-left).
                                draw_membership_badges(
                                    ui.painter(),
                                    thumb_rect,
                                    &badges,
                                    &state.theme,
                                );

                                // Filename label.
                                let label_rect = Rect::from_min_size(
                                    Pos2::new(cell_rect.min.x, thumb_rect.max.y),
                                    Vec2::new(THUMB_SIZE, LABEL_HEIGHT),
                                );
                                let filename = &asset.original_file_name;
                                let display_name = if filename.len() > 16 {
                                    format!("{}...", &filename[..13])
                                } else {
                                    filename.clone()
                                };
                                ui.painter().text(
                                    label_rect.center(),
                                    Align2::CENTER_CENTER,
                                    display_name,
                                    FontId::proportional(10.0),
                                    state.theme.text_dim,
                                );

                                if response.clicked() {
                                    let mods = ui.input(|i| i.modifiers);
                                    let kind = if mods.shift {
                                        ClickKind::Range
                                    } else if mods.command || mods.ctrl {
                                        ClickKind::Toggle
                                    } else {
                                        ClickKind::Plain
                                    };
                                    click = Some((idx, kind));
                                }

                                response.context_menu(|ui| {
                                    let count = if is_in_selection {
                                        state.timeline_selection.len()
                                    } else {
                                        1
                                    };
                                    ui.label(
                                        egui::RichText::new(format!(
                                            "Add {count} image{} to:",
                                            if count == 1 { "" } else { "s" }
                                        ))
                                        .size(10.0)
                                        .color(state.theme.text_dim),
                                    );
                                    ui.separator();

                                    if !state.collections.is_empty() {
                                        for c in &state.collections {
                                            let label = format!(
                                                "{} — {} ({})",
                                                c.kind.label(),
                                                c.name,
                                                c.items.len(),
                                            );
                                            if ui.button(label).clicked() {
                                                if !is_in_selection {
                                                    promote_clicked =
                                                        Some(asset_id.clone());
                                                }
                                                add_target =
                                                    Some(AddTo::Existing(c.id));
                                                ui.close_menu();
                                            }
                                        }
                                        ui.separator();
                                    }
                                    for kind in CollectionKind::ALL {
                                        let label = format!("New {} group", kind.label());
                                        if ui.button(label).clicked() {
                                            if !is_in_selection {
                                                promote_clicked = Some(asset_id.clone());
                                            }
                                            add_target = Some(AddTo::NewKind(*kind));
                                            ui.close_menu();
                                        }
                                    }
                                    ui.separator();
                                    if ui.button("Clear gallery selection").clicked() {
                                        clear_selection = true;
                                        ui.close_menu();
                                    }
                                });

                                ui.add_space(GRID_SPACING);
                            }
                        });
                        ui.add_space(GRID_SPACING);
                    }

                    // Apply deferred click.
                    if let Some((idx, kind)) = click {
                        let asset_id = state.search_results[idx].id.clone();
                        match kind {
                            ClickKind::Plain => {
                                state.timeline_selection.clear();
                                state.timeline_selection.insert(asset_id.clone());
                                state.timeline_anchor = Some(asset_id);
                            }
                            ClickKind::Toggle => {
                                if !state.timeline_selection.remove(&asset_id) {
                                    state.timeline_selection.insert(asset_id.clone());
                                }
                                state.timeline_anchor = Some(asset_id);
                            }
                            ClickKind::Range => {
                                let anchor_idx =
                                    state.timeline_anchor.as_ref().and_then(|a| {
                                        state.search_results.iter().position(|x| &x.id == a)
                                    });
                                let (lo, hi) = match anchor_idx {
                                    Some(a) if a <= idx => (a, idx),
                                    Some(a) => (idx, a),
                                    None => (idx, idx),
                                };
                                state.timeline_selection.clear();
                                for i in lo..=hi {
                                    state
                                        .timeline_selection
                                        .insert(state.search_results[i].id.clone());
                                }
                            }
                        }
                        state.selected_asset = Some(state.search_results[idx].clone());
                        state.image_zoom = 1.0;
                        state.image_offset = Vec2::ZERO;
                    }

                    if let Some(id) = promote_clicked {
                        state.timeline_selection.clear();
                        state.timeline_selection.insert(id);
                    }

                    if let Some(target) = add_target {
                        let collection_id = match target {
                            AddTo::Existing(id) => id,
                            AddTo::NewKind(kind) => {
                                let mut n = 1;
                                let label = kind.label();
                                let name = loop {
                                    let candidate = format!("{label} {n}");
                                    if !state
                                        .collections
                                        .iter()
                                        .any(|c| c.name == candidate)
                                    {
                                        break candidate;
                                    }
                                    n += 1;
                                };
                                state.ensure_collection(&name, kind)
                            }
                        };
                        state.last_collection_id = Some(collection_id);
                        let items: Vec<CollectionItem> = state
                            .search_results
                            .iter()
                            .filter(|a| state.timeline_selection.contains(&a.id))
                            .map(|a| CollectionItem {
                                asset_id: a.id.clone(),
                                filename: a.original_file_name.clone(),
                                accepted: true,
                            })
                            .collect();
                        let added = state.add_to_collection(collection_id, items);
                        let label = state
                            .collection(collection_id)
                            .map(|c| format!("{} — {}", c.kind.label(), c.name))
                            .unwrap_or_else(|| "(unknown)".into());
                        state.add_log(
                            "INFO",
                            "stacking",
                            &format!("Added {added} image(s) to {label}"),
                        );
                        state.stacking_preview.dirty = true;
                    }

                    if clear_selection {
                        state.timeline_selection.clear();
                        state.timeline_anchor = None;
                    }

                    ui.add_space(PADDING);
                });
        });
}

/// Map of `asset_id → list of (group_kind, group_name)` for badge
/// rendering. Computed once per frame in O(N·M) where N is search
/// results and M is total collection items.
fn compute_memberships(
    state: &AppState,
) -> std::collections::HashMap<String, Vec<(CollectionKind, String)>> {
    let mut out: std::collections::HashMap<String, Vec<(CollectionKind, String)>> =
        std::collections::HashMap::new();
    for c in &state.collections {
        for item in &c.items {
            out.entry(item.asset_id.clone())
                .or_default()
                .push((c.kind, c.name.clone()));
        }
    }
    out
}

/// Draw small kind-coloured chips at the top-left of the thumb to
/// show which groups it belongs to.
fn draw_membership_badges(
    painter: &egui::Painter,
    thumb_rect: Rect,
    badges: &[(CollectionKind, String)],
    theme: &crate::theme::Theme,
) {
    if badges.is_empty() {
        return;
    }
    let mut x = thumb_rect.min.x + 4.0;
    let y = thumb_rect.min.y + 4.0;
    for (kind, _name) in badges.iter().take(4) {
        let r = Rect::from_min_size(Pos2::new(x, y), Vec2::new(8.0, 8.0));
        painter.rect(
            r,
            1.0,
            kind_color(*kind, theme),
            Stroke::new(0.5, theme.canvas),
            StrokeKind::Inside,
        );
        x += 10.0;
    }
    if badges.len() > 4 {
        painter.text(
            Pos2::new(x + 1.0, y + 4.0),
            Align2::LEFT_CENTER,
            format!("+{}", badges.len() - 4),
            FontId::proportional(8.0),
            theme.text,
        );
    }
}

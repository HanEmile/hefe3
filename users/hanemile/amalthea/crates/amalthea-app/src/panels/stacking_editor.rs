/// Stacking editor — Blender-style node graph for the stacking pipeline.
///
/// Two sections, top to bottom:
/// 1. Header (Stack / Save / Reset buttons + collection counts)
/// 2. Node editor — pan/zoom canvas, drag nodes, wire output→input
///    ports, edit params inline.
///
/// Source nodes are bound to a Collection (group of asset ids). The
/// node body shows a tiny preview strip plus a "browse" button that
/// opens a floating popup with the full grid + accept toggles. The
/// editor no longer renders a panel-wide preview strip — overflowing
/// thumbnails were getting clipped.
///
/// Stack nodes carry two input ports: Lights (top) and Darks (bottom).
/// The dark bag is averaged per-pixel and subtracted from each light
/// before reduction, removing sensor hot-pixels.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use egui::{Color32, FontId, Pos2, Rect, Sense, Stroke, StrokeKind, Ui, Vec2};

use super::util::trim;
use crate::stacking;
use crate::state::{
    AdjustNode, AppState, BoxDrag, CollectionItem, CollectionKind, In, OutputNode, SourceNode,
    StackMethod, StackNode, StackingEdge, StackingNode, StackingNodeSlot,
};

/// Render the stacking editor panel.
pub fn show(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.panel);

    let header_h = 26.0;
    let header_rect = Rect::from_min_size(rect.min, Vec2::new(rect.width(), header_h));
    let graph_rect = Rect::from_min_max(
        Pos2::new(rect.min.x, rect.min.y + header_h),
        rect.max,
    );

    show_header(ui, header_rect, state);
    show_node_graph(ui, graph_rect, state);
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

fn show_header(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.chrome);
    painter.line_segment(
        [Pos2::new(rect.min.x, rect.max.y), Pos2::new(rect.max.x, rect.max.y)],
        Stroke::new(1.0, state.theme.border),
    );

    let total_items: usize = state.collections.iter().map(|c| c.items.len()).sum();
    let preview_ready = state.stacking_preview.output.is_some();
    let running = state.stacking_preview.running;
    let can_stack = total_items >= 2 && !state.stacking_nodes.is_empty();

    painter.text(
        Pos2::new(rect.min.x + 8.0, rect.center().y),
        egui::Align2::LEFT_CENTER,
        "Stacking",
        FontId::proportional(12.0),
        state.theme.text,
    );

    let status = if running {
        "stacking…".to_string()
    } else if let Some(err) = &state.stacking_preview.error {
        format!("error: {err}")
    } else if preview_ready {
        format!("{} group(s) · preview ready", state.collections.len())
    } else {
        format!("{} image(s) across {} group(s)", total_items, state.collections.len())
    };
    let status_color = if state.stacking_preview.error.is_some() {
        state.theme.error
    } else {
        state.theme.text_dim
    };
    painter.text(
        Pos2::new(rect.min.x + 80.0, rect.center().y),
        egui::Align2::LEFT_CENTER,
        status,
        FontId::proportional(11.0),
        status_color,
    );

    // Stack and Save buttons (right-aligned).
    let btn_size = Vec2::new(72.0, 18.0);
    let stack_rect = Rect::from_min_size(
        Pos2::new(rect.max.x - btn_size.x - 6.0, rect.center().y - btn_size.y / 2.0),
        btn_size,
    );
    if button(ui, stack_rect, "Stack", can_stack, &state.theme) {
        state.stacking_preview.dirty = true;
        state.add_log("INFO", "stacking", "Re-evaluating graph.");
    }

    let save_rect = Rect::from_min_size(
        Pos2::new(stack_rect.min.x - btn_size.x - 4.0, stack_rect.min.y),
        btn_size,
    );
    let save_filename = output_filename(state);
    if button(ui, save_rect, "Save PNG", preview_ready, &state.theme) {
        if let Some(name) = save_filename {
            stacking::save_output(state, &name);
        }
    }

    let reset_rect = Rect::from_min_size(
        Pos2::new(save_rect.min.x - btn_size.x - 4.0, save_rect.min.y),
        btn_size,
    );
    if button(ui, reset_rect, "Reset", true, &state.theme) {
        state.stacking_default_seeded = false;
        state.stacking_nodes.clear();
        state.stacking_edges.clear();
        state.stacking_selection.clear();
        state.stacking_pan = Vec2::ZERO;
        state.stacking_zoom = 1.0;
    }
}

fn output_filename(state: &AppState) -> Option<String> {
    state
        .stacking_nodes
        .iter()
        .find_map(|slot| {
            if let StackingNode::Output(o) = &slot.node {
                Some(o.filename.clone())
            } else {
                None
            }
        })
        .or_else(|| Some("stacked.png".to_string()))
}

fn button(ui: &mut Ui, rect: Rect, text: &str, enabled: bool, t: &crate::theme::Theme) -> bool {
    let resp = ui.allocate_rect(rect, Sense::click());
    let (fill, stroke, fg) = if !enabled {
        (t.panel, t.border, t.text_faint)
    } else if resp.hovered() {
        (t.accent, t.accent, t.canvas)
    } else {
        (t.accent_dim, t.accent, t.text)
    };
    let painter = ui.painter();
    painter.rect(rect, 2.0, fill, Stroke::new(1.0, stroke), StrokeKind::Inside);
    painter.text(
        rect.center(),
        egui::Align2::CENTER_CENTER,
        text,
        FontId::proportional(11.0),
        fg,
    );
    enabled && resp.clicked()
}

// ---------------------------------------------------------------------------
// Node editor — pan, zoom, box-select, ports, bodies
// ---------------------------------------------------------------------------

const NODE_W: f32 = 200.0;
const TITLE_H: f32 = 18.0;
const PORT_R: f32 = 4.5;
const BODY_PAD: f32 = 6.0;
const MIN_ZOOM: f32 = 0.4;
const MAX_ZOOM: f32 = 2.0;

/// Fallback body height per kind — used the first frame before the
/// measurement pass has filled in `measured_height`. Generous so most
/// content fits on the first paint and the dynamic resize converges
/// quickly.
fn fallback_body_height(node: &StackingNode) -> f32 {
    match node {
        StackingNode::Source(_) => 76.0,  // strip + bind row + browse btn
        StackingNode::Stack(_) => 60.0,   // method + dark-subtract toggle
        StackingNode::Adjust(_) => 124.0, // 4 sliders @ ~28px + spacing
        StackingNode::Output(_) => 64.0,  // label + textedit + button
    }
}

fn slot_height(slot: &StackingNodeSlot) -> f32 {
    let body = slot
        .measured_height
        .unwrap_or_else(|| fallback_body_height(&slot.node));
    TITLE_H + body + BODY_PAD * 2.0
}

/// Convert a graph-local point to screen coordinates given pan/zoom.
///
/// The node editor uses the "Blender style" transform: zoom only
/// affects *positions* (node origins move further apart as you zoom
/// in), not *sizes*. Node frames, text, and body widgets always
/// render at their natural screen size. This keeps text readable at
/// any zoom level and avoids the "widget overflows the node frame"
/// bug where the frame scales but its contents don't.
fn to_screen(graph_origin: Pos2, pan: Vec2, zoom: f32, p: Pos2) -> Pos2 {
    Pos2::new(
        graph_origin.x + pan.x + p.x * zoom,
        graph_origin.y + pan.y + p.y * zoom,
    )
}

/// Convert a screen point to graph-local coordinates.
fn to_graph(graph_origin: Pos2, pan: Vec2, zoom: f32, p: Pos2) -> Pos2 {
    Pos2::new(
        (p.x - graph_origin.x - pan.x) / zoom,
        (p.y - graph_origin.y - pan.y) / zoom,
    )
}

fn show_node_graph(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let painter = ui.painter_at(rect);
    painter.rect_filled(rect, 0.0, state.theme.canvas);
    painter.rect_stroke(
        rect,
        0.0,
        Stroke::new(1.0, state.theme.border),
        StrokeKind::Inside,
    );

    // Subtle grid that scrolls with the pan offset.
    let grid_step = 24.0 * state.stacking_zoom;
    let grid_color = Color32::from_rgba_unmultiplied(0xff, 0xff, 0xff, 8);
    let pan = state.stacking_pan;
    let mut gx = rect.min.x + pan.x.rem_euclid(grid_step);
    while gx < rect.max.x {
        painter.line_segment(
            [Pos2::new(gx, rect.min.y), Pos2::new(gx, rect.max.y)],
            Stroke::new(0.5, grid_color),
        );
        gx += grid_step;
    }
    let mut gy = rect.min.y + pan.y.rem_euclid(grid_step);
    while gy < rect.max.y {
        painter.line_segment(
            [Pos2::new(rect.min.x, gy), Pos2::new(rect.max.x, gy)],
            Stroke::new(0.5, grid_color),
        );
        gy += grid_step;
    }

    let canvas_resp = ui.allocate_rect(rect, Sense::click_and_drag());
    let pointer = ui.input(|i| i.pointer.interact_pos());

    // -- Pan / zoom interaction. Done via raw input so middle-button
    // drags work — Sense::click_and_drag() on the canvas only senses
    // primary-button drags.
    handle_pan_zoom(ui, rect, state);

    // Snapshot pan/zoom after handling so the rest of this frame uses
    // the new transform.
    let pan = state.stacking_pan;
    let zoom = state.stacking_zoom;
    let origin = rect.min;

    // -- Right-click on empty canvas: add-node menu.
    canvas_resp.context_menu(|ui| {
        let pos_screen = ui.input(|i| i.pointer.interact_pos()).unwrap_or(rect.center());
        let local = to_graph(origin, pan, zoom, pos_screen);
        let local = Pos2::new(
            local.x.max(0.0),
            local.y.max(0.0),
        );

        ui.label(
            egui::RichText::new("Add node")
                .size(10.0)
                .color(state.theme.text_dim),
        );
        ui.separator();

        // Submenu: Source from <collection>.
        ui.menu_button("Source from group", |ui| {
            if state.collections.is_empty() {
                ui.label(
                    egui::RichText::new("(no groups yet — add some from the timeline)")
                        .size(10.0)
                        .color(state.theme.text_faint),
                );
            }
            // Snapshot to a small Vec to avoid an aliased borrow when
            // we mutate state via push_node below.
            let groups: Vec<(u64, String)> = state
                .collections
                .iter()
                .map(|c| (c.id, format!("{} — {} ({})", c.kind.label(), c.name, c.items.len())))
                .collect();
            let mut chosen: Option<u64> = None;
            for (id, label) in &groups {
                if ui.button(label).clicked() {
                    chosen = Some(*id);
                    ui.close_menu();
                }
            }
            if let Some(cid) = chosen {
                push_node(
                    state,
                    local,
                    StackingNode::Source(SourceNode { collection_id: Some(cid) }),
                );
            }
        });

        if ui.button("Source (unbound)").clicked() {
            push_node(
                state,
                local,
                StackingNode::Source(SourceNode { collection_id: None }),
            );
            ui.close_menu();
        }
        if ui.button("Stack").clicked() {
            push_node(state, local, StackingNode::Stack(StackNode {
                method: StackMethod::Mean,
                subtract_darks: true,
            }));
            ui.close_menu();
        }
        if ui.button("Adjust").clicked() {
            push_node(state, local, StackingNode::Adjust(AdjustNode::default()));
            ui.close_menu();
        }
        if ui.button("Output").clicked() {
            push_node(state, local, StackingNode::Output(OutputNode::default()));
            ui.close_menu();
        }
        ui.separator();
        if ui.button("Reset to default chain").clicked() {
            state.stacking_default_seeded = false;
            state.stacking_nodes.clear();
            state.stacking_edges.clear();
            state.stacking_selection.clear();
            ui.close_menu();
        }
        if !state.stacking_selection.is_empty()
            && ui.button(format!("Delete {} selected", state.stacking_selection.len())).clicked()
        {
            let to_remove: Vec<u64> = state.stacking_selection.iter().copied().collect();
            for id in &to_remove {
                state.stacking_nodes.retain(|n| n.id != *id);
                state.stacking_edges.retain(|e| e.from != *id && e.to != *id);
            }
            state.stacking_selection.clear();
            state.stacking_preview.dirty = true;
            ui.close_menu();
        }
    });

    // -- Box-select drag start (only on empty canvas, primary button).
    if canvas_resp.drag_started_by(egui::PointerButton::Primary) {
        if let Some(p) = pointer {
            // Only start if not over a node title bar (those start a node-drag).
            let on_node = node_under(state, origin, pan, zoom, p).is_some();
            if !on_node {
                let mods = ui.input(|i| i.modifiers);
                let local = to_graph(origin, pan, zoom, p);
                // Shift+drag is the "slice" gesture: delete any edge
                // whose path the drag crosses. Plain drag is box-select.
                state.stacking_box_drag = Some(BoxDrag {
                    start: local,
                    current: local,
                    additive: mods.shift,
                    slice: mods.shift,
                });
                if !mods.shift {
                    state.stacking_selection.clear();
                }
                state.stacking_pending_edge = None;
            }
        }
    }
    if let Some(box_drag) = state.stacking_box_drag.as_mut() {
        if let Some(p) = pointer {
            box_drag.current = to_graph(origin, pan, zoom, p);
        }
        // Visualise: red slice line for shift-drag, accent box otherwise.
        let a = to_screen(origin, pan, zoom, box_drag.start);
        let b = to_screen(origin, pan, zoom, box_drag.current);
        if box_drag.slice {
            ui.painter().line_segment(
                [a, b],
                Stroke::new(2.0, state.theme.error),
            );
        } else {
            let r = Rect::from_two_pos(a, b);
            ui.painter().rect_filled(
                r,
                0.0,
                Color32::from_rgba_unmultiplied(
                    state.theme.accent.r(),
                    state.theme.accent.g(),
                    state.theme.accent.b(),
                    28,
                ),
            );
            ui.painter()
                .rect_stroke(r, 0.0, Stroke::new(1.0, state.theme.accent), StrokeKind::Inside);
        }
    }
    if canvas_resp.drag_stopped() {
        if let Some(box_drag) = state.stacking_box_drag.take() {
            if box_drag.slice {
                // Convert the slice endpoints to screen coords; the
                // edge bezier sampling we re-do below is in screen
                // coords too. Anything that intersects = delete.
                let s = to_screen(origin, pan, zoom, box_drag.start);
                let e = to_screen(origin, pan, zoom, box_drag.current);
                let mut removed = 0usize;
                let edges_snapshot: Vec<crate::state::StackingEdge> =
                    state.stacking_edges.clone();
                let mut to_keep: Vec<bool> = vec![true; edges_snapshot.len()];
                for (i, edge) in edges_snapshot.iter().enumerate() {
                    if edge_crosses_segment(state, origin, pan, zoom, *edge, s, e) {
                        to_keep[i] = false;
                        removed += 1;
                    }
                }
                if removed > 0 {
                    let mut idx = 0;
                    state.stacking_edges.retain(|_| {
                        let keep = to_keep[idx];
                        idx += 1;
                        keep
                    });
                    state.stacking_preview.dirty = true;
                    state
                        .add_log("INFO", "stacking", &format!("Cut {removed} edge(s)"));
                }
            } else {
                // Box drag start/current are graph-local coords; node
                // origins are also graph-local; node frames render at
                // fixed *screen* size, so in graph space they cover
                // `NODE_W/zoom` x `slot_height/zoom`.
                let r = Rect::from_two_pos(box_drag.start, box_drag.current);
                if !box_drag.additive {
                    state.stacking_selection.clear();
                }
                let inv = if zoom > 0.0 { 1.0 / zoom } else { 1.0 };
                for slot in &state.stacking_nodes {
                    let nr = Rect::from_min_size(
                        slot.pos,
                        Vec2::new(NODE_W * inv, slot_height(slot) * inv),
                    );
                    if r.intersects(nr) {
                        state.stacking_selection.insert(slot.id);
                    }
                }
            }
        }
    }

    // -- Edges (drawn first so nodes paint on top).
    //
    // Node frames are at fixed screen size — only their *origin*
    // moves with pan/zoom. So body height is `slot_height(slot)`,
    // not `slot_height(slot) * zoom`.
    let positions: Vec<(u64, Pos2, f32)> = state
        .stacking_nodes
        .iter()
        .map(|n| (n.id, to_screen(origin, pan, zoom, n.pos), slot_height(n)))
        .collect();
    let pos_of = |id: u64| -> Option<(Pos2, f32)> {
        positions.iter().find(|(i, _, _)| *i == id).map(|(_, p, h)| (*p, *h))
    };
    for edge in &state.stacking_edges {
        if let (Some((a, ah)), Some((b, bh))) = (pos_of(edge.from), pos_of(edge.to)) {
            let from_p = Pos2::new(a.x + NODE_W, a.y + ah / 2.0);
            // Find the destination node so we can look up the right port y.
            let dest = state.stacking_nodes.iter().find(|n| n.id == edge.to);
            let to_p = match dest {
                Some(slot) => port_pos(slot, b, bh, edge.to_port, true),
                None => Pos2::new(b.x, b.y + bh / 2.0),
            };
            draw_edge(&painter, from_p, to_p, state.theme.accent_dim);
        }
    }
    if let Some((src, _)) = state.stacking_pending_edge {
        if let (Some((a, ah)), Some(p)) = (pos_of(src), pointer) {
            let from_p = Pos2::new(a.x + NODE_W, a.y + ah / 2.0);
            draw_edge(&painter, from_p, p, state.theme.accent);
        }
    }

    // -- Node bodies / titles / ports.
    let mut delete_id: Option<u64> = None;
    // Per-frame port registry — populated during the node loop, consumed
    // afterwards for hit-testing the wire drag.
    let mut output_ports: Vec<PortHit> = Vec::new();
    let mut input_ports: Vec<PortHit> = Vec::new();
    let mut save_request: Option<String> = None;
    let mut open_popup_for: Option<u64> = None;

    let node_count = state.stacking_nodes.len();
    for i in 0..node_count {
        let (node_id, node_screen, body_height) = {
            let n = &state.stacking_nodes[i];
            (n.id, to_screen(origin, pan, zoom, n.pos), slot_height(n))
        };
        // Node frames are fixed-size on screen — zoom only moves them.
        let node_rect = Rect::from_min_size(node_screen, Vec2::new(NODE_W, body_height));
        let title_rect = Rect::from_min_size(node_screen, Vec2::new(NODE_W, TITLE_H));
        let body_rect = Rect::from_min_max(
            Pos2::new(node_screen.x, node_screen.y + TITLE_H),
            node_rect.max,
        );

        let selected = state.stacking_selection.contains(&node_id);

        // Title bar — drag handle + select.
        let title_resp = ui.allocate_rect(title_rect, Sense::click_and_drag());
        if title_resp.drag_started() {
            // Selection handling: if not already in selection, replace it
            // unless shift/cmd held.
            let mods = ui.input(|i| i.modifiers);
            if !selected {
                if !(mods.shift || mods.command || mods.ctrl) {
                    state.stacking_selection.clear();
                }
                state.stacking_selection.insert(node_id);
            }
            let grab = pointer.unwrap_or(title_rect.min) - title_rect.min;
            state.stacking_drag = Some((node_id, Vec2::new(-grab.x, -grab.y) / zoom));
        }
        if title_resp.dragged() {
            if let (Some((drag_id, offset)), Some(p)) = (state.stacking_drag, pointer) {
                if drag_id == node_id {
                    // Pointer in graph-local coords + the original
                    // drag-start grab offset. No clamping — nodes can
                    // be parked anywhere in graph space, the user can
                    // pan to find them.
                    let new_local = Pos2::new(
                        (p.x - origin.x - pan.x) / zoom + offset.x,
                        (p.y - origin.y - pan.y) / zoom + offset.y,
                    );
                    let leader_old = state.stacking_nodes[i].pos;
                    let delta = new_local - leader_old;
                    if state.stacking_selection.len() > 1 && state.stacking_selection.contains(&node_id) {
                        // Move every selected node by the same delta.
                        for slot in state.stacking_nodes.iter_mut() {
                            if state.stacking_selection.contains(&slot.id) {
                                slot.pos += delta;
                            }
                        }
                    } else if let Some(slot) = state.stacking_nodes.get_mut(i) {
                        slot.pos = new_local;
                    }
                }
            }
        }
        if title_resp.drag_stopped() {
            state.stacking_drag = None;
        }
        if title_resp.clicked() {
            // Toggle popup on title click. With Shift: toggle in selection.
            let mods = ui.input(|i| i.modifiers);
            if mods.shift || mods.command || mods.ctrl {
                if !state.stacking_selection.remove(&node_id) {
                    state.stacking_selection.insert(node_id);
                }
            } else {
                state.stacking_selection.clear();
                state.stacking_selection.insert(node_id);
                // Source nodes pop up the group browser.
                if matches!(&state.stacking_nodes[i].node, StackingNode::Source(_)) {
                    open_popup_for = Some(node_id);
                }
            }
        }
        title_resp.context_menu(|ui| {
            if ui.button("Delete node").clicked() {
                delete_id = Some(node_id);
                ui.close_menu();
            }
            if state.stacking_selection.len() > 1
                && ui.button(format!("Delete {} selected", state.stacking_selection.len())).clicked()
            {
                let to_remove: Vec<u64> =
                    state.stacking_selection.iter().copied().collect();
                for id in &to_remove {
                    state.stacking_nodes.retain(|n| n.id != *id);
                    state.stacking_edges.retain(|e| e.from != *id && e.to != *id);
                }
                state.stacking_selection.clear();
                state.stacking_preview.dirty = true;
                ui.close_menu();
            }
        });

        // Body frame.
        let body_color = node_accent(&state.stacking_nodes[i].node, &state.theme);
        let outline = if selected { state.theme.accent } else { body_color };
        ui.painter().rect(
            node_rect,
            3.0,
            state.theme.panel,
            Stroke::new(if selected { 2.0 } else { 1.0 }, outline),
            StrokeKind::Inside,
        );
        ui.painter().rect_filled(title_rect, 3.0, body_color);
        ui.painter().text(
            title_rect.center(),
            egui::Align2::CENTER_CENTER,
            state.stacking_nodes[i].node.label(),
            FontId::proportional(10.0),
            state.theme.canvas,
        );

        // Body rendered in its own foreground Area so widget hits land
        // before the canvas drag handler eats them.
        let body_inner = body_rect.shrink(BODY_PAD);
        let mut measured = 0.0_f32;
        let mut node = state.stacking_nodes[i].node.clone();
        let prev = node.clone();
        let area_id = egui::Id::new(("stacking_node_body", node_id));
        egui::Area::new(area_id)
            .fixed_pos(body_inner.min)
            .order(egui::Order::Middle)
            .show(ui.ctx(), |ui| {
                ui.set_clip_rect(body_inner);
                ui.set_max_size(body_inner.size());
                ui.spacing_mut().item_spacing.y = 3.0;
                match &mut node {
                    StackingNode::Source(s) => {
                        if show_source_body(ui, state, s) {
                            open_popup_for = Some(node_id);
                        }
                    }
                    StackingNode::Stack(s) => show_stack_body(ui, state, s),
                    StackingNode::Adjust(a) => show_adjust_body(ui, state, a),
                    StackingNode::Output(o) => {
                        if show_output_body(ui, state, o) {
                            save_request = Some(o.filename.clone());
                        }
                    }
                }
                measured = ui.min_rect().height();
            });

        if !node_eq(&prev, &node) {
            state.stacking_preview.dirty = true;
        }
        if let Some(slot) = state.stacking_nodes.get_mut(i) {
            slot.node = node;
            let floor = fallback_body_height(&slot.node);
            let want = (measured / zoom).max(floor);
            let prev_h = slot.measured_height.unwrap_or(0.0);
            if (want - prev_h).abs() > 0.5 {
                slot.measured_height = Some(want);
            }
        }

        // -- Ports. Node frames are fixed-size on screen; port
        // positions don't depend on zoom either.
        let slot_ref = &state.stacking_nodes[i];
        let body_h_screen = slot_height(slot_ref);

        if !matches!(slot_ref.node, StackingNode::Output(_)) {
            let p = port_pos(slot_ref, node_screen, body_h_screen, In::Default, false);
            output_ports.push(PortHit { node_id, port: In::Default, center: p, color: state.theme.accent });
        }
        match &slot_ref.node {
            StackingNode::Source(_) => {}
            StackingNode::Stack(_) => {
                for port in [In::Lights, In::Darks] {
                    let p = port_pos(slot_ref, node_screen, body_h_screen, port, true);
                    let color = match port {
                        In::Darks => state.theme.error,
                        _ => state.theme.accent,
                    };
                    input_ports.push(PortHit { node_id, port, center: p, color });
                }
            }
            _ => {
                let p = port_pos(slot_ref, node_screen, body_h_screen, In::Default, true);
                input_ports.push(PortHit { node_id, port: In::Default, center: p, color: state.theme.accent });
            }
        }
    }

    // -- Wire creation: render+interact ports last so they win the
    // click priority dance with the canvas-wide drag handler.
    handle_ports(ui, &output_ports, &input_ports, state);

    if let Some(id) = delete_id {
        state.stacking_nodes.retain(|n| n.id != id);
        state.stacking_edges.retain(|e| e.from != id && e.to != id);
        state.stacking_selection.remove(&id);
        state.stacking_preview.dirty = true;
    }

    if let Some(name) = save_request {
        stacking::save_output(state, &name);
    }

    if canvas_resp.clicked() && state.stacking_pending_edge.is_some() {
        state.stacking_pending_edge = None;
    }

    // Group browser popup (if any).
    if let Some(node_id) = open_popup_for {
        state.stacking_open_popup = Some(node_id);
    }
    show_group_popup(ui, rect, state);

    if state.stacking_nodes.is_empty() {
        ui.painter().text(
            rect.center(),
            egui::Align2::CENTER_CENTER,
            "Right-click to add a node",
            FontId::proportional(11.0),
            state.theme.text_faint,
        );
    }
}

/// Pan & zoom the node-editor canvas. Driven by raw input so middle-
/// mouse drags work (egui's `Sense::click_and_drag()` only listens for
/// primary-button drags).
///
/// - Middle-button drag, or Space + Left-button drag, pans.
/// - Scroll wheel zooms toward the cursor (no modifier needed — the
///   canvas owns the wheel when the pointer is over it; widgets inside
///   nodes still get their own scroll because their Areas are
///   foreground and consume the event first).
/// - Ctrl/Cmd + scroll also zooms (kept for muscle memory).
fn handle_pan_zoom(ui: &mut Ui, rect: Rect, state: &mut AppState) {
    let pointer = ui.input(|i| i.pointer.interact_pos());
    let pointer_in_rect = pointer.map(|p| rect.contains(p)).unwrap_or(false);

    // Pan with middle button anywhere it's pressed (we don't gate on
    // hover so drags that briefly leave the panel keep tracking).
    let middle_down = ui.input(|i| i.pointer.middle_down());
    let primary_down = ui.input(|i| i.pointer.primary_down());
    let space_down = ui.input(|i| i.key_down(egui::Key::Space));

    let want_pan = middle_down || (primary_down && space_down);
    let pointer_delta = ui.input(|i| i.pointer.delta());

    // Track an edge-triggered "panning" flag so we don't accidentally
    // pan when the cursor enters the rect mid-drag started elsewhere.
    let id = egui::Id::new("__stacking_panning");
    let was_panning = ui.ctx().data(|d| d.get_temp::<bool>(id).unwrap_or(false));

    // Pan starts when a button goes down inside the rect.
    let middle_pressed = ui.input(|i| i.pointer.button_pressed(egui::PointerButton::Middle));
    let primary_pressed = ui.input(|i| i.pointer.button_pressed(egui::PointerButton::Primary));
    let press_in_rect = (middle_pressed || (primary_pressed && space_down)) && pointer_in_rect;

    let panning = if press_in_rect {
        true
    } else if !want_pan {
        false
    } else {
        was_panning
    };
    ui.ctx().data_mut(|d| d.insert_temp(id, panning));

    if panning {
        state.stacking_pan += pointer_delta;
        ui.ctx().set_cursor_icon(egui::CursorIcon::Grabbing);
    }

    // Zoom: scroll while hovering the rect.
    if pointer_in_rect {
        let scroll = ui.input(|i| i.smooth_scroll_delta.y);
        if scroll.abs() > 0.1 {
            let pointer_pos = pointer.unwrap_or(rect.center());
            let old_zoom = state.stacking_zoom;
            let new_zoom = (old_zoom * (1.0 + scroll * 0.0025)).clamp(MIN_ZOOM, MAX_ZOOM);
            if (new_zoom - old_zoom).abs() > 1e-4 {
                // Keep the graph point under the cursor stable.
                let pre = to_graph(rect.min, state.stacking_pan, old_zoom, pointer_pos);
                state.stacking_zoom = new_zoom;
                let post = to_screen(rect.min, state.stacking_pan, new_zoom, pre);
                state.stacking_pan += pointer_pos - post;
            }
        }
    }
}

/// One port we collected during the node-render pass; used to draw the
/// circle and (for outputs) own a drag interaction.
struct PortHit {
    node_id: u64,
    port: In,
    center: Pos2,
    color: Color32,
}

/// Render & interact with all node ports. Outputs sense drag (start a
/// pending edge); inputs are visual only — we look up the input port
/// under the pointer at drag-stop and create the edge then.
fn handle_ports(
    ui: &mut Ui,
    outputs: &[PortHit],
    inputs: &[PortHit],
    state: &mut AppState,
) {
    let port_hit_radius = (PORT_R * 3.0).max(10.0);

    // Inputs: paint them and remember "hot" highlight when an edge is
    // being dragged onto them.
    let pending = state.stacking_pending_edge.is_some();
    let pointer_pos = ui.input(|i| i.pointer.interact_pos());
    let hot_input: Option<(u64, In)> = pending.then(|| {
        pointer_pos.and_then(|p| {
            inputs
                .iter()
                .find(|h| (h.center - p).length() <= port_hit_radius)
                .map(|h| (h.node_id, h.port))
        })
    }).flatten();

    let mut clicked_input: Option<(u64, In)> = None;
    for hit in inputs {
        let glow = hot_input == Some((hit.node_id, hit.port));
        let r = Rect::from_center_size(hit.center, Vec2::splat(port_hit_radius));
        let area_id = egui::Id::new(("stacking_input_port", hit.node_id, port_key(hit.port)));
        egui::Area::new(area_id)
            .fixed_pos(r.min)
            .order(egui::Order::Foreground)
            .show(ui.ctx(), |ui| {
                let resp = ui.allocate_rect(r, Sense::click());
                let stroke_w = if glow || resp.hovered() { 2.0 } else { 1.0 };
                ui.painter().circle_filled(hit.center, PORT_R, state.theme.accent_dim);
                ui.painter()
                    .circle_stroke(hit.center, PORT_R, Stroke::new(stroke_w, hit.color));
                if resp.clicked() {
                    clicked_input = Some((hit.node_id, hit.port));
                }
            });
        let label = match hit.port {
            In::Darks => Some("darks"),
            In::Lights => Some("lights"),
            In::Default => None,
        };
        if let Some(label) = label {
            ui.painter().text(
                Pos2::new(hit.center.x - 8.0, hit.center.y),
                egui::Align2::RIGHT_CENTER,
                label,
                FontId::proportional(8.0),
                state.theme.text_faint,
            );
        }
    }

    // Outputs: own a drag interaction so we can start a wire by
    // pulling from the port. We render in a foreground Area so the
    // canvas drag handler doesn't eat the click. Each port gets its
    // own area / id.
    let mut start_drag_from: Option<u64> = None;
    let mut release_pos: Option<Pos2> = None;

    for hit in outputs {
        let r = Rect::from_center_size(hit.center, Vec2::splat(port_hit_radius));
        let area_id = egui::Id::new(("stacking_output_port", hit.node_id));
        egui::Area::new(area_id)
            .fixed_pos(r.min)
            .order(egui::Order::Foreground)
            .show(ui.ctx(), |ui| {
                let resp = ui.allocate_rect(r, Sense::click_and_drag());
                ui.painter().circle_filled(hit.center, PORT_R, state.theme.accent_dim);
                ui.painter().circle_stroke(
                    hit.center,
                    PORT_R,
                    Stroke::new(if resp.hovered() { 2.0 } else { 1.0 }, hit.color),
                );

                if resp.drag_started() || resp.clicked() {
                    start_drag_from = Some(hit.node_id);
                }
                if resp.drag_stopped() {
                    release_pos = ui.input(|i| i.pointer.interact_pos());
                }
            });
    }

    // Begin a pending edge.
    if let Some(src) = start_drag_from {
        state.stacking_pending_edge = Some((src, Pos2::ZERO));
    }

    // End-of-drag drop: if the user released the wire over an input,
    // commit the edge.
    if let (Some(p), Some((src, _))) = (release_pos, state.stacking_pending_edge) {
        let target = inputs
            .iter()
            .find(|h| (h.center - p).length() <= port_hit_radius)
            .map(|h| (h.node_id, h.port));
        match target {
            Some((dst, port)) if dst != src => {
                if !state
                    .stacking_edges
                    .iter()
                    .any(|e| e.from == src && e.to == dst && e.to_port == port)
                {
                    state.stacking_edges.push(StackingEdge {
                        from: src,
                        to: dst,
                        to_port: port,
                    });
                    state.stacking_preview.dirty = true;
                }
                state.stacking_pending_edge = None;
            }
            _ => {
                // Released elsewhere — cancel the pending wire.
                state.stacking_pending_edge = None;
            }
        }
    }

    // Click-click fallback: clicking an input while a wire is pending
    // commits it. Lets users wire without holding the button.
    if let Some((dst, port)) = clicked_input {
        if let Some((src, _)) = state.stacking_pending_edge {
            if dst != src
                && !state
                    .stacking_edges
                    .iter()
                    .any(|e| e.from == src && e.to == dst && e.to_port == port)
            {
                state.stacking_edges.push(StackingEdge {
                    from: src,
                    to: dst,
                    to_port: port,
                });
                state.stacking_preview.dirty = true;
            }
            state.stacking_pending_edge = None;
        }
    }
}

fn port_key(p: In) -> u8 {
    match p {
        In::Default => 0,
        In::Lights => 1,
        In::Darks => 2,
    }
}

/// Position of a node port in screen coordinates. Stack node: lights at
/// 1/3 height, darks at 2/3. Other nodes: vertical centre.
///
/// Sizes are in fixed screen pixels — node frames don't scale with
/// zoom, so neither do port positions relative to the frame.
fn port_pos(slot: &StackingNodeSlot, node_screen: Pos2, body_h_screen: f32, port: In, is_input: bool) -> Pos2 {
    let x = if is_input {
        node_screen.x
    } else {
        node_screen.x + NODE_W
    };
    let y = match (&slot.node, port) {
        (StackingNode::Stack(_), In::Darks) => node_screen.y + body_h_screen * 0.66,
        (StackingNode::Stack(_), In::Lights) => node_screen.y + body_h_screen * 0.33,
        _ => node_screen.y + body_h_screen / 2.0,
    };
    Pos2::new(x, y)
}

/// Find a node whose title bar contains the screen point. Used so a
/// box-select drag won't start when we're really initiating a node drag.
fn node_under(state: &AppState, origin: Pos2, pan: Vec2, zoom: f32, p: Pos2) -> Option<u64> {
    for slot in &state.stacking_nodes {
        let s = to_screen(origin, pan, zoom, slot.pos);
        let r = Rect::from_min_size(s, Vec2::new(NODE_W, slot_height(slot)));
        if r.contains(p) {
            return Some(slot.id);
        }
    }
    None
}

fn push_node(state: &mut AppState, pos: Pos2, node: StackingNode) {
    let id = state.stacking_next_node_id;
    state.stacking_next_node_id += 1;
    state.stacking_nodes.push(StackingNodeSlot {
        id,
        pos,
        node,
        measured_height: None,
    });
    state.stacking_preview.dirty = true;
}

// ---------------------------------------------------------------------------
// Per-node body renderers
// ---------------------------------------------------------------------------

/// Source: thumb strip + group selector + browse button. Returns true if
/// the body wants the popup opened (e.g. clicking a thumb that's not
/// fully visible).
fn show_source_body(ui: &mut Ui, state: &AppState, s: &mut SourceNode) -> bool {
    let mut want_popup = false;
    let bound = s.collection_id.and_then(|id| state.collection(id));
    let label = match bound {
        Some(c) => format!("{} — {} ({})", c.kind.label(), c.name, c.items.len()),
        None => "(no group)".to_string(),
    };

    egui::ComboBox::from_id_salt(("stacking_source_pick", s.collection_id))
        .selected_text(egui::RichText::new(label).size(10.0).color(state.theme.text))
        .width(160.0)
        .show_ui(ui, |ui| {
            for c in &state.collections {
                let selected = s.collection_id == Some(c.id);
                let label = format!("{} — {} ({})", c.kind.label(), c.name, c.items.len());
                if ui.selectable_label(selected, label).clicked() {
                    s.collection_id = Some(c.id);
                }
            }
            if state.collections.is_empty() {
                ui.label(
                    egui::RichText::new("(no groups yet)")
                        .size(10.0)
                        .color(state.theme.text_faint),
                );
            }
        });

    // Thumbnail strip — first ~6 thumbs.
    if let Some(c) = bound {
        let thumb = 22.0;
        let max_thumbs = 6;
        ui.horizontal(|ui| {
            for (i, item) in c.items.iter().enumerate().take(max_thumbs) {
                let (rect, resp) = ui.allocate_exact_size(Vec2::splat(thumb), Sense::click());
                let painter = ui.painter();
                if let Some(tex) = state.thumbnail_cache.peek(&item.asset_id) {
                    let ts = tex.size_vec2();
                    let scale = (thumb / ts.x).min(thumb / ts.y);
                    let display = ts * scale;
                    let centered = Rect::from_center_size(rect.center(), display);
                    painter.image(
                        tex.id(),
                        centered,
                        Rect::from_min_max(Pos2::ZERO, Pos2::new(1.0, 1.0)),
                        Color32::WHITE,
                    );
                } else {
                    painter.rect_filled(rect, 1.0, state.theme.canvas);
                }
                if !item.accepted {
                    painter.line_segment(
                        [rect.left_top(), rect.right_bottom()],
                        Stroke::new(1.0, state.theme.error),
                    );
                }
                painter.rect_stroke(rect, 1.0, Stroke::new(0.5, state.theme.border), StrokeKind::Inside);
                let _ = i;
                if resp.clicked() {
                    want_popup = true;
                }
            }
            if c.items.len() > max_thumbs {
                ui.label(
                    egui::RichText::new(format!("+{}", c.items.len() - max_thumbs))
                        .size(10.0)
                        .color(state.theme.text_dim),
                );
            }
        });
    }

    if ui.small_button("Browse…").clicked() {
        want_popup = true;
    }

    want_popup
}

fn show_stack_body(ui: &mut Ui, state: &AppState, s: &mut StackNode) {
    ui.spacing_mut().item_spacing.y = 2.0;
    ui.horizontal(|ui| {
        ui.label(
            egui::RichText::new("method").size(10.0).color(state.theme.text_dim),
        );
        for (m, label) in [
            (StackMethod::Mean, "mean"),
            (StackMethod::Median, "median"),
            (StackMethod::Max, "max"),
        ] {
            let active = s.method == m;
            if ui.selectable_label(active, egui::RichText::new(label).size(10.0)).clicked() {
                s.method = m;
            }
        }
    });
    ui.checkbox(&mut s.subtract_darks, egui::RichText::new("subtract darks").size(10.0));

    // Sanity-check diagnostics from the last successful run. Helps the
    // user catch "I added darks but nothing happened" sorts of issues
    // without cracking open the log panel.
    if let Some(d) = &state.stacking_preview.diagnostics {
        let line = format!(
            "{}L · {}D · {} hot · {}×{}",
            d.lights, d.darks, d.hot_pixels, d.width, d.height,
        );
        ui.label(
            egui::RichText::new(line)
                .size(9.0)
                .monospace()
                .color(state.theme.text_dim),
        );
    }
}

fn show_adjust_body(ui: &mut Ui, state: &AppState, a: &mut AdjustNode) {
    ui.spacing_mut().item_spacing.y = 1.0;
    ui.spacing_mut().slider_width = 90.0;
    slider_row(ui, state, "exposure", &mut a.exposure, -2.0..=2.0);
    slider_row(ui, state, "gamma", &mut a.gamma, 0.1..=3.0);
    slider_row(ui, state, "saturation", &mut a.saturation, 0.0..=2.0);
    slider_row(ui, state, "hue", &mut a.hue_shift, -180.0..=180.0);
}

fn slider_row(
    ui: &mut Ui,
    state: &AppState,
    label: &str,
    v: &mut f32,
    range: std::ops::RangeInclusive<f32>,
) {
    ui.horizontal(|ui| {
        ui.label(
            egui::RichText::new(label)
                .size(9.0)
                .color(state.theme.text_dim),
        );
        ui.add(
            egui::Slider::new(v, range)
                .show_value(true)
                .trailing_fill(true),
        );
    });
}

fn show_output_body(ui: &mut Ui, state: &AppState, o: &mut OutputNode) -> bool {
    ui.spacing_mut().item_spacing.y = 2.0;
    ui.label(
        egui::RichText::new("filename")
            .size(9.0)
            .color(state.theme.text_dim),
    );
    ui.add(
        egui::TextEdit::singleline(&mut o.filename)
            .desired_width(140.0)
            .font(FontId::monospace(10.0)),
    );
    ui.small_button("Save").clicked()
}

// ---------------------------------------------------------------------------
// Group browser popup
// ---------------------------------------------------------------------------

fn show_group_popup(ui: &mut Ui, panel_rect: Rect, state: &mut AppState) {
    let Some(node_id) = state.stacking_open_popup else { return };
    let collection_id = state
        .stacking_nodes
        .iter()
        .find_map(|slot| match (slot.id == node_id, &slot.node) {
            (true, StackingNode::Source(s)) => s.collection_id,
            _ => None,
        });
    let Some(collection_id) = collection_id else {
        // Node was deleted; close.
        state.stacking_open_popup = None;
        return;
    };

    // Find a sensible position: the node's screen rect lower-left.
    let popup_pos = state
        .stacking_nodes
        .iter()
        .find(|n| n.id == node_id)
        .map(|n| {
            let s = to_screen(panel_rect.min, state.stacking_pan, state.stacking_zoom, n.pos);
            Pos2::new(s.x, s.y + slot_height(n) + 4.0)
        })
        .unwrap_or_else(|| panel_rect.center());

    let mut close = false;
    let resp = egui::Window::new("Group browser")
        .id(egui::Id::new(("stacking_group_popup", node_id)))
        .fixed_pos(popup_pos)
        .order(egui::Order::Foreground)
        .collapsible(false)
        .resizable(true)
        .default_width(360.0)
        .default_height(260.0)
        .show(ui.ctx(), |ui| {
            let collection = state.collection(collection_id).cloned();
            let Some(collection) = collection else { return };
            ui.horizontal(|ui| {
                ui.label(
                    egui::RichText::new(format!(
                        "{} — {}",
                        collection.kind.label(),
                        collection.name,
                    ))
                    .size(11.0)
                    .strong()
                    .color(state.theme.text),
                );
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if ui.small_button("Close").clicked() {
                        close = true;
                    }
                });
            });
            ui.separator();

            let thumb = 80.0;
            let columns = (ui.available_width() / (thumb + 6.0)).floor().max(1.0) as usize;

            egui::ScrollArea::vertical().show(ui, |ui| {
                let row_count = (collection.items.len() + columns - 1) / columns;
                let mut toggles: Vec<usize> = Vec::new();
                let mut removes: Vec<usize> = Vec::new();
                for row in 0..row_count {
                    ui.horizontal(|ui| {
                        for col in 0..columns {
                            let idx = row * columns + col;
                            if idx >= collection.items.len() {
                                break;
                            }
                            let item = &collection.items[idx];
                            let (rect, resp) =
                                ui.allocate_exact_size(Vec2::splat(thumb), Sense::click());
                            let painter = ui.painter();
                            if let Some(tex) = state.thumbnail_cache.peek(&item.asset_id) {
                                let ts = tex.size_vec2();
                                let scale = (thumb / ts.x).min(thumb / ts.y);
                                let display = ts * scale;
                                let centered =
                                    Rect::from_center_size(rect.center(), display);
                                painter.image(
                                    tex.id(),
                                    centered,
                                    Rect::from_min_max(Pos2::ZERO, Pos2::new(1.0, 1.0)),
                                    Color32::WHITE,
                                );
                            } else {
                                painter.rect_filled(rect, 2.0, state.theme.canvas);
                                painter.text(
                                    rect.center(),
                                    egui::Align2::CENTER_CENTER,
                                    "...",
                                    FontId::proportional(10.0),
                                    state.theme.text_faint,
                                );
                            }
                            if !item.accepted {
                                painter.rect_filled(
                                    rect,
                                    2.0,
                                    Color32::from_rgba_unmultiplied(20, 8, 8, 160),
                                );
                                painter.line_segment(
                                    [rect.left_top(), rect.right_bottom()],
                                    Stroke::new(2.0, state.theme.error),
                                );
                                painter.line_segment(
                                    [rect.right_top(), rect.left_bottom()],
                                    Stroke::new(2.0, state.theme.error),
                                );
                            }
                            let border = if item.accepted { state.theme.border } else { state.theme.error };
                            painter.rect_stroke(rect, 2.0, Stroke::new(1.0, border), StrokeKind::Inside);

                            // Filename below thumb.
                            painter.text(
                                Pos2::new(rect.center().x, rect.max.y + 9.0),
                                egui::Align2::CENTER_TOP,
                                trim(&item.filename, 14),
                                FontId::proportional(9.0),
                                state.theme.text_dim,
                            );

                            if resp.clicked() {
                                toggles.push(idx);
                            }
                            resp.context_menu(|ui| {
                                if ui.button("Remove from group").clicked() {
                                    removes.push(idx);
                                    ui.close_menu();
                                }
                            });
                            ui.add_space(4.0);
                        }
                    });
                    ui.add_space(18.0);
                }

                if !toggles.is_empty() || !removes.is_empty() {
                    if let Some(coll) = state.collection_mut(collection_id) {
                        for idx in toggles {
                            if let Some(item) = coll.items.get_mut(idx) {
                                item.accepted = !item.accepted;
                            }
                        }
                        // Apply removes in reverse so indices stay valid.
                        let mut sorted = removes;
                        sorted.sort_unstable();
                        sorted.dedup();
                        for idx in sorted.into_iter().rev() {
                            if idx < coll.items.len() {
                                coll.items.remove(idx);
                            }
                        }
                    }
                    state.stacking_preview.dirty = true;
                }
            });
        });

    // Click-outside-to-close: if the user clicked elsewhere, close.
    let pointer_pressed = ui.input(|i| i.pointer.any_pressed());
    if pointer_pressed {
        if let (Some(rect), Some(p)) = (resp.map(|r| r.response.rect), ui.input(|i| i.pointer.interact_pos())) {
            if !rect.contains(p) {
                close = true;
            }
        }
    }
    if close {
        state.stacking_open_popup = None;
    }
}


// ---------------------------------------------------------------------------
// Misc
// ---------------------------------------------------------------------------

fn node_accent(node: &StackingNode, theme: &crate::theme::Theme) -> Color32 {
    match node {
        StackingNode::Source(_) => theme.success,
        StackingNode::Stack(_) => theme.accent,
        StackingNode::Adjust(_) => theme.info,
        StackingNode::Output(_) => theme.text_dim,
    }
}

fn node_eq(a: &StackingNode, b: &StackingNode) -> bool {
    match (a, b) {
        (StackingNode::Source(x), StackingNode::Source(y)) => x.collection_id == y.collection_id,
        (StackingNode::Stack(x), StackingNode::Stack(y)) => {
            x.method == y.method && x.subtract_darks == y.subtract_darks
        }
        (StackingNode::Adjust(x), StackingNode::Adjust(y)) => {
            x.exposure == y.exposure
                && x.gamma == y.gamma
                && x.saturation == y.saturation
                && x.hue_shift == y.hue_shift
        }
        (StackingNode::Output(x), StackingNode::Output(y)) => x.filename == y.filename,
        _ => false,
    }
}

fn draw_edge(painter: &egui::Painter, from: Pos2, to: Pos2, color: Color32) {
    let dx = (to.x - from.x).abs().max(40.0) * 0.5;
    let c1 = Pos2::new(from.x + dx, from.y);
    let c2 = Pos2::new(to.x - dx, to.y);
    let stroke = Stroke::new(1.5, color);
    let segments = 24;
    let mut prev = from;
    for i in 1..=segments {
        let t = i as f32 / segments as f32;
        let p = cubic_bezier(from, c1, c2, to, t);
        painter.line_segment([prev, p], stroke);
        prev = p;
    }
}

/// Does the user's slice segment (`s`–`e`, screen coords) intersect
/// the visual path of `edge`? We sample the bezier the same way
/// `draw_edge` does and check segment-segment intersection. Cheap and
/// accurate enough — edges are short.
fn edge_crosses_segment(
    state: &AppState,
    origin: Pos2,
    pan: Vec2,
    zoom: f32,
    edge: crate::state::StackingEdge,
    s: Pos2,
    e: Pos2,
) -> bool {
    let from_slot = state.stacking_nodes.iter().find(|n| n.id == edge.from);
    let to_slot = state.stacking_nodes.iter().find(|n| n.id == edge.to);
    let (Some(from_slot), Some(to_slot)) = (from_slot, to_slot) else {
        return false;
    };
    let from_screen = to_screen(origin, pan, zoom, from_slot.pos);
    let to_screen_pt = to_screen(origin, pan, zoom, to_slot.pos);
    let from_h = slot_height(from_slot);
    let to_h = slot_height(to_slot);
    let from = Pos2::new(from_screen.x + NODE_W, from_screen.y + from_h / 2.0);
    let to = port_pos(to_slot, to_screen_pt, to_h, edge.to_port, true);

    let dx = (to.x - from.x).abs().max(40.0) * 0.5;
    let c1 = Pos2::new(from.x + dx, from.y);
    let c2 = Pos2::new(to.x - dx, to.y);
    let segments = 24;
    let mut prev = from;
    for i in 1..=segments {
        let t = i as f32 / segments as f32;
        let p = cubic_bezier(from, c1, c2, to, t);
        if segments_intersect(prev, p, s, e) {
            return true;
        }
        prev = p;
    }
    false
}

/// Standard segment-segment intersection test. Returns true if (a1,a2)
/// crosses (b1,b2) anywhere in their interiors.
fn segments_intersect(a1: Pos2, a2: Pos2, b1: Pos2, b2: Pos2) -> bool {
    fn ccw(a: Pos2, b: Pos2, c: Pos2) -> f32 {
        (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x)
    }
    let d1 = ccw(b1, b2, a1);
    let d2 = ccw(b1, b2, a2);
    let d3 = ccw(a1, a2, b1);
    let d4 = ccw(a1, a2, b2);
    ((d1 > 0.0 && d2 < 0.0) || (d1 < 0.0 && d2 > 0.0))
        && ((d3 > 0.0 && d4 < 0.0) || (d3 < 0.0 && d4 > 0.0))
}

fn cubic_bezier(p0: Pos2, p1: Pos2, p2: Pos2, p3: Pos2, t: f32) -> Pos2 {
    let u = 1.0 - t;
    let b0 = u * u * u;
    let b1 = 3.0 * u * u * t;
    let b2 = 3.0 * u * t * t;
    let b3 = t * t * t;
    Pos2::new(
        b0 * p0.x + b1 * p1.x + b2 * p2.x + b3 * p3.x,
        b0 * p0.y + b1 * p1.y + b2 * p2.y + b3 * p3.y,
    )
}

// Suppress unused-import warnings when only some of these are used by
// the thinned compile path.
#[allow(unused_imports)]
use CollectionItem as _CollectionItem;
#[allow(unused_imports)]
use CollectionKind as _CollectionKind;

/// Binary Space Partitioning layout engine.
///
/// Implements a Blender-style panel system where the workspace is recursively
/// subdivided into regions. Each leaf holds a panel; splits can be resized by
/// dragging the divider. Panel types are selectable via a dropdown in the
/// header. Right-click context menu provides split/close operations.

use egui::{Rect, Pos2, Vec2, Ui, Sense, CursorIcon, Stroke, StrokeKind};
use crate::panels::PanelKind;
use crate::theme::Theme;

/// Unique identifier for a leaf panel.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, serde::Serialize, serde::Deserialize)]
pub struct PanelId(pub u64);

impl PanelId {
    pub fn next() -> Self {
        use std::sync::atomic::{AtomicU64, Ordering};
        static COUNTER: AtomicU64 = AtomicU64::new(1);
        Self(COUNTER.fetch_add(1, Ordering::Relaxed))
    }
}

/// Direction of a split.
#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub enum SplitDirection {
    Horizontal, // left | right
    Vertical,   // top / bottom
}

/// A node in the BSP tree.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub enum BspNode {
    Leaf {
        id: PanelId,
        panel: PanelKind,
    },
    Split {
        direction: SplitDirection,
        /// Position of the divider, 0.0..1.0.
        ratio: f32,
        first: Box<BspNode>,
        second: Box<BspNode>,
    },
}

/// Thickness of the draggable divider between panels.
const DIVIDER_THICKNESS: f32 = 4.0;

/// Minimum panel size in pixels.
const MIN_PANEL_SIZE: f32 = 80.0;

/// Height of the panel header bar.
const HEADER_HEIGHT: f32 = 22.0;

/// Result of a BSP interaction: what the caller should do.
pub enum BspAction {
    None,
    /// Split the panel with the given ID.
    Split {
        target: PanelId,
        direction: SplitDirection,
    },
    /// Merge the panel into its sibling.
    Merge { target: PanelId },
}

impl BspNode {
    /// Create the default "Library" layout.
    pub fn default_layout() -> Self {
        BspNode::Split {
            direction: SplitDirection::Vertical,
            ratio: 0.75,
            first: Box::new(BspNode::Split {
                direction: SplitDirection::Horizontal,
                ratio: 0.65,
                first: Box::new(BspNode::Leaf {
                    id: PanelId::next(),
                    panel: PanelKind::ImageViewer,
                }),
                second: Box::new(BspNode::Split {
                    direction: SplitDirection::Vertical,
                    ratio: 0.5,
                    first: Box::new(BspNode::Leaf {
                        id: PanelId::next(),
                        panel: PanelKind::ExifInspector,
                    }),
                    second: Box::new(BspNode::Leaf {
                        id: PanelId::next(),
                        panel: PanelKind::Histogram,
                    }),
                }),
            }),
            second: Box::new(BspNode::Leaf {
                id: PanelId::next(),
                panel: PanelKind::Timeline,
            }),
        }
    }

    /// Render the BSP tree into the given rect, returning any user action.
    ///
    /// `show_panel` is the per-panel renderer. It receives:
    /// - the `header_extra` rect — a strip *inside* the BSP header bar
    ///   between the panel-type dropdown and the action buttons. Panels
    ///   are free to render controls there (asset pickers, search bars,
    ///   info chips, ...). When the panel doesn't need it, ignore it.
    /// - the `content` rect — everything below the header.
    pub fn show(
        &mut self,
        ui: &mut Ui,
        rect: Rect,
        theme: &Theme,
        show_panel: &mut dyn FnMut(&mut Ui, PanelId, &PanelKind, Rect, Rect),
    ) -> BspAction {
        match self {
            BspNode::Leaf { id, panel } => {
                Self::show_leaf(ui, *id, panel, rect, theme, show_panel)
            }
            BspNode::Split { direction, ratio, first, second } => {
                Self::show_split(ui, *direction, ratio, first, second, rect, theme, show_panel)
            }
        }
    }

    fn show_leaf(
        ui: &mut Ui,
        id: PanelId,
        panel: &mut PanelKind,
        rect: Rect,
        theme: &Theme,
        show_panel: &mut dyn FnMut(&mut Ui, PanelId, &PanelKind, Rect, Rect),
    ) -> BspAction {
        // Draw panel border.
        ui.painter().rect_stroke(
            rect,
            0.0,
            Stroke::new(1.0, theme.border),
            StrokeKind::Outside,
        );

        let header_rect = Rect::from_min_size(
            rect.min,
            Vec2::new(rect.width(), HEADER_HEIGHT),
        );
        let content_rect = Rect::from_min_max(
            Pos2::new(rect.min.x, rect.min.y + HEADER_HEIGHT),
            rect.max,
        );

        // Draw header background.
        ui.painter().rect_filled(header_rect, 0.0, theme.chrome);

        // Separator line below header.
        ui.painter().line_segment(
            [
                Pos2::new(header_rect.min.x, header_rect.max.y),
                Pos2::new(header_rect.max.x, header_rect.max.y),
            ],
            Stroke::new(1.0, theme.border),
        );

        // Panel type dropdown (top-left of header).
        let dropdown_width = 130.0_f32.min(rect.width() - 30.0);
        let dropdown_rect = Rect::from_min_size(
            Pos2::new(header_rect.min.x + 4.0, header_rect.min.y + 2.0),
            Vec2::new(dropdown_width, HEADER_HEIGHT - 4.0),
        );

        let combo_id = egui::Id::new("panel_type_combo").with(id.0);
        egui::Area::new(combo_id.with("area"))
            .fixed_pos(dropdown_rect.min)
            .order(egui::Order::Foreground)
            .show(ui.ctx(), |ui| {
                ui.set_clip_rect(ui.ctx().screen_rect()); // allow dropdown to overflow panel
                egui::ComboBox::from_id_salt(combo_id)
                    .selected_text(panel.name())
                    .width(dropdown_width - 8.0)
                    .show_ui(ui, |ui| {
                        for kind in PanelKind::ALL {
                            let is_selected = *panel == *kind;
                            if ui.selectable_label(is_selected, kind.name()).clicked() {
                                *panel = kind.clone();
                            }
                        }
                    });
            });

        // Header buttons (right side): [|] split vertical, [—] split horizontal, [×] close
        let mut action = BspAction::None;
        let btn_size = Vec2::new(HEADER_HEIGHT - 4.0, HEADER_HEIGHT - 4.0);
        let btn_y = header_rect.min.y + 2.0;
        let btn_spacing = 2.0;

        // × close button (rightmost)
        let close_rect = Rect::from_min_size(
            Pos2::new(header_rect.max.x - btn_size.x - 4.0, btn_y),
            btn_size,
        );
        let close_resp = ui.allocate_rect(close_rect, Sense::click());
        let close_color = if close_resp.hovered() {
            theme.error
        } else {
            theme.text_dim
        };
        ui.painter().text(
            close_rect.center(),
            egui::Align2::CENTER_CENTER,
            "×",
            egui::FontId::proportional(14.0),
            close_color,
        );
        if close_resp.clicked() {
            action = BspAction::Merge { target: id };
        }

        // — horizontal split button
        let hsplit_rect = Rect::from_min_size(
            Pos2::new(close_rect.min.x - btn_size.x - btn_spacing, btn_y),
            btn_size,
        );
        let hsplit_resp = ui.allocate_rect(hsplit_rect, Sense::click());
        let hsplit_color = if hsplit_resp.hovered() {
            theme.text
        } else {
            theme.text_dim
        };
        ui.painter().text(
            hsplit_rect.center(),
            egui::Align2::CENTER_CENTER,
            "—",
            egui::FontId::proportional(12.0),
            hsplit_color,
        );
        if hsplit_resp.on_hover_text("Split horizontal").clicked() {
            action = BspAction::Split { target: id, direction: SplitDirection::Vertical };
        }

        // | vertical split button
        let vsplit_rect = Rect::from_min_size(
            Pos2::new(hsplit_rect.min.x - btn_size.x - btn_spacing, btn_y),
            btn_size,
        );
        let vsplit_resp = ui.allocate_rect(vsplit_rect, Sense::click());
        let vsplit_color = if vsplit_resp.hovered() {
            theme.text
        } else {
            theme.text_dim
        };
        ui.painter().text(
            vsplit_rect.center(),
            egui::Align2::CENTER_CENTER,
            "|",
            egui::FontId::proportional(12.0),
            vsplit_color,
        );
        if vsplit_resp.on_hover_text("Split vertical").clicked() {
            action = BspAction::Split { target: id, direction: SplitDirection::Horizontal };
        }

        if !matches!(action, BspAction::None) {
            return action;
        }

        // Header-extra strip: lives between the type dropdown (left) and
        // the action buttons (right). When there's no room (extremely
        // narrow panel), pass an empty rect — panels are expected to
        // check `header_extra.width()` and bail.
        let buttons_left = vsplit_rect.min.x;
        let header_extra_left = dropdown_rect.max.x + 6.0;
        let header_extra_right = buttons_left - 4.0;
        let header_extra_rect = if header_extra_right > header_extra_left {
            Rect::from_min_max(
                Pos2::new(header_extra_left, header_rect.min.y + 2.0),
                Pos2::new(header_extra_right, header_rect.max.y - 2.0),
            )
        } else {
            Rect::NOTHING
        };

        // Render the actual panel content + header extras.
        show_panel(ui, id, panel, header_extra_rect, content_rect);

        BspAction::None
    }

    fn show_split(
        ui: &mut Ui,
        direction: SplitDirection,
        ratio: &mut f32,
        first: &mut BspNode,
        second: &mut BspNode,
        rect: Rect,
        theme: &Theme,
        show_panel: &mut dyn FnMut(&mut Ui, PanelId, &PanelKind, Rect, Rect),
    ) -> BspAction {
        let (first_rect, divider_rect, second_rect) =
            Self::compute_rects(direction, *ratio, rect);

        // Draggable divider.
        let divider_response = ui.allocate_rect(divider_rect, Sense::drag());
        if divider_response.hovered() {
            ui.ctx().set_cursor_icon(match direction {
                SplitDirection::Horizontal => CursorIcon::ResizeHorizontal,
                SplitDirection::Vertical => CursorIcon::ResizeVertical,
            });
        }
        if divider_response.dragged() {
            let delta = divider_response.drag_delta();
            let total = match direction {
                SplitDirection::Horizontal => rect.width(),
                SplitDirection::Vertical => rect.height(),
            };
            if total > 0.0 {
                let shift = match direction {
                    SplitDirection::Horizontal => delta.x / total,
                    SplitDirection::Vertical => delta.y / total,
                };
                *ratio = (*ratio + shift).clamp(
                    MIN_PANEL_SIZE / total,
                    1.0 - MIN_PANEL_SIZE / total,
                );
            }
        }

        // Draw divider.
        let divider_color = if divider_response.hovered() || divider_response.dragged() {
            theme.accent
        } else {
            theme.border
        };
        ui.painter().rect_filled(divider_rect, 0.0, divider_color);

        // Recurse into children.
        let action = first.show(ui, first_rect, theme, show_panel);
        if !matches!(action, BspAction::None) {
            return action;
        }
        second.show(ui, second_rect, theme, show_panel)
    }

    fn compute_rects(
        direction: SplitDirection,
        ratio: f32,
        rect: Rect,
    ) -> (Rect, Rect, Rect) {
        let half_div = DIVIDER_THICKNESS / 2.0;
        match direction {
            SplitDirection::Horizontal => {
                let split_x = rect.min.x + rect.width() * ratio;
                let first = Rect::from_min_max(
                    rect.min,
                    Pos2::new(split_x - half_div, rect.max.y),
                );
                let divider = Rect::from_min_max(
                    Pos2::new(split_x - half_div, rect.min.y),
                    Pos2::new(split_x + half_div, rect.max.y),
                );
                let second = Rect::from_min_max(
                    Pos2::new(split_x + half_div, rect.min.y),
                    rect.max,
                );
                (first, divider, second)
            }
            SplitDirection::Vertical => {
                let split_y = rect.min.y + rect.height() * ratio;
                let first = Rect::from_min_max(
                    rect.min,
                    Pos2::new(rect.max.x, split_y - half_div),
                );
                let divider = Rect::from_min_max(
                    Pos2::new(rect.min.x, split_y - half_div),
                    Pos2::new(rect.max.x, split_y + half_div),
                );
                let second = Rect::from_min_max(
                    Pos2::new(rect.min.x, split_y + half_div),
                    rect.max,
                );
                (first, divider, second)
            }
        }
    }

    /// Apply an action to the BSP tree (split or merge).
    pub fn apply_action(&mut self, action: BspAction) {
        match action {
            BspAction::None => {}
            BspAction::Split { target, direction } => {
                self.split_panel(target, direction);
            }
            BspAction::Merge { target } => {
                self.merge_panel(target);
            }
        }
    }

    fn split_panel(&mut self, target: PanelId, direction: SplitDirection) {
        match self {
            BspNode::Leaf { id, panel } if *id == target => {
                let new_leaf = BspNode::Leaf {
                    id: PanelId::next(),
                    panel: panel.clone(),
                };
                let old = std::mem::replace(self, BspNode::Leaf {
                    id: PanelId::next(),
                    panel: PanelKind::ImageViewer,
                });
                *self = BspNode::Split {
                    direction,
                    ratio: 0.5,
                    first: Box::new(old),
                    second: Box::new(new_leaf),
                };
            }
            BspNode::Split { first, second, .. } => {
                first.split_panel(target, direction);
                second.split_panel(target, direction);
            }
            _ => {}
        }
    }

    fn merge_panel(&mut self, target: PanelId) {
        match self {
            BspNode::Split { first, second, .. } => {
                if let BspNode::Leaf { id, .. } = first.as_ref() {
                    if *id == target {
                        *self = *second.clone();
                        return;
                    }
                }
                if let BspNode::Leaf { id, .. } = second.as_ref() {
                    if *id == target {
                        *self = *first.clone();
                        return;
                    }
                }
                first.merge_panel(target);
                second.merge_panel(target);
            }
            _ => {}
        }
    }
}

/// Workspace management — save, load, and switch between BSP layouts.
///
/// Each workspace stores a named BSP tree layout that can be serialized to
/// JSON for persistence.

use crate::bsp::BspNode;
use crate::panels::PanelKind;
use crate::bsp::PanelId;

/// A named workspace containing a BSP layout.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct Workspace {
    pub name: String,
    pub layout: BspNode,
}

/// Manages a list of workspaces with one active at a time.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct WorkspaceManager {
    pub workspaces: Vec<Workspace>,
    pub active_index: usize,
}

impl WorkspaceManager {
    /// Get a mutable reference to the active workspace's BSP layout.
    pub fn active_layout_mut(&mut self) -> Option<&mut BspNode> {
        self.workspaces.get_mut(self.active_index).map(|ws| &mut ws.layout)
    }

    /// Add a new workspace and switch to it.
    pub fn add_workspace(&mut self, name: String, layout: BspNode) {
        self.workspaces.push(Workspace { name, layout });
        self.active_index = self.workspaces.len() - 1;
    }

    /// Remove a workspace by index. Returns false if it's the last workspace.
    pub fn remove_workspace(&mut self, index: usize) -> bool {
        if self.workspaces.len() <= 1 || index >= self.workspaces.len() {
            return false;
        }
        self.workspaces.remove(index);
        if self.active_index >= self.workspaces.len() {
            self.active_index = self.workspaces.len() - 1;
        }
        true
    }

    /// Rename the workspace at the given index.
    pub fn rename_workspace(&mut self, index: usize, name: String) {
        if let Some(ws) = self.workspaces.get_mut(index) {
            ws.name = name;
        }
    }

    /// Serialize all workspaces to JSON for persistence.
    pub fn to_json(&self) -> String {
        serde_json::to_string(self).unwrap_or_else(|_| "{}".to_string())
    }

    /// Deserialize workspaces from JSON. Falls back to defaults on error.
    pub fn from_json(json: &str) -> Self {
        serde_json::from_str(json).unwrap_or_default()
    }

    /// Active workspace name.
    pub fn active_name(&self) -> &str {
        self.workspaces.get(self.active_index)
            .map(|ws| ws.name.as_str())
            .unwrap_or("Unknown")
    }
}

impl Default for WorkspaceManager {
    fn default() -> Self {
        Self {
            workspaces: vec![
                Workspace {
                    name: "Gallery".to_string(),
                    layout: BspNode::default_layout(),
                },
                Workspace {
                    name: "Sky Planner".to_string(),
                    layout: BspNode::Split {
                        direction: crate::bsp::SplitDirection::Horizontal,
                        ratio: 0.7,
                        first: Box::new(BspNode::Leaf {
                            id: PanelId::next(),
                            panel: PanelKind::SkyViewer,
                        }),
                        second: Box::new(BspNode::Split {
                            direction: crate::bsp::SplitDirection::Vertical,
                            ratio: 0.5,
                            first: Box::new(BspNode::Leaf {
                                id: PanelId::next(),
                                panel: PanelKind::TripPlanner,
                            }),
                            second: Box::new(BspNode::Leaf {
                                id: PanelId::next(),
                                panel: PanelKind::ExifInspector,
                            }),
                        }),
                    },
                },
                Workspace {
                    name: "Stacking".to_string(),
                    // Default Stacking workspace:
                    //   ┌──────────────────┬──────────────┐
                    //   │  ImageViewer     │  Inspector   │
                    //   │ (preview)        │ (per-frame)  │
                    //   ├──────────────────┴──────────────┤
                    //   │       Stacking Editor           │
                    //   └─────────────────────────────────┘
                    layout: BspNode::Split {
                        direction: crate::bsp::SplitDirection::Vertical,
                        ratio: 0.55,
                        first: Box::new(BspNode::Split {
                            direction: crate::bsp::SplitDirection::Horizontal,
                            ratio: 0.72,
                            first: Box::new(BspNode::Leaf {
                                id: PanelId::next(),
                                panel: PanelKind::ImageViewer,
                            }),
                            second: Box::new(BspNode::Leaf {
                                id: PanelId::next(),
                                panel: PanelKind::StackingInspector,
                            }),
                        }),
                        second: Box::new(BspNode::Leaf {
                            id: PanelId::next(),
                            panel: PanelKind::StackingEditor,
                        }),
                    },
                },
                Workspace {
                    name: "Alignment".to_string(),
                    // Alignment workspace: AlignmentViewer takes the
                    // bulk of the area, Gallery on the right for
                    // picking different reference / secondary frames.
                    layout: BspNode::Split {
                        direction: crate::bsp::SplitDirection::Horizontal,
                        ratio: 0.7,
                        first: Box::new(BspNode::Leaf {
                            id: PanelId::next(),
                            panel: PanelKind::AlignmentViewer,
                        }),
                        second: Box::new(BspNode::Leaf {
                            id: PanelId::next(),
                            panel: PanelKind::Timeline,
                        }),
                    },
                },
            ],
            active_index: 0,
        }
    }
}

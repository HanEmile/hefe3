/// Panel types and rendering.
///
/// Each panel kind can be placed in any BSP leaf. The panel type selector
/// dropdown in the header bar lets users swap panel kinds at will.

pub mod util;
pub mod image_viewer;
pub mod histogram;
pub mod exif_inspector;
pub mod timeline;
pub mod sky_viewer;
pub mod stacking_editor;
pub mod stacking_inspector;
pub mod alignment_viewer;
pub mod star_tracker;
pub mod settings;
pub mod log_viewer;
pub mod trip_planner;

use egui::{Ui, Rect};
use crate::state::AppState;

/// Enumeration of all available panel types.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub enum PanelKind {
    ImageViewer,
    Histogram,
    ExifInspector,
    Timeline,
    SkyViewer,
    StackingEditor,
    StackingInspector,
    AlignmentViewer,
    StarTracker,
    Settings,
    LogViewer,
    TripPlanner,
}

impl PanelKind {
    /// All available panel kinds, for the type selector dropdown.
    pub const ALL: &[PanelKind] = &[
        PanelKind::ImageViewer,
        PanelKind::Histogram,
        PanelKind::ExifInspector,
        PanelKind::Timeline,
        PanelKind::SkyViewer,
        PanelKind::StackingEditor,
        PanelKind::StackingInspector,
        PanelKind::AlignmentViewer,
        PanelKind::StarTracker,
        PanelKind::TripPlanner,
        PanelKind::Settings,
        PanelKind::LogViewer,
    ];

    /// Human-readable name for the panel type selector.
    pub fn name(&self) -> &'static str {
        match self {
            PanelKind::ImageViewer => "Image Viewer",
            PanelKind::Histogram => "Histogram",
            PanelKind::ExifInspector => "EXIF Inspector",
            PanelKind::Timeline => "Gallery",
            PanelKind::SkyViewer => "Sky Viewer",
            PanelKind::StackingEditor => "Stacking Editor",
            PanelKind::StackingInspector => "Stacking Inspector",
            PanelKind::AlignmentViewer => "Alignment Viewer",
            PanelKind::StarTracker => "Star Tracker",
            PanelKind::Settings => "Settings",
            PanelKind::LogViewer => "Log Viewer",
            PanelKind::TripPlanner => "Trip Planner",
        }
    }
}

/// Trait that all panels implement.
pub trait Panel {
    fn show(&mut self, ui: &mut Ui, rect: Rect, state: &mut AppState);
}

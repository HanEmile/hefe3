/// Centralised colour tokens, runtime-tweakable from the settings panel.
///
/// The theme is a plain `Theme` value held on `AppState`. Panels read
/// colours through `state.theme.<token>` instead of free constants so the
/// settings panel can mutate them live and `theme::apply(ctx, &theme)` is
/// re-run each frame.
///
/// Two presets are provided (`Theme::dark()`, `Theme::light()`); the
/// settings panel can flip between them or fine-tune individual tokens.
/// Tokens are serialised through serde so the app can persist the active
/// theme to the backend's settings store.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use egui::Color32;
use serde::{Deserialize, Serialize};

/// Serde shim — egui's `Color32` doesn't implement Serialize/Deserialize,
/// so each field is stored as `[u8; 3]` (sRGB triplet) on the wire and
/// converted via `From`.
fn ser_color<S: serde::Serializer>(c: &Color32, s: S) -> Result<S::Ok, S::Error> {
    [c.r(), c.g(), c.b()].serialize(s)
}
fn de_color<'de, D: serde::Deserializer<'de>>(d: D) -> Result<Color32, D::Error> {
    let [r, g, b]: [u8; 3] = <[u8; 3]>::deserialize(d)?;
    Ok(Color32::from_rgb(r, g, b))
}

/// A complete colour palette. Each field is one named token used by the
/// rest of the app. Keep this list small — every new token has to be
/// surfaced in the settings panel and considered for both presets.
#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
pub struct Theme {
    /// Outermost canvas — what shows behind / between panels.
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub canvas: Color32,
    /// Default panel fill.
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub panel: Color32,
    /// Header / status / chrome bars.
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub chrome: Color32,
    /// Hovered / pressed surface tint.
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub raised: Color32,
    /// Hairline borders between panels and around controls.
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub border: Color32,

    /// Primary accent — selections, focused tabs, focus rings.
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub accent: Color32,
    /// Dimmed accent for non-focused states.
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub accent_dim: Color32,

    /// Primary foreground (titles, body text).
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub text: Color32,
    /// Secondary foreground (less-important info).
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub text_dim: Color32,
    /// Tertiary foreground (placeholders, hints).
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub text_faint: Color32,

    /// Status colours.
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub success: Color32,
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub warn: Color32,
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub error: Color32,
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub info: Color32,
    #[serde(serialize_with = "ser_color", deserialize_with = "de_color")]
    pub debug: Color32,

    /// Whether to use rounded corners (sharp corners for power-user feel
    /// when false). Stored as an integer pixel radius.
    pub corner_radius: u8,
}

impl Default for Theme {
    fn default() -> Self {
        Self::dark()
    }
}

impl Theme {
    /// Default dark preset — astrophotography-friendly navy + amber.
    pub fn dark() -> Self {
        Self {
            canvas:    Color32::from_rgb(0x0a, 0x12, 0x18),
            panel:     Color32::from_rgb(0x12, 0x1a, 0x24),
            chrome:    Color32::from_rgb(0x1a, 0x25, 0x32),
            raised:    Color32::from_rgb(0x22, 0x2f, 0x3d),
            border:    Color32::from_rgb(0x2a, 0x37, 0x44),

            accent:     Color32::from_rgb(0xe8, 0xa0, 0x40),
            accent_dim: Color32::from_rgb(0xa8, 0x70, 0x28),

            text:       Color32::from_rgb(0xe8, 0xec, 0xf1),
            text_dim:   Color32::from_rgb(0xa8, 0xb2, 0xbd),
            text_faint: Color32::from_rgb(0x70, 0x7c, 0x88),

            success: Color32::from_rgb(0x6e, 0xc6, 0x7a),
            warn:    Color32::from_rgb(0xee, 0xc0, 0x4d),
            error:   Color32::from_rgb(0xe2, 0x6a, 0x6a),
            info:    Color32::from_rgb(0x6a, 0xb8, 0xe2),
            debug:   Color32::from_rgb(0xa6, 0xa9, 0xb0),

            corner_radius: 0,
        }
    }

    /// Light preset — high-contrast on a near-white canvas. Same accent
    /// hue as dark so the brand identity carries.
    pub fn light() -> Self {
        Self {
            canvas:    Color32::from_rgb(0xea, 0xed, 0xf1),
            panel:     Color32::from_rgb(0xf6, 0xf7, 0xf9),
            chrome:    Color32::from_rgb(0xde, 0xe2, 0xe8),
            raised:    Color32::from_rgb(0xc8, 0xce, 0xd6),
            border:    Color32::from_rgb(0xa8, 0xb0, 0xba),

            accent:     Color32::from_rgb(0xc4, 0x70, 0x10),
            accent_dim: Color32::from_rgb(0xe8, 0xa0, 0x40),

            text:       Color32::from_rgb(0x18, 0x1c, 0x22),
            text_dim:   Color32::from_rgb(0x4a, 0x52, 0x5c),
            text_faint: Color32::from_rgb(0x78, 0x80, 0x8a),

            success: Color32::from_rgb(0x1e, 0x8a, 0x32),
            warn:    Color32::from_rgb(0xa8, 0x66, 0x00),
            error:   Color32::from_rgb(0xc6, 0x28, 0x28),
            info:    Color32::from_rgb(0x1d, 0x6a, 0xc4),
            debug:   Color32::from_rgb(0x60, 0x66, 0x70),

            corner_radius: 0,
        }
    }

    /// Heuristic: a theme is "dark" when the canvas luminance is low.
    /// Used by `apply` to pick the right base `Visuals` flavour.
    pub fn is_dark(&self) -> bool {
        let [r, g, b, _] = self.canvas.to_array();
        // Rec.601 luma; cheap and good enough for the binary check.
        let y = 0.299 * r as f32 + 0.587 * g as f32 + 0.114 * b as f32;
        y < 128.0
    }
}

/// Apply the theme to an egui context. Run each frame from the top-level
/// app — cheap, and lets settings-panel edits take effect immediately.
pub fn apply(ctx: &egui::Context, t: &Theme) {
    let mut v = if t.is_dark() {
        egui::Visuals::dark()
    } else {
        egui::Visuals::light()
    };

    v.panel_fill = t.panel;
    v.window_fill = t.panel;
    v.extreme_bg_color = t.canvas;
    v.faint_bg_color = t.raised;
    v.code_bg_color = t.canvas;

    v.selection.bg_fill = t.accent_dim;
    v.selection.stroke = egui::Stroke::new(1.0, t.accent);

    v.hyperlink_color = t.accent;
    v.warn_fg_color = t.warn;
    v.error_fg_color = t.error;
    v.override_text_color = Some(t.text);

    v.window_stroke = egui::Stroke::new(1.0, t.border);

    let cr = egui::CornerRadius::same(t.corner_radius);
    let widgets = &mut v.widgets;
    widgets.noninteractive.bg_fill = t.panel;
    widgets.noninteractive.weak_bg_fill = t.panel;
    widgets.noninteractive.bg_stroke = egui::Stroke::new(1.0, t.border);
    widgets.noninteractive.fg_stroke = egui::Stroke::new(1.0, t.text);
    widgets.noninteractive.corner_radius = cr;

    widgets.inactive.bg_fill = t.chrome;
    widgets.inactive.weak_bg_fill = t.chrome;
    widgets.inactive.bg_stroke = egui::Stroke::new(1.0, t.border);
    widgets.inactive.fg_stroke = egui::Stroke::new(1.0, t.text_dim);
    widgets.inactive.corner_radius = cr;

    widgets.hovered.bg_fill = t.raised;
    widgets.hovered.weak_bg_fill = t.raised;
    widgets.hovered.bg_stroke = egui::Stroke::new(1.0, t.accent_dim);
    widgets.hovered.fg_stroke = egui::Stroke::new(1.0, t.text);
    widgets.hovered.corner_radius = cr;

    widgets.active.bg_fill = t.accent_dim;
    widgets.active.weak_bg_fill = t.accent_dim;
    widgets.active.bg_stroke = egui::Stroke::new(1.0, t.accent);
    widgets.active.fg_stroke = egui::Stroke::new(1.0, t.text);
    widgets.active.corner_radius = cr;

    widgets.open.bg_fill = t.raised;
    widgets.open.weak_bg_fill = t.raised;
    widgets.open.bg_stroke = egui::Stroke::new(1.0, t.accent);
    widgets.open.fg_stroke = egui::Stroke::new(1.0, t.text);
    widgets.open.corner_radius = cr;

    // Power-user defaults: tighter spacing, sharp corners.
    let style = egui::Style {
        visuals: v,
        spacing: egui::style::Spacing {
            item_spacing: egui::Vec2::new(4.0, 3.0),
            window_margin: egui::Margin::same(2),
            menu_margin: egui::Margin::same(2),
            button_padding: egui::Vec2::new(4.0, 2.0),
            indent: 12.0,
            interact_size: egui::Vec2::new(28.0, 18.0),
            slider_width: 100.0,
            combo_width: 100.0,
            text_edit_width: 200.0,
            icon_width: 14.0,
            icon_width_inner: 8.0,
            icon_spacing: 4.0,
            ..Default::default()
        },
        ..(*ctx.style()).clone()
    };
    ctx.set_style(style);
}

/// Convenience: log-level → token color.
pub fn level_color(t: &Theme, level: &str) -> Color32 {
    match level {
        "ERROR" => t.error,
        "WARN" => t.warn,
        "INFO" => t.info,
        "DEBUG" => t.debug,
        _ => t.text_dim,
    }
}

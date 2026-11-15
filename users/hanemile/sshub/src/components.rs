//! Tiny widget DSL for sshub. A `Panel` wraps a ratatui `Block` and applies
//! consistent focus/entered semantics across the UI:
//!   - unfocused:    plain dim border
//!   - focused:      plain bright border (current panel, h/l switches)
//!   - entered:      double bright border (all keys routed here)

use ratatui::{
    layout::Rect,
    style::{Color, Modifier, Style},
    text::{Line, Span},
    widgets::{Block, BorderType, Borders, Clear},
    Frame,
};

use crate::theme::Theme;

// ── style/span helpers ──────────────────────────────────────────────────
//
// `s(text, color)` builds a plain `Span` colored `color`.
// `b(text, color)` builds a bold `Span` colored `color`.
// `style!(fg: color, bold)` builds a `Style` from a token list.

pub fn s(text: impl Into<String>, color: Color) -> Span<'static> {
    Span::styled(text.into(), Style::default().fg(color))
}

pub fn b(text: impl Into<String>, color: Color) -> Span<'static> {
    Span::styled(text.into(), Style::default().fg(color).add_modifier(Modifier::BOLD))
}

/// Build a Style from a comma-separated list of fragments.
/// Supported: `fg: <color>`, `bg: <color>`, `bold`, `underlined`.
///
/// ```ignore
/// let highlighted = style!(fg: t.accent, bg: t.selection_bg, bold);
/// ```
#[macro_export]
macro_rules! style {
    ($($tok:tt)*) => {{
        let s = ratatui::style::Style::default();
        $crate::style_inner!(s; $($tok)*)
    }};
}

#[doc(hidden)]
#[macro_export]
macro_rules! style_inner {
    ($s:expr; fg: $c:expr $(, $($rest:tt)*)?) => {
        $crate::style_inner!($s.fg($c); $($($rest)*)?)
    };
    ($s:expr; bg: $c:expr $(, $($rest:tt)*)?) => {
        $crate::style_inner!($s.bg($c); $($($rest)*)?)
    };
    ($s:expr; bold $(, $($rest:tt)*)?) => {
        $crate::style_inner!($s.add_modifier(ratatui::style::Modifier::BOLD); $($($rest)*)?)
    };
    ($s:expr; underlined $(, $($rest:tt)*)?) => {
        $crate::style_inner!($s.add_modifier(ratatui::style::Modifier::UNDERLINED); $($($rest)*)?)
    };
    ($s:expr;) => { $s };
}

#[derive(Clone, Copy)]
pub struct PanelState {
    pub focused: bool,
    pub entered: bool,
}

impl PanelState {
    pub const NONE: Self = Self { focused: false, entered: false };
    pub fn focused(focused: bool, entered: bool) -> Self { Self { focused, entered: focused && entered } }
}

pub struct Panel<'a> {
    pub theme: &'a Theme,
    pub state: PanelState,
    pub title: Line<'a>,
    pub bg: Color,
}

impl<'a> Panel<'a> {
    pub fn new(theme: &'a Theme, title: Line<'a>) -> Self {
        Self { theme, state: PanelState::NONE, title, bg: theme.surface }
    }

    pub fn state(mut self, state: PanelState) -> Self { self.state = state; self }
    pub fn bg(mut self, bg: Color) -> Self { self.bg = bg; self }

    pub fn block(self) -> Block<'a> {
        let (color, btype) = match (self.state.focused, self.state.entered) {
            (true, true) => (self.theme.border_active, BorderType::Double),
            (true, false) => (self.theme.border_active, BorderType::Plain),
            _ => (self.theme.border, BorderType::Plain),
        };
        Block::default()
            .title(self.title)
            .borders(Borders::ALL)
            .border_type(btype)
            .border_style(Style::default().fg(color))
            .style(Style::default().bg(self.bg))
    }
}

/// Render a centered popup with a double border. Returns the inner Rect.
pub fn render_popup<'a>(
    frame: &mut Frame,
    area: Rect,
    title: impl Into<Line<'a>>,
    accent: Color,
    bg: Color,
    pw: u16,
    ph: u16,
) -> Rect {
    let pw = pw.min(area.width.saturating_sub(2));
    let ph = ph.min(area.height.saturating_sub(2));
    let rect = Rect::new(
        area.x + (area.width.saturating_sub(pw)) / 2,
        area.y + (area.height.saturating_sub(ph)) / 2,
        pw,
        ph,
    );
    frame.render_widget(Clear, rect);
    let block = Block::default()
        .title(title.into())
        .borders(Borders::ALL)
        .border_type(BorderType::Double)
        .border_style(Style::default().fg(accent).add_modifier(Modifier::BOLD))
        .style(Style::default().bg(bg));
    let inner = block.inner(rect);
    frame.render_widget(block, rect);
    inner
}


use ratatui::style::Color;

#[derive(Debug, Clone, Copy)]
pub struct Theme {
    pub bg: Color,
    pub surface: Color,
    pub border: Color,
    pub border_active: Color,
    pub fg: Color,
    pub fg_dim: Color,
    pub accent: Color,
    pub accent2: Color,
    pub good: Color,
    pub warn: Color,
    pub bad: Color,
    pub selection_bg: Color,
}

impl Default for Theme {
    fn default() -> Self {
        Self::green()
    }
}

impl Theme {
    pub fn green() -> Self {
        Self {
            bg: rgb(12, 18, 14),
            surface: rgb(18, 26, 20),
            border: rgb(40, 60, 46),
            border_active: rgb(90, 220, 130),
            fg: rgb(200, 230, 210),
            fg_dim: rgb(90, 120, 100),
            accent: rgb(90, 220, 130),
            accent2: rgb(160, 240, 170),
            good: rgb(120, 230, 150),
            warn: rgb(240, 200, 80),
            bad: rgb(220, 90, 90),
            selection_bg: rgb(30, 60, 40),
        }
    }

    pub fn blue() -> Self {
        Self {
            bg: rgb(16, 17, 22),
            surface: rgb(24, 26, 34),
            border: rgb(50, 55, 72),
            border_active: rgb(100, 180, 255),
            fg: rgb(200, 210, 230),
            fg_dim: rgb(90, 100, 120),
            accent: rgb(100, 180, 255),
            accent2: rgb(160, 120, 255),
            good: rgb(80, 200, 120),
            warn: rgb(240, 200, 80),
            bad: rgb(220, 80, 80),
            selection_bg: rgb(30, 45, 65),
        }
    }

    pub fn amber() -> Self {
        Self {
            bg: rgb(20, 16, 12),
            surface: rgb(28, 24, 18),
            border: rgb(70, 56, 36),
            border_active: rgb(240, 180, 60),
            fg: rgb(230, 215, 190),
            fg_dim: rgb(120, 100, 80),
            accent: rgb(240, 180, 60),
            accent2: rgb(255, 140, 70),
            good: rgb(180, 220, 120),
            warn: rgb(240, 200, 80),
            bad: rgb(220, 90, 90),
            selection_bg: rgb(60, 45, 20),
        }
    }

    pub fn named(name: &str) -> Self {
        match name {
            "blue" => Self::blue(),
            "amber" => Self::amber(),
            _ => Self::green(),
        }
    }

    pub fn name(&self) -> &'static str {
        // Compare by accent color (cheap discriminator).
        if same(self.accent, rgb(100, 180, 255)) { "blue" }
        else if same(self.accent, rgb(240, 180, 60)) { "amber" }
        else { "green" }
    }

    pub fn cycle_next(&self) -> Self {
        match self.name() {
            "green" => Self::blue(),
            "blue" => Self::amber(),
            _ => Self::green(),
        }
    }
}

fn rgb(r: u8, g: u8, b: u8) -> Color { Color::Rgb(r, g, b) }

fn same(a: Color, b: Color) -> bool {
    matches!((a, b),
        (Color::Rgb(r1, g1, b1), Color::Rgb(r2, g2, b2))
            if r1 == r2 && g1 == g2 && b1 == b2)
}

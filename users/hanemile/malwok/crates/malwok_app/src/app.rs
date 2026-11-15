#![warn(clippy::all)]

// use crossbeam_channel::{unbounded, Receiver, Sender};
use eframe::egui;
// use serde::{Deserialize, Serialize};
// use std::collections::HashMap;
// use std::thread;
// use std::time::{Duration, Instant, SystemTime};
// use tungstenite::{connect, Message};
// use url::Url;
// use egui::{Pos2, Event::PointerMoved};

#[derive(Default)]
#[derive(serde::Deserialize, serde::Serialize)]
#[serde(default)]
pub struct State {
    backend_panel: super::backend_panel::BackendPanel,
}

pub struct Malwok {
    // Example stuff:
    pub label: String,
    pub state: State,
}

impl Malwok {
    pub fn new(_: &eframe::CreationContext<'_>) -> Self {
        // This is also where you can customize the look and feel of egui using
        // `cc.egui_ctx.set_visuals` and `cc.egui_ctx.set_fonts`.

        // Load previous app state (if any).
        // Note that you must enable the `persistence` feature for this to work.
        // if let Some(storage) = cc.storage {
            // return eframe::get_value(storage, eframe::APP_KEY).unwrap_or_default();
        // }

        #[allow(unused_mut, clippy::allow_attributes)]
        let mut slf = Self {
                label: "Welcome to malwok".to_string(),
                state: State::default(),
            
        };

        #[cfg(feature = "persistence")]
        if let Some(storage) = cc.storage {
            if let Some(state) = eframe::get_value(storage, eframe::APP_KEY) {
                slf.state = state;
            }
        }

        slf
    }
}

impl eframe::App for Malwok {
    /// Called by the frame work to save state before shutdown.
    fn save(&mut self, storage: &mut dyn eframe::Storage) {
        eframe::set_value(storage, eframe::APP_KEY, self);
    }

    /// Called each time the UI needs repainting, which may be many times per second.
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        // Put your widgets into a `SidePanel`, `TopBottomPanel`, `CentralPanel`, `Window` or `Area`.
        // For inspiration and more examples, go to https://emilk.github.io/egui

        self.state.backend_panel.update(ctx, frame);
        
        egui::SidePanel::right("right_panel")
            .resizable(false)
            .default_width(160.0)
            .min_width(160.0)
            .show(ctx, |ui| {
                ui.add_space(4.0);
                ui.vertical_centered(|ui| {
                    ui.heading("malwok");
                });
                ui.separator();
                //ui.label("Remote cursors:");
                //for (id, &(x, y)) in &self.remote_cursors {
                //    ui.label(format!("{}: ({:.1}, {:.1})", id, x, y));
                //}
            });

        egui::TopBottomPanel::top("top_panel").show(ctx, |ui| {
            // The top panel is often a good place for a menu bar:

            egui::menu::bar(ui, |ui| {
                // NOTE: no File->Quit on web pages!
                let is_web = cfg!(target_arch = "wasm32");
                if !is_web {
                    ui.menu_button("File", |ui| {
                        if ui.button("Quit").clicked() {
                            ctx.send_viewport_cmd(egui::ViewportCommand::Close);
                        }
                    });
                    ui.add_space(16.0);
                }

                egui::widgets::global_theme_preference_buttons(ui);
            });
        });

        egui::CentralPanel::default().show(ctx, |ui| {
            ui.heading("Remote Cursors Display");
            ui.label(self.label.clone());
        });

        ctx.request_repaint();
    }
}

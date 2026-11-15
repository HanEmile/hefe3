use egui::{
    Vec2, Pos2, Rect,

    Key, Modifiers, ViewportCommand, // fullscreen, close

    // widgets
    Label,
    // Button
    // Slider

    Ui, Context,

    // PopupCloseBehavior,

    containers::{
        // Window,
        Scene, Area,
        frame::Frame,
        menu::{
            MenuBar, // MenuButton
            // MenuConfig,
            // SubMenuButton,
        },
        panel::{ SidePanel, TopBottomPanel, CentralPanel, }
    },
};

use virt::connect::Connect;
use virt::domain::Domain;


#[derive(Default)]
#[derive(serde::Deserialize, serde::Serialize)]
#[serde(default)]
pub struct State {}

pub struct WokApp {
    state: State,
    label: String,
    value: f32,
    scene_rect: Rect,
    repl_visible: bool,
    libvirt_conn: Result<Connect, virt::error::Error>,
}

impl Default for WokApp {
    fn default() -> Self {
        Self {
            state: Default::default(),
            label: "Hello World!".to_owned(),
            value: 2.7,
            scene_rect: Rect::ZERO, // `egui::Scene` will initialize this to something valid
            repl_visible: true,
            libvirt_conn: Connect::open(Some("qemu+ssh://root@medano/system")),
        }
    }
}

impl WokApp {
    /// Called once before the first frame.
    pub fn new(cc: &eframe::CreationContext<'_>) -> Self {
        // This is also where you can customize the look and feel of egui using
        // `cc.egui_ctx.set_visuals` and `cc.egui_ctx.set_fonts`.

        // Load previous app state (if any).
        // Note that you must enable the `persistence` feature for this to work.
        // if let Some(storage) = cc.storage {
        //     eframe::get_value(storage, eframe::APP_KEY).unwrap_or_default()
        // } else {
        //     Default::default()
        // }
        // Default::default()

        #[allow(unused_mut, clippy::allow_attributes)]
        let mut slf = Self {
            state: State::default(),
            label: "Hello".to_string(),
            value: 1.3, 
            scene_rect: Rect::ZERO, // `egui::Scene` will initialize this to something valid
            repl_visible: true,
            libvirt_conn: Connect::open(Some("qemu+ssh://root@medano/system")),
        };

        if let Some(storage) = cc.storage
            && let Some(state) = eframe::get_value(storage, eframe::APP_KEY)
        {
            slf.state = state;
        }

        slf
    }
}

impl eframe::App for WokApp {
    /// Called by the framework to save state before shutdown.
    // #[cfg(feature = "persistence")]
    fn save(&mut self, storage: &mut dyn eframe::Storage) {
        eframe::set_value(storage, eframe::APP_KEY, &self.state);
    }

    /// Called each time the UI needs repainting, which may be many times per second.
    fn update(&mut self, ctx: &Context, _frame: &mut eframe::Frame) {

        // Enter fullscreen mode
        if ctx.input_mut(|i| i.consume_key(Modifiers::NONE, Key::F11)) {
            let fullscreen = ctx.input(|i| i.viewport().fullscreen.unwrap_or(false));
            ctx.send_viewport_cmd(ViewportCommand::Fullscreen(!fullscreen));
        }

        if ctx.input_mut(|i| i.consume_key(Modifiers::NONE, Key::A)) {
            // Do Something
        }

        // The TOP Panel
        TopBottomPanel::top("top_panel").show(ctx, |ui| {
            MenuBar::new().ui(ui, |ui| {
                // NOTE: no File->Quit on web pages!
                let is_web = cfg!(target_arch = "wasm32");
                if !is_web {
                    ui.menu_button("Menu", |ui| {
                        if ui.button("Quit").clicked() {
                            ctx.send_viewport_cmd(ViewportCommand::Close);
                        }
                    });
                    ui.add_space(16.0);
                }

                egui::widgets::global_theme_preference_switch(ui);

                ui.separator();
                if ui.button("Files").clicked() { }
                if ui.button("VMs").clicked() { }
                if ui.button("Report").clicked() { }
                if ui.button("Symbols").clicked() { }
                if ui.button("Strings").clicked() { }
                ui.separator();
                if ui.button("1").clicked() { }
                if ui.button("2").clicked() { }
                if ui.button("3").clicked() { }
                if ui.button("4").clicked() { }
                if ui.button("5").clicked() { }
                if ui.button("6").clicked() { }
                if ui.button("7").clicked() { }
                if ui.button("8").clicked() { }
                if ui.button("9").clicked() { }
            });
        });

        // The Bottom panel
        TopBottomPanel::bottom("bottom_panel").show(ctx, |ui| {
            ui.horizontal(|ui| {
                ui.add_space(16.0);
                ui.label("Wok");
                ui.separator();
                ui.label("v0.0.1");
                ui.separator();
                powered_by_egui_and_eframe(ui);
                egui::warn_if_debug_build(ui);
            });
        });
        TopBottomPanel::bottom("repl_panel").show_animated(ctx, self.repl_visible, |ui| {
            ui.label(self.label.clone());
            let response = ui.add(
                egui::TextEdit::singleline(&mut self.label)
            );
            if response.changed() {
                self.label += ".";
            }
            if response.lost_focus() && ui.input(|i| i.key_pressed(egui::Key::Enter)) {
                self.label += "<CR>";
            }

        });

        // self.state.backend_panel.update(ctx, frame);

        // TODO(emile): implement
        // self.show_selected_app(ctx, frame);

        // self.state.backend_panel.end_of_frame(ctx);

        SidePanel::right("my_right_panel").show(ctx, |ui| {
            ui.take_available_space();
            ui.label("Hello Right!");

            // https://gitlab.com/libvirt/libvirt-rust/-/blob/master/examples/hello.rs?ref_type=heads
            if ui.button("load info").clicked() {
                if let Ok(conn) = self.libvirt_conn.clone() {
                    if let Ok(dom) = Domain::lookup_by_name(&conn, "arr") {
                        if let Ok(info) = dom.get_scheduler_parameters() {
                            if let Some(quota) = info.vcpu_bw.quota {
                                self.label = format!("{}", quota);
                            }
                        };
                    };
                };
            }

            ui.label(self.label.clone());
        });

        CentralPanel::default().show(ctx, |ui| {
            // Toggle repl visibility using `r`
            if ctx.input_mut(|i| i.consume_key(Modifiers::NONE, Key::R)) {
                self.repl_visible = !self.repl_visible;
            }

            ui.label(format!("Scene rect: {:#?}", &mut self.scene_rect));
            Frame::group(ui.style())
                .inner_margin(1.0)
                .show(ui, |ui| {
                    let scene = Scene::new()
                        .max_inner_size([350.0, 1000.0])
                        .zoom_range(0.1..=2.0);

                    let mut reset_view = false;
                    let mut inner_rect = Rect::NAN;
                    let response = scene
                        .show(ui, &mut self.scene_rect, |ui| {
                            reset_view = ui.button("Reset view").clicked();

                            ui.add_space(16.0);

                            // self.widget_gallery.ui(ui);
                            // ui.horizontal(|ui| {
                            //     ui.label("Write something: ");
                            //     ui.text_edit_singleline(&mut self.label);
                            // });
                            

                            ui.put(
                                Rect::from_min_size(
                            Pos2::new(0.0, -64.0),
                            Vec2::new(200.0, 16.0)),
                                    Label::new("You can put a widget anywhere")
                                        .selectable(false),
                            );

                            inner_rect = ui.min_rect();
                        })
                        .response;

                    if reset_view || response.double_clicked() {
                        self.scene_rect = inner_rect;
                    }
                });
        });
    }
}

fn powered_by_egui_and_eframe(ui: &mut Ui) {
    ui.horizontal(|ui| {
        ui.spacing_mut().item_spacing.x = 0.0;
        ui.label("Powered by ");
        ui.hyperlink_to("egui", "https://github.com/emilk/egui");
        ui.label(" and ");
        ui.hyperlink_to(
            "eframe",
            "https://github.com/emilk/egui/tree/master/crates/eframe",
        );
        ui.label(".");
    });
}

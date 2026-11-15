/// Amalthea native desktop entry point.

fn main() -> eframe::Result<()> {
    tracing_subscriber::fmt::init();

    let options = eframe::NativeOptions {
        viewport: eframe::egui::ViewportBuilder::default()
            .with_inner_size([1600.0, 1000.0])
            .with_title("Amalthea"),
        renderer: eframe::Renderer::Wgpu,
        ..Default::default()
    };

    eframe::run_native(
        "Amalthea",
        options,
        Box::new(|cc| Ok(Box::new(amalthea_app::AmaltheaApp::new(cc)))),
    )
}

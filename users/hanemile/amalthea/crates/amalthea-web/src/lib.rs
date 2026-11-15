/// Amalthea WASM web entry point.

use wasm_bindgen::prelude::*;
use wasm_bindgen::JsCast;

#[wasm_bindgen(start)]
pub async fn start() -> Result<(), JsValue> {
    // Browser-console tracing.
    tracing_subscriber::fmt()
        .with_writer(tracing_web::MakeWebConsoleWriter::new())
        .without_time()
        .init();

    let web_options = eframe::WebOptions::default();

    let canvas = web_sys::window()
        .and_then(|w| w.document())
        .and_then(|d| d.get_element_by_id("amalthea-canvas"))
        .and_then(|e| e.dyn_into::<web_sys::HtmlCanvasElement>().ok())
        .ok_or_else(|| JsValue::from_str("amalthea-canvas not found"))?;

    eframe::WebRunner::new()
        .start(
            canvas,
            web_options,
            Box::new(|cc| Ok(Box::new(amalthea_app::AmaltheaApp::new(cc)))),
        )
        .await
        .map_err(|e| JsValue::from_str(&format!("{e:?}")))?;

    Ok(())
}

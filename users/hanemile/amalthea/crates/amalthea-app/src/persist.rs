/// Theme persistence layer — reads/writes the theme JSON through the
/// backend's `/db/settings/{key}` endpoint.
///
/// Native: a tokio runtime spun up on demand on a worker thread for
/// initial load (blocks until the response arrives, max ~2s). Writes
/// happen on a fresh runtime per save — fire-and-forget.
/// WASM: `wasm_bindgen_futures::spawn_local` for both.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use crate::state::AppState;
use crate::theme::Theme;

const KEY: &str = "theme.json";

/// Pull the persisted theme into `state.theme`. Called once after the
/// first frame so the UI is responsive even if the backend is slow.
pub fn load_initial(state: &mut AppState) {
    if state.theme_loaded {
        return;
    }
    state.theme_loaded = true;

    #[cfg(not(target_arch = "wasm32"))]
    {
        let url = format!(
            "{}/db/settings/{KEY}",
            state.config.backend_url.trim_end_matches('/'),
        );
        let result = std::thread::spawn(move || -> Option<Theme> {
            let rt = tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .ok()?;
            rt.block_on(async {
                let client = reqwest::Client::builder()
                    .timeout(std::time::Duration::from_secs(2))
                    .build()
                    .ok()?;
                let resp = client.get(&url).send().await.ok()?;
                if !resp.status().is_success() {
                    return None;
                }
                let v: serde_json::Value = resp.json().await.ok()?;
                let s = v.get("value").and_then(|x| x.as_str())?;
                serde_json::from_str::<Theme>(s).ok()
            })
        })
        .join()
        .ok()
        .flatten();

        if let Some(theme) = result {
            state.theme = theme;
            state.add_log("INFO", "settings", "Loaded persisted theme.");
        } else {
            state.add_log("INFO", "settings", "No persisted theme — using default.");
        }
    }
    #[cfg(target_arch = "wasm32")]
    {
        // On WASM we'd need a different path (fetch + same-origin). Skip
        // for now: the default theme just shows until the user changes
        // something. flush() still works.
    }
}

/// Persist the current theme if dirty. Idempotent — call every frame;
/// it self-throttles by checking `theme_dirty`.
pub fn flush(state: &mut AppState) {
    if !state.theme_dirty {
        return;
    }
    state.theme_dirty = false;

    let body = match serde_json::to_string(&state.theme) {
        Ok(s) => s,
        Err(_) => return,
    };
    let url = format!(
        "{}/db/settings/{KEY}",
        state.config.backend_url.trim_end_matches('/'),
    );

    #[cfg(not(target_arch = "wasm32"))]
    {
        std::thread::spawn(move || {
            let rt = match tokio::runtime::Builder::new_current_thread().enable_all().build() {
                Ok(rt) => rt,
                Err(_) => return,
            };
            rt.block_on(async {
                if let Ok(client) = reqwest::Client::builder().build() {
                    let _ = client
                        .put(&url)
                        .json(&serde_json::json!({ "value": body }))
                        .send()
                        .await;
                }
            });
        });
    }
    #[cfg(target_arch = "wasm32")]
    {
        wasm_bindgen_futures::spawn_local(async move {
            if let Ok(client) = reqwest::Client::builder().build() {
                let _ = client
                    .put(&url)
                    .json(&serde_json::json!({ "value": body }))
                    .send()
                    .await;
            }
        });
    }
}

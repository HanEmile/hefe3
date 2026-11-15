/// Shared link import logic — works both through the backend proxy and directly.
///
/// For native: calls the Immich API directly (no backend needed).
/// For WASM: calls through the backend proxy (avoids CORS).

use crate::state::ImportMessage;
use crate::immich;

/// Extract the shared link key from a URL.
///
/// Handles: `https://photo.emile.space/share/KEY?at=ASSET_ID`, `/share/KEY`, or just `KEY`.
pub fn extract_key(input: &str) -> String {
    let input = input.trim();
    if let Some(idx) = input.find("/share/") {
        let after = &input[idx + 7..];
        return after.split('?').next().unwrap_or(after).to_string();
    }
    input.split('?').next().unwrap_or(input).to_string()
}

/// Extract the Immich base URL from a shared link URL.
///
/// `https://photo.emile.space/share/KEY` → `https://photo.emile.space`
pub fn extract_base_url(input: &str) -> Option<String> {
    if let Some(idx) = input.find("/share/") {
        Some(input[..idx].to_string())
    } else {
        None
    }
}

/// Spawn a background fetch for the preview-size full image.
///
/// Sends an `ImageReady` message on success. Errors are silently dropped
/// (the placeholder "Loading..." stays in place — the user can re-select).
#[cfg(not(target_arch = "wasm32"))]
pub fn spawn_image_fetch(
    tx: std::sync::mpsc::Sender<ImportMessage>,
    immich_base: String,
    key: String,
    asset_id: String,
    ctx: egui::Context,
) {
    std::thread::spawn(move || {
        let rt = match tokio::runtime::Runtime::new() {
            Ok(rt) => rt,
            Err(_) => return,
        };

        rt.block_on(async {
            let client = match reqwest::Client::builder()
                .timeout(std::time::Duration::from_secs(60))
                .build()
            {
                Ok(c) => c,
                Err(_) => return,
            };

            // Use the preview size — Immich returns a JPEG (~1-2 MB) which
            // the `image` crate can decode. The original is often a raw
            // file (CR3, NEF, ARW) that the crate cannot handle.
            let url = format!("{immich_base}/api/assets/{asset_id}/thumbnail?size=preview&key={key}");
            if let Ok(resp) = client.get(&url).send().await {
                if resp.status().is_success() {
                    if let Ok(bytes) = resp.bytes().await {
                        let _ = tx.send(ImportMessage::ImageReady {
                            asset_id,
                            data: bytes.to_vec(),
                        });
                        ctx.request_repaint();
                    }
                }
            }
        });
    });
}

/// Run the import on a background thread (native only).
/// Native: import a shared Immich link directly (no backend proxy).
/// The URL is parsed for `/share/<key>` and the host portion. Errors
/// flow back through `ImportMessage::Error`.
#[cfg(not(target_arch = "wasm32"))]
pub fn spawn_import(
    tx: std::sync::mpsc::Sender<ImportMessage>,
    shared_link_url: String,
    ctx: egui::Context,
) {
    std::thread::spawn(move || {
        let rt = match tokio::runtime::Runtime::new() {
            Ok(rt) => rt,
            Err(e) => {
                let _ = tx.send(ImportMessage::Error(format!("Runtime error: {e}")));
                return;
            }
        };

        rt.block_on(async {
            let client = reqwest::Client::builder()
                .timeout(std::time::Duration::from_secs(30))
                .build()
                .unwrap();

            let key = extract_key(&shared_link_url);
            if key.is_empty() {
                let _ = tx.send(ImportMessage::Error("Could not extract key from URL".into()));
                return;
            }

            // Determine the Immich base URL from the shared link.
            let immich_base = extract_base_url(&shared_link_url)
                .unwrap_or_else(|| "https://photo.emile.space".to_string());

            let _ = tx.send(ImportMessage::Status("Fetching album metadata...".into()));

            // Step 1: Get shared link info to find the album ID.
            let link_url = format!("{immich_base}/api/shared-links/me?key={key}");
            let link_resp = match client.get(&link_url).send().await {
                Ok(r) => r,
                Err(e) => {
                    let _ = tx.send(ImportMessage::Error(format!("Shared link fetch failed: {e}")));
                    return;
                }
            };

            let link_data: serde_json::Value = match link_resp.json().await {
                Ok(d) => d,
                Err(e) => {
                    let _ = tx.send(ImportMessage::Error(format!("Failed to parse shared link: {e}")));
                    return;
                }
            };

            // Step 2: If it's an album, fetch the album with assets.
            let link_type = link_data["type"].as_str().unwrap_or("");
            let assets_json = if link_type == "ALBUM" {
                let album_id = match link_data["album"]["id"].as_str() {
                    Some(id) => id,
                    None => {
                        let _ = tx.send(ImportMessage::Error("No album ID in shared link".into()));
                        return;
                    }
                };

                let album_name = link_data["album"]["albumName"].as_str().unwrap_or("Unknown album");
                let _ = tx.send(ImportMessage::Status(format!("Loading album: {album_name}...")));

                let album_url = format!("{immich_base}/api/albums/{album_id}?key={key}");
                let album_resp = match client.get(&album_url).send().await {
                    Ok(r) => r,
                    Err(e) => {
                        let _ = tx.send(ImportMessage::Error(format!("Album fetch failed: {e}")));
                        return;
                    }
                };

                let album_data: serde_json::Value = match album_resp.json().await {
                    Ok(d) => d,
                    Err(e) => {
                        let _ = tx.send(ImportMessage::Error(format!("Album parse failed: {e}")));
                        return;
                    }
                };

                album_data["assets"].clone()
            } else {
                link_data["assets"].clone()
            };

            // Parse assets.
            let assets: Vec<immich::AssetResponse> = match assets_json.as_array() {
                Some(arr) => arr.iter()
                    .filter_map(|v| serde_json::from_value(v.clone()).ok())
                    .collect(),
                None => {
                    let _ = tx.send(ImportMessage::Error("No assets found".into()));
                    return;
                }
            };

            let asset_ids: Vec<String> = assets.iter().map(|a| a.id.clone()).collect();
            let count = assets.len();
            let _ = tx.send(ImportMessage::AssetsLoaded(assets));
            let _ = tx.send(ImportMessage::Status(format!("Loaded {count} assets, fetching thumbnails...")));
            ctx.request_repaint();

            // Fetch thumbnails with bounded concurrency.
            //
            // We request `size=preview` even for grid thumbnails. Immich's
            // default `size=thumbnail` returns WebP, which the `image` crate
            // doesn't support without an extra (vendored) dependency. The
            // preview is JPEG and the size cost is acceptable on a LAN.
            //
            // We keep up to MAX_INFLIGHT requests in the air at once.
            // The cap is small enough that we don't drown the Immich VM
            // (which is single-host on the same LAN) but large enough to
            // mask request latency: 8× concurrency cuts a 50-asset
            // import from ~5s wall-clock to ~1s.
            const MAX_INFLIGHT: usize = 8;
            let mut join_set: tokio::task::JoinSet<(String, Option<Vec<u8>>)> =
                tokio::task::JoinSet::new();
            let mut next = 0;
            let mut completed = 0;
            let mut last_status_tick = 0;

            while completed < count {
                while join_set.len() < MAX_INFLIGHT && next < count {
                    let asset_id = asset_ids[next].clone();
                    let url = format!(
                        "{immich_base}/api/assets/{asset_id}/thumbnail?size=preview&key={key}"
                    );
                    let client = client.clone();
                    join_set.spawn(async move {
                        let bytes = match client.get(&url).send().await {
                            Ok(resp) if resp.status().is_success() => {
                                resp.bytes().await.ok().map(|b| b.to_vec())
                            }
                            _ => None,
                        };
                        (asset_id, bytes)
                    });
                    next += 1;
                }

                match join_set.join_next().await {
                    Some(Ok((asset_id, Some(data)))) => {
                        let _ = tx.send(ImportMessage::ThumbnailReady { asset_id, data });
                        ctx.request_repaint();
                    }
                    Some(Ok((_, None))) => {
                        // failed thumbnail — skip silently like the
                        // sequential implementation did.
                    }
                    Some(Err(_)) => {
                        // task panicked; treat like a skip.
                    }
                    None => break,
                }
                completed += 1;

                if completed - last_status_tick >= 5 || completed == count {
                    last_status_tick = completed;
                    let _ = tx.send(ImportMessage::Status(format!(
                        "Thumbnails: {completed}/{count}"
                    )));
                }
            }

            let _ = tx.send(ImportMessage::Status(format!("Import complete: {count} assets")));
            ctx.request_repaint();
        });
    });
}

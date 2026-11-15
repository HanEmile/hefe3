/// Amalthea backend server.
///
/// Serves the WASM web application, proxies Immich API calls, handles OIDC
/// authentication, and provides a REST API backed by SQLite.

mod oidc;
mod proxy;
pub mod db;

use axum::{Router, routing::{get, put}, extract::{State, Path, Query}, response::IntoResponse, Json};
use tower_http::{services::ServeDir, trace::TraceLayer, cors::CorsLayer};
use std::sync::Arc;
use serde::Deserialize;

/// Shared application state for all request handlers.
pub struct ServerState {
    pub immich_url: String,
    pub immich_api_key: String,
    pub oidc_issuer: String,
    pub oidc_client_id: String,
    pub oidc_client_secret: String,
    pub oidc_redirect_uri: String,
    pub http_client: reqwest::Client,
    pub db: db::Database,
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt::init();

    let listen_addr = std::env::var("LISTEN_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:8080".to_string());
    let static_dir = std::env::var("STATIC_DIR")
        .unwrap_or_else(|_| "./dist".to_string());
    let db_path = std::env::var("DB_PATH")
        .unwrap_or_else(|_| "/var/lib/amalthea/amalthea.db".to_string());

    let database = db::Database::open(std::path::Path::new(&db_path))
        .unwrap_or_else(|e| {
            tracing::warn!("failed to open {db_path}: {e}, using in-memory database");
            db::Database::open_memory().expect("failed to create in-memory database")
        });

    let state = Arc::new(ServerState {
        immich_url: std::env::var("IMMICH_URL")
            .unwrap_or_else(|_| "http://127.0.0.1:9091".to_string()),
        immich_api_key: read_secret_file("IMMICH_API_KEY_FILE"),
        oidc_issuer: std::env::var("OIDC_ISSUER")
            .unwrap_or_else(|_| "https://auth.medano.emile.space".to_string()),
        oidc_client_id: std::env::var("OIDC_CLIENT_ID")
            .unwrap_or_else(|_| "amalthea".to_string()),
        oidc_client_secret: read_secret_file("OIDC_CLIENT_SECRET_FILE"),
        oidc_redirect_uri: std::env::var("OIDC_REDIRECT_URI")
            .unwrap_or_else(|_| "https://amalthea.medano.emile.space/auth/callback".to_string()),
        http_client: reqwest::Client::builder()
            .timeout(std::time::Duration::from_secs(30))
            .build()
            .expect("failed to create HTTP client"),
        db: database,
    });

    let app = Router::new()
        .route("/health", get(|| async { "ok" }))
        .nest("/auth", oidc::router())
        .nest("/immich", proxy::router())
        // Database API
        .route("/db/actions", get(db_query_actions).post(db_log_action))
        .route("/db/workspaces", get(db_list_workspaces).post(db_create_workspace))
        .route("/db/workspaces/{id}", put(db_update_workspace).delete(db_delete_workspace))
        .route("/db/plans", get(db_list_plans).post(db_create_plan))
        .route("/db/plans/{id}", put(db_update_plan).delete(db_delete_plan))
        .route("/db/settings/{key}", get(db_get_setting).put(db_set_setting))
        .route("/db/export", get(db_export))
        // Immich shared link import
        .route("/import/shared-link", get(import_shared_link))
        .with_state(state)
        .layer(TraceLayer::new_for_http())
        .layer(CorsLayer::permissive())
        .fallback_service(ServeDir::new(&static_dir));

    let listener = tokio::net::TcpListener::bind(&listen_addr)
        .await
        .expect("failed to bind");

    tracing::info!("listening on {listen_addr}");
    tracing::info!("serving static files from {static_dir}");
    tracing::info!("database at {db_path}");

    axum::serve(listener, app).await.expect("server error");
}

fn read_secret_file(env_var: &str) -> String {
    match std::env::var(env_var) {
        Ok(path) => std::fs::read_to_string(&path)
            .unwrap_or_else(|e| {
                tracing::warn!("failed to read secret from {path}: {e}");
                String::new()
            })
            .trim()
            .to_string(),
        Err(_) => String::new(),
    }
}

// -- Database API handlers -----------------------------------------------------

#[derive(Deserialize)]
struct ActionQuery {
    action_type: Option<String>,
    from: Option<String>,
    to: Option<String>,
    limit: Option<u32>,
}

async fn db_query_actions(
    State(state): State<Arc<ServerState>>,
    Query(q): Query<ActionQuery>,
) -> impl IntoResponse {
    match state.db.query_actions(
        q.action_type.as_deref(),
        q.from.as_deref(),
        q.to.as_deref(),
        q.limit.unwrap_or(100),
    ) {
        Ok(rows) => Json(serde_json::json!({ "actions": rows })).into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

async fn db_log_action(
    State(state): State<Arc<ServerState>>,
    Json(body): Json<serde_json::Value>,
) -> impl IntoResponse {
    match state.db.log_action(
        body["action_type"].as_str().unwrap_or("unknown"),
        body["entity_type"].as_str(),
        body["entity_id"].as_str(),
        body.get("payload").map(|v| v.to_string()).as_deref(),
        body["user_id"].as_str(),
    ) {
        Ok(id) => Json(serde_json::json!({ "id": id })).into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

async fn db_list_workspaces(State(state): State<Arc<ServerState>>) -> impl IntoResponse {
    match state.db.list_workspaces() {
        Ok(rows) => Json(serde_json::json!({ "workspaces": rows })).into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

async fn db_create_workspace(
    State(state): State<Arc<ServerState>>,
    Json(body): Json<serde_json::Value>,
) -> impl IntoResponse {
    let name = body["name"].as_str().unwrap_or("Untitled");
    let layout = body.get("layout_json").map(|v| v.to_string()).unwrap_or_else(|| "{}".to_string());
    match state.db.upsert_workspace(None, name, &layout) {
        Ok(id) => Json(serde_json::json!({ "id": id })).into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

async fn db_update_workspace(
    State(state): State<Arc<ServerState>>,
    Path(id): Path<i64>,
    Json(body): Json<serde_json::Value>,
) -> impl IntoResponse {
    let name = body["name"].as_str().unwrap_or("Untitled");
    let layout = body.get("layout_json").map(|v| v.to_string()).unwrap_or_else(|| "{}".to_string());
    match state.db.upsert_workspace(Some(id), name, &layout) {
        Ok(id) => Json(serde_json::json!({ "id": id })).into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

async fn db_delete_workspace(
    State(state): State<Arc<ServerState>>,
    Path(id): Path<i64>,
) -> impl IntoResponse {
    match state.db.delete_workspace(id) {
        Ok(()) => Json(serde_json::json!({ "ok": true })).into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

async fn db_list_plans(State(state): State<Arc<ServerState>>) -> impl IntoResponse {
    match state.db.list_plans() {
        Ok(rows) => Json(serde_json::json!({ "plans": rows })).into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

async fn db_create_plan(
    State(state): State<Arc<ServerState>>,
    Json(body): Json<serde_json::Value>,
) -> impl IntoResponse {
    match state.db.upsert_plan(None, &body) {
        Ok(id) => Json(serde_json::json!({ "id": id })).into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

async fn db_update_plan(
    State(state): State<Arc<ServerState>>,
    Path(id): Path<i64>,
    Json(body): Json<serde_json::Value>,
) -> impl IntoResponse {
    match state.db.upsert_plan(Some(id), &body) {
        Ok(id) => Json(serde_json::json!({ "id": id })).into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

async fn db_delete_plan(
    State(state): State<Arc<ServerState>>,
    Path(id): Path<i64>,
) -> impl IntoResponse {
    match state.db.delete_plan(id) {
        Ok(()) => Json(serde_json::json!({ "ok": true })).into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

async fn db_get_setting(
    State(state): State<Arc<ServerState>>,
    Path(key): Path<String>,
) -> impl IntoResponse {
    match state.db.get_setting(&key) {
        Ok(Some(val)) => Json(serde_json::json!({ "key": key, "value": val })).into_response(),
        Ok(None) => axum::http::StatusCode::NOT_FOUND.into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

async fn db_set_setting(
    State(state): State<Arc<ServerState>>,
    Path(key): Path<String>,
    Json(body): Json<serde_json::Value>,
) -> impl IntoResponse {
    let value = body["value"].as_str().unwrap_or("");
    match state.db.set_setting(&key, value) {
        Ok(()) => Json(serde_json::json!({ "ok": true })).into_response(),
        Err(e) => (axum::http::StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

#[derive(Deserialize)]
struct ExportQuery {
    table: Option<String>,
    format: Option<String>,
}

async fn db_export(
    State(state): State<Arc<ServerState>>,
    Query(q): Query<ExportQuery>,
) -> impl IntoResponse {
    let table = q.table.as_deref().unwrap_or("actions");
    match state.db.export_table(table) {
        Ok(rows) => {
            let format = q.format.as_deref().unwrap_or("json");
            if format == "csv" {
                let csv = rows_to_csv(&rows);
                (
                    [(axum::http::header::CONTENT_TYPE, "text/csv")],
                    csv,
                ).into_response()
            } else {
                Json(rows).into_response()
            }
        }
        Err(e) => (axum::http::StatusCode::BAD_REQUEST, e).into_response(),
    }
}

// -- Immich shared link import ------------------------------------------------

#[derive(Deserialize)]
struct SharedLinkQuery {
    /// The full shared link URL or just the key.
    url: String,
}

/// Import assets from an Immich shared link.
///
/// Accepts either a full URL like `https://photo.emile.space/share/KEY?at=ASSET`
/// or just the key. Calls Immich's `GET /api/shared-links/me?key=KEY` and returns
/// the asset list.
async fn import_shared_link(
    State(state): State<Arc<ServerState>>,
    Query(q): Query<SharedLinkQuery>,
) -> impl IntoResponse {
    // Extract the key from the URL.
    let key = extract_shared_link_key(&q.url);
    if key.is_empty() {
        return (axum::http::StatusCode::BAD_REQUEST, "could not extract key from URL").into_response();
    }

    // Step 1: Get shared link metadata to find the album ID.
    let link_url = format!("{}/api/shared-links/me?key={}", state.immich_url, key);
    tracing::info!("fetching shared link metadata: {link_url}");

    let link_data: serde_json::Value = match state.http_client
        .get(&link_url)
        .header("x-api-key", &state.immich_api_key)
        .send()
        .await
    {
        Ok(resp) if resp.status().is_success() => {
            match resp.json().await {
                Ok(data) => data,
                Err(e) => return (axum::http::StatusCode::BAD_GATEWAY, format!("failed to parse link: {e}")).into_response(),
            }
        }
        Ok(resp) => {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            tracing::error!("shared link fetch failed: {status} {body}");
            return (axum::http::StatusCode::BAD_GATEWAY, format!("Immich returned {status}")).into_response();
        }
        Err(e) => return (axum::http::StatusCode::BAD_GATEWAY, format!("request failed: {e}")).into_response(),
    };

    // Step 2: If it's an album link, fetch the album with assets.
    let link_type = link_data["type"].as_str().unwrap_or("");
    if link_type == "ALBUM" {
        if let Some(album_id) = link_data["album"]["id"].as_str() {
            let album_url = format!("{}/api/albums/{}?key={}", state.immich_url, album_id, key);
            tracing::info!("fetching album assets: {album_url}");

            match state.http_client
                .get(&album_url)
                .header("x-api-key", &state.immich_api_key)
                .send()
                .await
            {
                Ok(resp) if resp.status().is_success() => {
                    match resp.json::<serde_json::Value>().await {
                        Ok(album) => {
                            // Return the album data with assets included.
                            return Json(serde_json::json!({
                                "type": "album",
                                "album_name": album["albumName"],
                                "album_id": album_id,
                                "asset_count": album["assetCount"],
                                "assets": album["assets"],
                            })).into_response();
                        }
                        Err(e) => return (axum::http::StatusCode::BAD_GATEWAY, format!("failed to parse album: {e}")).into_response(),
                    }
                }
                Ok(resp) => {
                    let status = resp.status();
                    return (axum::http::StatusCode::BAD_GATEWAY, format!("album fetch returned {status}")).into_response();
                }
                Err(e) => return (axum::http::StatusCode::BAD_GATEWAY, format!("album request failed: {e}")).into_response(),
            }
        }
    }

    // For non-album shared links (individual assets), return the link data directly.
    Json(serde_json::json!({
        "type": "assets",
        "assets": link_data["assets"],
    })).into_response()
}

/// Extract the shared link key from a URL or raw key string.
///
/// Handles formats like:
/// - `https://photo.emile.space/share/MPLS9uHP-Fl2Ux...?at=...`
/// - `/share/MPLS9uHP-Fl2Ux...`
/// - `MPLS9uHP-Fl2Ux...` (raw key)
fn extract_shared_link_key(input: &str) -> String {
    let input = input.trim();
    // If it contains /share/, extract the path segment after it.
    if let Some(idx) = input.find("/share/") {
        let after = &input[idx + 7..];
        // The key ends at '?' or end of string.
        let key = after.split('?').next().unwrap_or(after);
        return key.to_string();
    }
    // Otherwise assume the whole thing is the key.
    input.split('?').next().unwrap_or(input).to_string()
}

fn rows_to_csv(rows: &[serde_json::Value]) -> String {
    if rows.is_empty() {
        return String::new();
    }
    let mut out = String::new();
    if let Some(obj) = rows[0].as_object() {
        let keys: Vec<&String> = obj.keys().collect();
        out.push_str(&keys.iter().map(|k| k.as_str()).collect::<Vec<_>>().join(","));
        out.push('\n');
        for row in rows {
            if let Some(obj) = row.as_object() {
                let vals: Vec<String> = keys.iter().map(|k| {
                    match obj.get(*k) {
                        Some(serde_json::Value::String(s)) => format!("\"{}\"", s.replace('"', "\"\"")),
                        Some(v) => v.to_string(),
                        None => String::new(),
                    }
                }).collect();
                out.push_str(&vals.join(","));
                out.push('\n');
            }
        }
    }
    out
}

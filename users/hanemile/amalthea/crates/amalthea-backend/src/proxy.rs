/// Immich API proxy.
///
/// Proxies requests from the WASM frontend to the Immich API, injecting the
/// server-side API key. This avoids CORS issues and keeps the API key secret.
///
/// All requests to /immich/* are forwarded to IMMICH_URL/api/* with the
/// x-api-key header added.

use axum::{
    Router,
    routing::get,
    extract::{State, Path, Request},
    response::{IntoResponse, Response},
    body::Body,
};
use std::sync::Arc;
use crate::ServerState;

pub fn router() -> Router<Arc<ServerState>> {
    Router::new()
        .route("/{*path}", get(proxy_get).post(proxy_post))
}

/// Proxy GET requests to Immich.
async fn proxy_get(
    State(state): State<Arc<ServerState>>,
    Path(path): Path<String>,
    req: Request<Body>,
) -> Response {
    proxy_request(state, path, req, reqwest::Method::GET).await
}

/// Proxy POST requests to Immich.
async fn proxy_post(
    State(state): State<Arc<ServerState>>,
    Path(path): Path<String>,
    req: Request<Body>,
) -> Response {
    proxy_request(state, path, req, reqwest::Method::POST).await
}

async fn proxy_request(
    state: Arc<ServerState>,
    path: String,
    req: Request<Body>,
    method: reqwest::Method,
) -> Response {
    let target_url = format!("{}/api/{}", state.immich_url, path);

    let mut builder = state.http_client
        .request(method.clone(), &target_url)
        .header("x-api-key", &state.immich_api_key);

    // Forward relevant headers.
    if let Some(content_type) = req.headers().get("content-type") {
        builder = builder.header("content-type", content_type.clone());
    }
    if let Some(accept) = req.headers().get("accept") {
        builder = builder.header("accept", accept.clone());
    }

    // Forward query string by rebuilding the URL.
    if let Some(query) = req.uri().query() {
        let target_with_query = format!("{target_url}?{query}");
        builder = state.http_client
            .request(method, &target_with_query)
            .header("x-api-key", &state.immich_api_key);
    }

    // Forward body for POST requests.
    let body_bytes = match axum::body::to_bytes(req.into_body(), 10 * 1024 * 1024).await {
        Ok(b) => b,
        Err(e) => {
            tracing::error!("failed to read request body: {e}");
            return (
                axum::http::StatusCode::BAD_REQUEST,
                "failed to read request body",
            ).into_response();
        }
    };
    if !body_bytes.is_empty() {
        builder = builder.body(body_bytes.to_vec());
    }

    match builder.send().await {
        Ok(resp) => {
            let status = axum::http::StatusCode::from_u16(resp.status().as_u16())
                .unwrap_or(axum::http::StatusCode::INTERNAL_SERVER_ERROR);
            let mut response_builder = axum::http::Response::builder().status(status);

            // Forward content-type and cache headers from Immich.
            for key in ["content-type", "cache-control", "etag", "last-modified"] {
                if let Some(val) = resp.headers().get(key) {
                    response_builder = response_builder.header(key, val);
                }
            }

            // Add aggressive cache headers for thumbnails.
            if path.contains("thumbnail") {
                response_builder = response_builder
                    .header("cache-control", "public, max-age=86400, immutable");
            }

            match resp.bytes().await {
                Ok(bytes) => response_builder
                    .body(Body::from(bytes))
                    .unwrap()
                    .into_response(),
                Err(e) => {
                    tracing::error!("failed to read Immich response: {e}");
                    axum::http::StatusCode::BAD_GATEWAY.into_response()
                }
            }
        }
        Err(e) => {
            tracing::error!("Immich proxy request failed: {e}");
            axum::http::StatusCode::BAD_GATEWAY.into_response()
        }
    }
}

/// OIDC authentication callback handler.
///
/// Handles the authorization code flow:
/// 1. GET /auth/login — redirects to Authelia authorization endpoint
/// 2. GET /auth/callback — exchanges code for tokens, sets session cookie

use axum::{Router, routing::get, extract::{State, Query}, response::{Redirect, IntoResponse}};
use serde::Deserialize;
use std::sync::Arc;
use crate::ServerState;

pub fn router() -> Router<Arc<ServerState>> {
    Router::new()
        .route("/login", get(login))
        .route("/callback", get(callback))
}

/// Redirect user to Authelia authorization endpoint.
async fn login(State(state): State<Arc<ServerState>>) -> Redirect {
    let auth_url = format!(
        "{}/api/oidc/authorization?client_id={}&redirect_uri={}&response_type=code&scope=openid+email+profile",
        state.oidc_issuer,
        state.oidc_client_id,
        urlencoding::encode(&state.oidc_redirect_uri),
    );
    Redirect::temporary(&auth_url)
}

#[derive(Deserialize)]
struct CallbackParams {
    code: String,
    #[allow(dead_code)]
    state: Option<String>,
}

/// Handle the OIDC callback: exchange code for tokens.
async fn callback(
    State(state): State<Arc<ServerState>>,
    Query(params): Query<CallbackParams>,
) -> impl IntoResponse {
    let token_url = format!("{}/api/oidc/token", state.oidc_issuer);

    let response = state.http_client
        .post(&token_url)
        .form(&[
            ("grant_type", "authorization_code"),
            ("client_id", &state.oidc_client_id),
            ("client_secret", &state.oidc_client_secret),
            ("code", &params.code),
            ("redirect_uri", &state.oidc_redirect_uri),
        ])
        .send()
        .await;

    match response {
        Ok(resp) if resp.status().is_success() => {
            // TODO: Parse token response, set secure HttpOnly cookie, redirect to app
            let _body = resp.text().await.unwrap_or_default();
            tracing::info!("OIDC token exchange successful");
            // For now, redirect to the app root. Full implementation will set a
            // session cookie with the access token.
            Redirect::temporary("/").into_response()
        }
        Ok(resp) => {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            tracing::error!("OIDC token exchange failed: {status} {body}");
            axum::http::StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
        Err(e) => {
            tracing::error!("OIDC token exchange request failed: {e}");
            axum::http::StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

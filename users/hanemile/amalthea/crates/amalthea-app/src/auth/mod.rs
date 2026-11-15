/// OIDC authentication state machine.
///
/// Models the lifecycle of an OpenID Connect authentication flow for both web
/// (redirect-based) and native (loopback) targets. The actual token exchange
/// is performed by the backend; this module only tracks client-side state
/// and constructs the initial authorization URL.

use chrono::{DateTime, Utc};

// ---------------------------------------------------------------------------
// Auth state
// ---------------------------------------------------------------------------

/// Possible states of the authentication flow.
#[derive(Debug, Clone)]
pub enum AuthState {
    /// No user session.
    Unauthenticated,

    /// The authorization URL has been opened and we are waiting for the
    /// identity provider to redirect back with an authorization code.
    AwaitingCallback {
        /// Random `state` parameter used to prevent CSRF.
        state_param: String,
        /// Nonce bound to the token request.
        nonce: String,
    },

    /// A valid access token is available.
    Authenticated {
        /// Bearer access token.
        access_token: String,
        /// Optional refresh token for token renewal.
        refresh_token: Option<String>,
        /// When the access token expires.
        expires_at: DateTime<Utc>,
    },

    /// The authentication attempt failed.
    Error(String),
}

impl Default for AuthState {
    fn default() -> Self {
        Self::Unauthenticated
    }
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

/// Configuration required to initiate an OIDC authorization-code flow.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct AuthConfig {
    /// OIDC issuer URL (e.g. `https://auth.example.com`).
    pub issuer: String,
    /// Client ID registered with the identity provider.
    pub client_id: String,
    /// Redirect URI the identity provider will call back to.
    pub redirect_uri: String,
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Generate a cryptographic-quality random hex string.
fn random_hex(len: usize) -> String {
    use std::fmt::Write;
    let bytes: Vec<u8> = (0..len).map(|_| rand_byte()).collect();
    let mut s = String::with_capacity(len * 2);
    for b in bytes {
        let _ = write!(s, "{b:02x}");
    }
    s
}

/// Produce a single pseudo-random byte.
///
/// Uses `getrandom` when available (native + WASM with the `js` feature) or
/// falls back to a simple timestamp-derived value. This is good enough for
/// OIDC state/nonce parameters.
fn rand_byte() -> u8 {
    // Try getrandom via uuid's underlying randomness (already a dependency).
    uuid::Uuid::new_v4().as_bytes()[0]
}

/// Construct the OIDC authorization URL and return `(url, state, nonce)`.
///
/// The caller should open this URL in a browser and transition the
/// [`AuthState`] to [`AuthState::AwaitingCallback`] with the returned
/// `state` and `nonce` values.
pub fn authorization_url(config: &AuthConfig) -> (String, String, String) {
    let state = random_hex(16);
    let nonce = random_hex(16);

    let url = format!(
        "{issuer}/authorize\
         ?response_type=code\
         &client_id={client_id}\
         &redirect_uri={redirect_uri}\
         &scope=openid%20profile%20email\
         &state={state}\
         &nonce={nonce}",
        issuer = config.issuer.trim_end_matches('/'),
        client_id = urlencoding_encode(&config.client_id),
        redirect_uri = urlencoding_encode(&config.redirect_uri),
        state = state,
        nonce = nonce,
    );

    (url, state, nonce)
}

/// Returns `true` if the current auth state represents an expired or missing
/// session.
pub fn is_expired(state: &AuthState) -> bool {
    match state {
        AuthState::Authenticated { expires_at, .. } => Utc::now() >= *expires_at,
        AuthState::Unauthenticated | AuthState::AwaitingCallback { .. } | AuthState::Error(_) => {
            true
        }
    }
}

/// Minimal percent-encoding for URL query parameters.
///
/// Only encodes the characters that are unsafe in a query-string value.
/// This avoids pulling in a full `urlencoding` crate.
fn urlencoding_encode(input: &str) -> String {
    let mut out = String::with_capacity(input.len());
    for byte in input.bytes() {
        match byte {
            b'A'..=b'Z'
            | b'a'..=b'z'
            | b'0'..=b'9'
            | b'-'
            | b'_'
            | b'.'
            | b'~' => out.push(byte as char),
            _ => {
                out.push('%');
                out.push(char::from(HEX[(byte >> 4) as usize]));
                out.push(char::from(HEX[(byte & 0x0f) as usize]));
            }
        }
    }
    out
}

const HEX: &[u8; 16] = b"0123456789ABCDEF";

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unauthenticated_is_expired() {
        assert!(is_expired(&AuthState::Unauthenticated));
    }

    #[test]
    fn future_token_is_not_expired() {
        let state = AuthState::Authenticated {
            access_token: "tok".into(),
            refresh_token: None,
            expires_at: Utc::now() + chrono::Duration::hours(1),
        };
        assert!(!is_expired(&state));
    }

    #[test]
    fn past_token_is_expired() {
        let state = AuthState::Authenticated {
            access_token: "tok".into(),
            refresh_token: None,
            expires_at: Utc::now() - chrono::Duration::hours(1),
        };
        assert!(is_expired(&state));
    }

    #[test]
    fn authorization_url_contains_params() {
        let config = AuthConfig {
            issuer: "https://auth.example.com".into(),
            client_id: "amalthea".into(),
            redirect_uri: "http://localhost:8080/callback".into(),
        };
        let (url, state, nonce) = authorization_url(&config);
        assert!(url.contains("response_type=code"));
        assert!(url.contains(&format!("state={state}")));
        assert!(url.contains(&format!("nonce={nonce}")));
        assert!(url.contains("client_id=amalthea"));
    }
}

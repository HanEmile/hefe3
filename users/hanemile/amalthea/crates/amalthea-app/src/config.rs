/// Application configuration.
///
/// Holds connection details for the backend, Immich instance, and OIDC
/// identity provider. Loaded from environment variables on native targets
/// and uses compile-time defaults for WASM.

/// Central configuration for the Amalthea application.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct AppConfig {
    /// URL of the amalthea backend (serves WASM assets, proxies API).
    pub backend_url: String,
    /// Direct Immich URL (used in native mode for server-to-server calls).
    pub immich_url: String,
    /// OIDC issuer URL (Authelia endpoint).
    pub oidc_issuer: String,
    /// OIDC client identifier registered with the issuer.
    pub oidc_client_id: String,
    /// Server-side SQLite database path. Editing this from the settings
    /// panel just updates the displayed value; the backend reopens the
    /// file the next time it is restarted.
    pub db_path: String,
}

impl Default for AppConfig {
    fn default() -> Self {
        #[cfg(target_arch = "wasm32")]
        {
            Self {
                backend_url: "".to_string(), // same-origin
                immich_url: String::new(), // unused in WASM — backend proxies
                oidc_issuer: String::new(),
                oidc_client_id: "amalthea".to_string(),
                db_path: String::new(), // server-side
            }
        }

        #[cfg(not(target_arch = "wasm32"))]
        {
            Self::from_env()
        }
    }
}

impl AppConfig {
    /// Load configuration from environment variables with sensible fallbacks.
    ///
    /// | Variable | Default |
    /// |---|---|
    /// | `AMALTHEA_BACKEND_URL` | `http://127.0.0.1:8080` |
    /// | `AMALTHEA_IMMICH_URL` | `http://192.168.75.9:9091` |
    /// | `AMALTHEA_OIDC_ISSUER` | (empty) |
    /// | `AMALTHEA_OIDC_CLIENT_ID` | `amalthea` |
    ///
    /// `backend_url` has no `/api` suffix — the backend mounts routes
    /// (`/db/*`, `/import/*`, `/health`) at the root. `127.0.0.1` is used
    /// instead of `localhost` to avoid the macOS quirk where `localhost`
    /// resolves to `::1` first and can hit a different IPv6 listener.
    #[cfg(not(target_arch = "wasm32"))]
    pub fn from_env() -> Self {
        Self {
            backend_url: std::env::var("AMALTHEA_BACKEND_URL")
                .unwrap_or_else(|_| "http://127.0.0.1:8080".to_string()),
            immich_url: std::env::var("AMALTHEA_IMMICH_URL")
                .unwrap_or_else(|_| "http://192.168.75.9:9091".to_string()),
            oidc_issuer: std::env::var("AMALTHEA_OIDC_ISSUER")
                .unwrap_or_default(),
            oidc_client_id: std::env::var("AMALTHEA_OIDC_CLIENT_ID")
                .unwrap_or_else(|_| "amalthea".to_string()),
            db_path: std::env::var("AMALTHEA_DB_PATH")
                .unwrap_or_else(|_| "/var/lib/amalthea/amalthea.db".to_string()),
        }
    }
}

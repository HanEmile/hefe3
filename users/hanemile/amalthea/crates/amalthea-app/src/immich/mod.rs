/// Hand-written async client for the Immich REST API.
///
/// Wraps `reqwest::Client` and authenticates every request with an API key
/// passed in the `x-api-key` header. The base URL defaults to the `/api`
/// path prefix following Immich convention.

pub mod types;

pub use types::*;

use reqwest::header::{HeaderMap, HeaderValue};
use url::Url;

// ---------------------------------------------------------------------------
// Error type
// ---------------------------------------------------------------------------

/// Errors produced by the Immich client.
#[derive(Debug, thiserror::Error)]
pub enum ImmichError {
    /// The underlying HTTP request failed.
    #[error("HTTP request failed: {0}")]
    Http(#[from] reqwest::Error),

    /// The server responded with a non-success status code.
    #[error("Immich API error {status}: {body}")]
    Api {
        status: u16,
        body: String,
    },

    /// Failed to parse the base URL.
    #[error("invalid base URL: {0}")]
    InvalidUrl(#[from] url::ParseError),

    /// JSON deserialization failed.
    #[error("failed to deserialize response: {0}")]
    Deserialize(#[source] reqwest::Error),
}

/// Convenience alias used throughout this module.
pub type Result<T> = std::result::Result<T, ImmichError>;

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

/// Async client for the Immich REST API.
///
/// Create one instance and reuse it — `reqwest::Client` maintains a
/// connection pool internally.
/// The API key passed to `new()` is baked into the underlying
/// `reqwest::Client`'s default headers — we don't store it on the
/// struct because nothing reads it back after construction.
#[derive(Debug, Clone)]
pub struct ImmichClient {
    /// Base URL of the Immich instance including the `/api` prefix.
    base_url: Url,
    /// Underlying HTTP client (carries the API-key header internally).
    http: reqwest::Client,
}

impl ImmichClient {
    /// Create a new client for the Immich instance at `base_url`.
    ///
    /// The URL is normalised to ensure it ends with `/api/`. For example
    /// `http://photo:9091` becomes `http://photo:9091/api/`.
    pub fn new(base_url: &str, api_key: String) -> Result<Self> {
        let mut url = Url::parse(base_url)?;

        // Ensure the path ends with /api/.
        let path = url.path().trim_end_matches('/').to_string();
        if !path.ends_with("/api") {
            url.set_path(&format!("{}/api/", path));
        } else {
            url.set_path(&format!("{}/", path));
        }

        let mut default_headers = HeaderMap::new();
        default_headers.insert(
            "x-api-key",
            HeaderValue::from_str(&api_key)
                .unwrap_or_else(|_| HeaderValue::from_static("")),
        );

        let http = reqwest::Client::builder()
            .default_headers(default_headers)
            .build()
            .map_err(ImmichError::Http)?;

        Ok(Self { base_url: url, http })
    }

    // -- Assets -------------------------------------------------------------

    /// Fetch the full metadata for a single asset by ID.
    pub async fn get_asset(&self, id: &str) -> Result<AssetResponse> {
        let url = self.url(&format!("assets/{id}"));
        let resp = self.http.get(url).send().await?;
        self.json(resp).await
    }

    /// Download the thumbnail bytes for an asset.
    pub async fn get_thumbnail(&self, id: &str) -> Result<Vec<u8>> {
        let url = self.url(&format!("assets/{id}/thumbnail"));
        let resp = self.http.get(url).send().await?;
        self.bytes(resp).await
    }

    /// Download the original (full-resolution) file bytes for an asset.
    pub async fn get_original(&self, id: &str) -> Result<Vec<u8>> {
        let url = self.url(&format!("assets/{id}/original"));
        let resp = self.http.get(url).send().await?;
        self.bytes(resp).await
    }

    // -- Search -------------------------------------------------------------

    /// Search assets by EXIF / metadata fields.
    pub async fn search_metadata(&self, query: MetadataSearchQuery) -> Result<SearchResponse> {
        let url = self.url("search/metadata");
        let resp = self.http.post(url).json(&query).send().await?;
        self.json(resp).await
    }

    /// Search assets using CLIP (smart / semantic) search.
    pub async fn search_smart(&self, query: &str) -> Result<SearchResponse> {
        let url = self.url("search/smart");
        let body = serde_json::json!({ "query": query });
        let resp = self.http.post(url).json(&body).send().await?;
        self.json(resp).await
    }

    // -- Albums -------------------------------------------------------------

    /// List all albums visible to the authenticated user.
    pub async fn list_albums(&self) -> Result<Vec<AlbumResponse>> {
        let url = self.url("albums");
        let resp = self.http.get(url).send().await?;
        self.json(resp).await
    }

    /// Fetch a single album by ID, including its assets.
    pub async fn get_album(&self, id: &str) -> Result<AlbumResponse> {
        let url = self.url(&format!("albums/{id}"));
        let resp = self.http.get(url).send().await?;
        self.json(resp).await
    }

    // -- Tags ---------------------------------------------------------------

    /// List all tags.
    pub async fn list_tags(&self) -> Result<Vec<TagResponse>> {
        let url = self.url("tags");
        let resp = self.http.get(url).send().await?;
        self.json(resp).await
    }

    // -- Helpers ------------------------------------------------------------

    /// Build a full URL by joining a relative path to the base.
    fn url(&self, path: &str) -> Url {
        // `Url::join` handles leading slashes correctly when the base ends
        // with '/'.
        self.base_url
            .join(path)
            .unwrap_or_else(|_| self.base_url.clone())
    }

    /// Ensure the response is successful and deserialize JSON.
    async fn json<T: serde::de::DeserializeOwned>(
        &self,
        resp: reqwest::Response,
    ) -> Result<T> {
        let resp = self.check_status(resp).await?;
        resp.json::<T>().await.map_err(ImmichError::Deserialize)
    }

    /// Ensure the response is successful and return the raw bytes.
    async fn bytes(&self, resp: reqwest::Response) -> Result<Vec<u8>> {
        let resp = self.check_status(resp).await?;
        Ok(resp.bytes().await.map_err(ImmichError::Http)?.to_vec())
    }

    /// Return the response if successful, or convert it into an `ImmichError`.
    async fn check_status(
        &self,
        resp: reqwest::Response,
    ) -> Result<reqwest::Response> {
        if resp.status().is_success() {
            Ok(resp)
        } else {
            let status = resp.status().as_u16();
            let body = resp.text().await.unwrap_or_default();
            Err(ImmichError::Api { status, body })
        }
    }
}

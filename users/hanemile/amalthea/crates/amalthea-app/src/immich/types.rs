/// Serde-deserializable types matching the Immich OpenAPI specification.
///
/// All structs use `#[serde(rename_all = "camelCase")]` because the Immich
/// REST API returns camelCase JSON. Fields that collide with Rust keywords
/// (e.g. `type`) are suffixed with `_` and explicitly renamed.

use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// Asset
// ---------------------------------------------------------------------------

/// The type of an asset (image or video).
#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize)]
pub enum AssetType {
    #[default]
    #[serde(rename = "IMAGE")]
    Image,
    #[serde(rename = "VIDEO")]
    Video,
}

/// Full asset response from `GET /assets/{id}` or search results.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AssetResponse {
    pub id: String,
    #[serde(default)]
    pub checksum: String,
    #[serde(default)]
    pub original_file_name: String,
    #[serde(default)]
    pub original_mime_type: String,
    #[serde(default)]
    pub file_created_at: String,
    #[serde(default)]
    pub file_modified_at: String,
    #[serde(default)]
    pub width: u32,
    #[serde(default)]
    pub height: u32,
    #[serde(default)]
    pub is_favorite: bool,
    #[serde(default)]
    pub is_archived: bool,
    /// Asset type: IMAGE or VIDEO.
    #[serde(rename = "type", default)]
    pub type_: AssetType,
    #[serde(default)]
    pub exif_info: Option<ExifResponse>,
    #[serde(default)]
    pub tags: Vec<TagResponse>,
    #[serde(default)]
    pub people: Vec<PersonResponse>,
    #[serde(default)]
    pub stack: Option<serde_json::Value>,
    #[serde(default)]
    pub thumbhash: Option<String>,
}

// ---------------------------------------------------------------------------
// EXIF
// ---------------------------------------------------------------------------

/// EXIF metadata embedded in an asset.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ExifResponse {
    pub exposure_time: Option<String>,
    pub f_number: Option<f64>,
    pub iso: Option<u32>,
    pub focal_length: Option<f64>,
    pub lens_model: Option<String>,
    pub make: Option<String>,
    pub model: Option<String>,
    pub date_time_original: Option<String>,
    pub latitude: Option<f64>,
    pub longitude: Option<f64>,
    pub city: Option<String>,
    pub country: Option<String>,
    #[serde(rename = "state")]
    pub state_: Option<String>,
    pub description: Option<String>,
    pub file_size_in_byte: Option<u64>,
    pub orientation: Option<String>,
    pub rating: Option<i32>,
}

// ---------------------------------------------------------------------------
// Album
// ---------------------------------------------------------------------------

/// Album response from `GET /albums` or `GET /albums/{id}`.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AlbumResponse {
    pub id: String,
    #[serde(default)]
    pub album_name: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub asset_count: u32,
    #[serde(default)]
    pub assets: Vec<AssetResponse>,
    #[serde(default)]
    pub created_at: String,
    #[serde(default)]
    pub updated_at: String,
}

// ---------------------------------------------------------------------------
// Tag
// ---------------------------------------------------------------------------

/// Tag attached to an asset.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TagResponse {
    pub id: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub value: String,
}

// ---------------------------------------------------------------------------
// Person
// ---------------------------------------------------------------------------

/// Recognized person (face detection) associated with an asset.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PersonResponse {
    pub id: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub thumbnail_path: String,
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

/// Top-level search response from `POST /search/metadata` or
/// `POST /search/smart`.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SearchResponse {
    pub assets: SearchAssetResponse,
}

/// Paginated list of assets within a search response.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SearchAssetResponse {
    #[serde(default)]
    pub items: Vec<AssetResponse>,
    pub next_page: Option<String>,
}

// ---------------------------------------------------------------------------
// Metadata search query
// ---------------------------------------------------------------------------

/// Query parameters for `POST /search/metadata`.
///
/// All fields are optional — only non-`None` values are serialized into the
/// request body.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MetadataSearchQuery {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub make: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub lens_model: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub city: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub country: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    #[serde(rename = "state")]
    pub state_: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub taken_after: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub taken_before: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub is_favorite: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub is_archived: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    #[serde(rename = "type")]
    pub type_: Option<AssetType>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub page: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub size: Option<u32>,
}

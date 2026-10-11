//! Qobuz provider models, track extraction, and search paths.

use regex::Regex;
use serde::{Deserialize, Serialize};
use std::sync::LazyLock;

pub const QOBUZ_ZARZ_CATALOG_APP_ID: &str = "798273057";

static QOBUZ_TRACK_URL_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?i)/(?:track|tracks)/(\d+)(?:[/?#]|$)").unwrap());

pub fn extract_qobuz_track_id(url: &str) -> Option<String> {
    let trimmed = url.trim();
    if !trimmed.to_ascii_lowercase().contains("qobuz") {
        return None;
    }
    QOBUZ_TRACK_URL_RE
        .captures(trimmed)
        .and_then(|c| c.get(1))
        .map(|m| m.as_str().to_string())
}

pub fn qobuz_zarz_search_path(query: &str, limit: usize) -> String {
    let limit = if limit == 0 { 10 } else { limit };
    let query_encoded =
        url::form_urlencoded::byte_serialize(query.trim().as_bytes()).collect::<String>();
    format!(
        "/qbz/track/search?query={}&limit={}&app_id={}",
        query_encoded, limit, QOBUZ_ZARZ_CATALOG_APP_ID
    )
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct QobuzPerformer {
    pub name: String,
    #[serde(default)]
    pub id: i64,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct QobuzAlbumImage {
    #[serde(default)]
    pub small: String,
    #[serde(default)]
    pub thumbnail: String,
    #[serde(default)]
    pub large: String,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct QobuzAlbum {
    pub title: String,
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub image: Option<QobuzAlbumImage>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct QobuzTrack {
    pub id: i64,
    pub title: String,
    #[serde(default)]
    pub version: Option<String>,
    #[serde(default)]
    pub duration: u64,
    #[serde(rename = "track_number", default)]
    pub track_number: u32,
    #[serde(rename = "media_number", default)]
    pub media_number: u32,
    #[serde(default)]
    pub isrc: Option<String>,
    #[serde(rename = "maximum_bit_depth", default)]
    pub maximum_bit_depth: Option<u32>,
    #[serde(rename = "maximum_sampling_rate", default)]
    pub maximum_sampling_rate: Option<f64>,
    #[serde(default)]
    pub hires: bool,
    #[serde(default)]
    pub performer: Option<QobuzPerformer>,
    #[serde(default)]
    pub album: Option<QobuzAlbum>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct QobuzSearchResult {
    pub total: usize,
    pub items: Vec<QobuzTrack>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_extract_qobuz_track_id() {
        assert_eq!(
            extract_qobuz_track_id("https://open.qobuz.com/track/1234567"),
            Some("1234567".into())
        );
        assert_eq!(
            extract_qobuz_track_id("https://play.qobuz.com/album/xyz/tracks/987654?stream=true"),
            Some("987654".into())
        );
        assert_eq!(
            extract_qobuz_track_id("https://open.spotify.com/track/123"),
            None
        );
    }

    #[test]
    fn test_qobuz_zarz_search_path() {
        let path = qobuz_zarz_search_path("Daft Punk Around The World", 5);
        assert!(path.starts_with("/qbz/track/search?"));
        assert!(path.contains("limit=5"));
        assert!(path.contains(QOBUZ_ZARZ_CATALOG_APP_ID));
    }
}

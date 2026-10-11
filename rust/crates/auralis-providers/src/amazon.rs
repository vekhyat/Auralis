//! Amazon Music provider types, quality normalization, and community API models.

use regex::Regex;
use serde::{Deserialize, Serialize};
use std::sync::LazyLock;

static AMAZON_TRACK_ASIN_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?i)/tracks/([a-z0-9]{10})(?:[/?#]|$)").unwrap());

pub fn extract_amazon_track_asin(url: &str) -> Option<String> {
    AMAZON_TRACK_ASIN_RE
        .captures(url.trim())
        .and_then(|c| c.get(1))
        .map(|m| m.as_str().to_string())
}

pub fn normalize_amazon_quality(quality: &str) -> &'static str {
    match quality.trim().to_lowercase().as_str() {
        "16" | "lossless" | "cd" => "16",
        "atmos" | "eac3" | "dolby" => "atmos",
        _ => "24",
    }
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
pub struct AmazonCommunityResponse {
    #[serde(default)]
    pub asin: String,
    #[serde(default)]
    pub codec: String,
    #[serde(rename = "bit_depth", default)]
    pub bit_depth: u32,
    #[serde(default)]
    pub url: String,
    #[serde(rename = "stream_url", default)]
    pub stream_url: String,
    #[serde(default)]
    pub key: String,
    #[serde(rename = "key_specs", default)]
    pub key_specs: Vec<String>,
    #[serde(default)]
    pub captcha: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_extract_amazon_track_asin() {
        assert_eq!(
            extract_amazon_track_asin("https://music.amazon.com/tracks/B09ABC1234?foo=bar"),
            Some("B09ABC1234".into())
        );
        assert_eq!(
            extract_amazon_track_asin("https://music.amazon.com/albums/B09ABC1234"),
            None
        );
    }

    #[test]
    fn test_normalize_amazon_quality() {
        assert_eq!(normalize_amazon_quality("CD"), "16");
        assert_eq!(normalize_amazon_quality("Lossless"), "16");
        assert_eq!(normalize_amazon_quality("Atmos"), "atmos");
        assert_eq!(normalize_amazon_quality("HiRes"), "24");
        assert_eq!(normalize_amazon_quality("anything-else"), "24");
    }
}

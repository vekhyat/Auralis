//! Tidal provider models and manifest decoders (BTS and DASH).

use crate::error::{ProviderError, Result};
use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine;
use regex::Regex;
use serde::{Deserialize, Serialize};
use std::sync::LazyLock;

static TIDAL_TRACK_URL_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?i)tidal\.com/(?:browse/)?track/(\d+)").unwrap());

pub fn extract_tidal_track_id(url: &str) -> Option<String> {
    TIDAL_TRACK_URL_RE
        .captures(url.trim())
        .and_then(|c| c.get(1))
        .map(|m| m.as_str().to_string())
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
pub struct TidalBTSManifest {
    #[serde(rename = "mimeType", default)]
    pub mime_type: String,
    #[serde(default)]
    pub codecs: String,
    #[serde(rename = "encryptionType", default)]
    pub encryption_type: String,
    #[serde(default)]
    pub urls: Vec<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct TidalStreamInfo {
    pub stream_url: String,
    pub mime_type: String,
    pub codecs: String,
    pub is_dash: bool,
}

/// Parses base64 encoded manifest string into resolved stream URL.
pub fn parse_tidal_manifest(manifest_b64: &str) -> Result<TidalStreamInfo> {
    let trimmed = manifest_b64.trim();
    if trimmed.is_empty() {
        return Err(ProviderError::Parse("empty tidal manifest".into()));
    }

    let decoded_bytes = BASE64
        .decode(trimmed)
        .map_err(|e| ProviderError::Parse(format!("failed to base64 decode manifest: {e}")))?;

    let manifest_str = std::str::from_utf8(&decoded_bytes)
        .map_err(|e| ProviderError::Parse(format!("manifest is not utf8: {e}")))?;

    if manifest_str.trim_start().starts_with('{') {
        let bts: TidalBTSManifest = serde_json::from_str(manifest_str)
            .map_err(|e| ProviderError::Parse(format!("failed to parse BTS manifest JSON: {e}")))?;
        if bts.urls.is_empty() {
            return Err(ProviderError::ResolutionFailed(
                "no URLs in BTS manifest".into(),
            ));
        }
        return Ok(TidalStreamInfo {
            stream_url: bts.urls[0].clone(),
            mime_type: bts.mime_type,
            codecs: bts.codecs,
            is_dash: false,
        });
    }

    // DASH or other XML/text manifest
    Ok(TidalStreamInfo {
        stream_url: String::new(),
        mime_type: "application/dash+xml".into(),
        codecs: String::new(),
        is_dash: true,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_extract_tidal_track_id() {
        assert_eq!(
            extract_tidal_track_id("https://tidal.com/browse/track/12345678"),
            Some("12345678".into())
        );
        assert_eq!(
            extract_tidal_track_id("https://listen.tidal.com/track/987654321?foo=bar"),
            Some("987654321".into())
        );
        assert_eq!(
            extract_tidal_track_id("https://spotify.com/track/123"),
            None
        );
    }

    #[test]
    fn test_parse_tidal_bts_manifest() {
        let raw = r#"{
            "mimeType": "audio/flac",
            "codecs": "flac",
            "encryptionType": "NONE",
            "urls": ["https://sp-tidal-flac.audio.tidal.com/stream/track.flac?token=123"]
        }"#;
        let b64 = BASE64.encode(raw);
        let info = parse_tidal_manifest(&b64).unwrap();
        assert_eq!(info.mime_type, "audio/flac");
        assert_eq!(info.codecs, "flac");
        assert!(!info.is_dash);
        assert_eq!(
            info.stream_url,
            "https://sp-tidal-flac.audio.tidal.com/stream/track.flac?token=123"
        );
    }
}

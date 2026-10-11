//! Deezer provider client, track extraction, and format identification.

pub use crate::deezer_crypto::{
    decrypt_deezer_bytes as decrypt_deezer_stream, deezer_blowfish_key, get_blowfish_key,
    DEEZER_CHUNK_SIZE, DEEZER_IV,
};
use regex::Regex;
use serde::{Deserialize, Serialize};
use std::sync::LazyLock;

static DEEZER_TRACK_URL_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?i)/(?:track|tracks)/(\d+)(?:[/?#]|$)").unwrap());

pub fn extract_deezer_track_id(url: &str) -> Option<String> {
    DEEZER_TRACK_URL_RE
        .captures(url.trim())
        .and_then(|c| c.get(1))
        .map(|m| m.as_str().to_string())
}

/// Detects audio container format from decrypted bytes.
pub fn detect_audio_extension(bytes: &[u8]) -> &'static str {
    if bytes.len() >= 4 && &bytes[0..4] == b"fLaC" {
        ".flac"
    } else if bytes.len() >= 3 && &bytes[0..3] == b"ID3" {
        ".mp3"
    } else if bytes.len() >= 8 && &bytes[4..8] == b"ftyp" {
        ".m4a"
    } else {
        ".flac"
    }
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
pub struct DeezerTrackInfo {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub title: String,
    #[serde(default)]
    pub artist: String,
    #[serde(rename = "decryption_key", default)]
    pub decryption_key: String,
    #[serde(rename = "stream_url", default)]
    pub stream_url: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_extract_deezer_track_id() {
        assert_eq!(
            extract_deezer_track_id("https://www.deezer.com/track/3135556"),
            Some("3135556".into())
        );
        assert_eq!(
            extract_deezer_track_id("https://deezer.page.link/track/999?source=share"),
            Some("999".into())
        );
    }

    #[test]
    fn test_detect_audio_extension() {
        assert_eq!(detect_audio_extension(b"fLaC\x00\x00"), ".flac");
        assert_eq!(detect_audio_extension(b"ID3\x03\x00"), ".mp3");
        assert_eq!(detect_audio_extension(b"\x00\x00\x00\x20ftypM4A "), ".m4a");
        assert_eq!(detect_audio_extension(b"unknown bytes"), ".flac");
    }
}

//! Apple Music provider helpers and track ID extraction.

use regex::Regex;
use std::sync::LazyLock;

static APPLE_SONG_ID_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?i)(?:/song/(?:[^/]+/)?|/songs/)(\d+)").unwrap());
static APPLE_I_QUERY_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?i)[?&]i=(\d+)").unwrap());

pub fn extract_apple_track_id(url: &str) -> Option<String> {
    let trimmed = url.trim();
    if !trimmed.to_ascii_lowercase().contains("apple.com") {
        return None;
    }
    if let Some(caps) = APPLE_SONG_ID_RE.captures(trimmed) {
        if let Some(m) = caps.get(1) {
            return Some(m.as_str().to_string());
        }
    }
    if let Some(caps) = APPLE_I_QUERY_RE.captures(trimmed) {
        if let Some(m) = caps.get(1) {
            return Some(m.as_str().to_string());
        }
    }
    None
}

pub fn canonical_apple_song_url(track_id: &str) -> String {
    format!("https://music.apple.com/song/{}", track_id.trim())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_extract_apple_track_id() {
        assert_eq!(
            extract_apple_track_id("https://music.apple.com/us/album/song-name/123456?i=987654321"),
            Some("987654321".into())
        );
        assert_eq!(
            extract_apple_track_id("https://music.apple.com/song/1122334455"),
            Some("1122334455".into())
        );
        assert_eq!(
            extract_apple_track_id("https://music.apple.com/us/songs/998877"),
            Some("998877".into())
        );
        assert_eq!(
            extract_apple_track_id("https://open.spotify.com/track/123"),
            None
        );
    }
}

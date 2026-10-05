//! LRC parser, formatter, and LRCLib integration.

use crate::error::Result;
use regex::Regex;
use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct LyricsLine {
    #[serde(rename = "startTimeMs")]
    pub start_time_ms: String,
    pub words: String,
    #[serde(rename = "endTimeMs")]
    pub end_time_ms: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub translation: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct LyricsResponse {
    pub error: bool,
    #[serde(rename = "syncType")]
    pub sync_type: String,
    pub lines: Vec<LyricsLine>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct LrcLibResponse {
    pub id: Option<i64>,
    pub name: Option<String>,
    #[serde(rename = "trackName")]
    pub track_name: Option<String>,
    #[serde(rename = "artistName")]
    pub artist_name: Option<String>,
    #[serde(rename = "albumName")]
    pub album_name: Option<String>,
    pub duration: Option<f64>,
    pub instrumental: Option<bool>,
    #[serde(rename = "plainLyrics")]
    pub plain_lyrics: Option<String>,
    #[serde(rename = "syncedLyrics")]
    pub synced_lyrics: Option<String>,
}

pub fn parse_lrc(content: &str) -> Vec<LyricsLine> {
    let re = Regex::new(r"^\[(\d{1,2}):(\d{2})(?:\.(\d{2,3}))?\](.*)$").unwrap();
    let mut lines = Vec::new();

    for raw_line in content.lines() {
        let trimmed = raw_line.trim();
        if trimmed.is_empty() {
            continue;
        }

        if let Some(caps) = re.captures(trimmed) {
            let minutes: u64 = caps[1].parse().unwrap_or(0);
            let seconds: u64 = caps[2].parse().unwrap_or(0);
            let subsec_str = caps.get(3).map(|m| m.as_str()).unwrap_or("00");
            let millis: u64 = if subsec_str.len() == 2 {
                subsec_str.parse::<u64>().unwrap_or(0) * 10
            } else {
                subsec_str.parse::<u64>().unwrap_or(0)
            };

            let total_ms = (minutes * 60 + seconds) * 1000 + millis;
            let words = caps.get(4).map(|m| m.as_str().trim()).unwrap_or("").to_string();

            lines.push(LyricsLine {
                start_time_ms: total_ms.to_string(),
                words,
                end_time_ms: "0".to_string(),
                translation: None,
            });
        } else if !trimmed.starts_with('[') {
            lines.push(LyricsLine {
                start_time_ms: "0".to_string(),
                words: trimmed.to_string(),
                end_time_ms: "0".to_string(),
                translation: None,
            });
        }
    }

    lines
}

pub fn format_lrc(lines: &[LyricsLine]) -> String {
    let mut out = String::new();
    for line in lines {
        let ms: u64 = line.start_time_ms.parse().unwrap_or(0);
        let total_secs = ms / 1000;
        let mins = total_secs / 60;
        let secs = total_secs % 60;
        let hundredths = (ms % 1000) / 10;

        out.push_str(&format!(
            "[{:02}:{:02}.{:02}] {}\n",
            mins, secs, hundredths, line.words
        ));
    }
    out
}

pub fn save_lrc_file(lines: &[LyricsLine], output_path: &Path) -> Result<()> {
    if let Some(parent) = output_path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let formatted = format_lrc(lines);
    std::fs::write(output_path, formatted)?;
    Ok(())
}

fn url_encode(input: &str) -> String {
    let mut encoded = String::new();
    for byte in input.bytes() {
        match byte {
            b'a'..=b'z' | b'A'..=b'Z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                encoded.push(byte as char);
            }
            _ => {
                encoded.push_str(&format!("%{:02X}", byte));
            }
        }
    }
    encoded
}

pub async fn fetch_lrclib_lyrics(
    track_name: &str,
    artist_name: &str,
    album_name: Option<&str>,
    duration_secs: Option<u32>,
) -> Result<Option<LyricsResponse>> {
    let client = reqwest::Client::builder()
        .user_agent("Auralis")
        .build()?;

    let mut url = format!(
        "https://lrclib.net/api/get?artist_name={}&track_name={}",
        url_encode(artist_name),
        url_encode(track_name)
    );

    if let Some(album) = album_name {
        if !album.is_empty() {
            url.push_str(&format!("&album_name={}", url_encode(album)));
        }
    }

    if let Some(dur) = duration_secs {
        if dur > 0 {
            url.push_str(&format!("&duration={}", dur));
        }
    }

    let resp = client.get(&url).send().await?;
    if !resp.status().is_success() {
        return Ok(None);
    }

    let item: LrcLibResponse = resp.json().await?;

    if let Some(synced) = item.synced_lyrics {
        let parsed = parse_lrc(&synced);
        return Ok(Some(LyricsResponse {
            error: false,
            sync_type: "LINE_SYNCED".into(),
            lines: parsed,
        }));
    }

    if let Some(plain) = item.plain_lyrics {
        let lines = plain
            .lines()
            .map(|l| LyricsLine {
                start_time_ms: "0".into(),
                words: l.trim().into(),
                end_time_ms: "0".into(),
                translation: None,
            })
            .collect();

        return Ok(Some(LyricsResponse {
            error: false,
            sync_type: "UNSYNCED".into(),
            lines,
        }));
    }

    Ok(None)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_and_format_lrc() {
        let lrc_raw = "[01:23.45] First lyric line\n[02:00.10] Second lyric line";
        let parsed = parse_lrc(lrc_raw);
        assert_eq!(parsed.len(), 2);
        assert_eq!(parsed[0].words, "First lyric line");
        assert_eq!(parsed[0].start_time_ms, "83450");
        assert_eq!(parsed[1].words, "Second lyric line");
        assert_eq!(parsed[1].start_time_ms, "120100");

        let formatted = format_lrc(&parsed);
        assert!(formatted.contains("[01:23.45] First lyric line"));
        assert!(formatted.contains("[02:00.10] Second lyric line"));
    }

    #[test]
    fn test_url_encode() {
        assert_eq!(url_encode("Hello World!"), "Hello%20World%21");
    }
}

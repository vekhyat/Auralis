//! Sanitization rules for configuration settings.

use serde_json::{json, Map, Value};
use std::collections::HashSet;

pub fn normalize_custom_tidal_api(val: Option<&Value>) -> String {
    let Some(raw) = val.and_then(|v| v.as_str()) else {
        return String::new();
    };
    let trimmed = raw.trim().trim_end_matches('/');
    if trimmed.starts_with("https://") {
        trimmed.to_string()
    } else {
        String::new()
    }
}

pub fn sanitize_downloader(val: Option<&Value>) -> &'static str {
    let Some(raw) = val.and_then(|v| v.as_str()) else {
        return "auto";
    };
    match raw.trim().to_lowercase().as_str() {
        "tidal" => "tidal",
        "qobuz" => "qobuz",
        "amazon" => "amazon",
        "deezer" => "deezer",
        "apple" => "apple",
        "jiosaavn" => "jiosaavn",
        _ => "auto",
    }
}

pub fn sanitize_auto_order(val: Option<&Value>) -> String {
    let fallback = "tidal-qobuz-amazon";
    let Some(raw) = val.and_then(|v| v.as_str()) else {
        return fallback.to_string();
    };

    let allowed = ["tidal", "qobuz", "amazon", "deezer", "apple", "jiosaavn"];
    let mut seen = HashSet::new();
    let mut parts = Vec::new();

    for part in raw.trim().to_lowercase().split('-') {
        let trimmed = part.trim();
        if trimmed.is_empty() {
            continue;
        }
        if allowed.contains(&trimmed) && seen.insert(trimmed.to_string()) {
            parts.push(trimmed.to_string());
        }
    }

    if parts.len() < 2 {
        fallback.to_string()
    } else {
        parts.join("-")
    }
}

pub fn normalize_existing_file_check_mode(val: Option<&Value>) -> &'static str {
    let Some(raw) = val.and_then(|v| v.as_str()) else {
        return "filename";
    };
    match raw.trim().to_lowercase().as_str() {
        "isrc" | "upc" => "isrc",
        "hybrid" => "hybrid",
        _ => "filename",
    }
}

pub fn normalize_link_resolver(val: Option<&Value>) -> &'static str {
    let Some(raw) = val.and_then(|v| v.as_str()) else {
        return "deezer-songlink";
    };
    match raw.trim().to_lowercase().as_str() {
        "songlink" | "deezer-songlink" => "deezer-songlink",
        "songstats" => "songstats",
        _ => "deezer-songlink",
    }
}

pub fn sanitize_settings_map(settings: &mut Map<String, Value>) {
    let custom_tidal = normalize_custom_tidal_api(settings.get("customTidalApi"));
    settings.insert("customTidalApi".to_string(), json!(custom_tidal));

    let downloader = sanitize_downloader(settings.get("downloader"));
    settings.insert("downloader".to_string(), json!(downloader));

    let auto_order = sanitize_auto_order(settings.get("autoOrder"));
    settings.insert("autoOrder".to_string(), json!(auto_order));

    if let Some(gain_val) = settings.get("autoReplayGainMode") {
        let is_track = gain_val
            .as_str()
            .map(|s| s.trim().eq_ignore_ascii_case("track"))
            .unwrap_or(false);
        if is_track {
            settings.insert("autoReplayGainMode".to_string(), json!("track"));
        } else {
            settings.insert("autoReplayGainMode".to_string(), json!("album"));
        }
    }

    if let Some(mode_val) = settings.get("existingFileCheckMode") {
        let mode = normalize_existing_file_check_mode(Some(mode_val));
        settings.insert("existingFileCheckMode".to_string(), json!(mode));
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_sanitization_rules() {
        let mut map = Map::new();
        map.insert(
            "customTidalApi".into(),
            json!("  http://insecure.api.com/  "),
        );
        map.insert("downloader".into(), json!("TIDAL"));
        map.insert("autoOrder".into(), json!("qobuz-invalid-tidal-qobuz"));
        map.insert("autoReplayGainMode".into(), json!("TRACK"));
        map.insert("existingFileCheckMode".into(), json!("upc"));

        sanitize_settings_map(&mut map);

        assert_eq!(map["customTidalApi"], json!(""));
        assert_eq!(map["downloader"], json!("tidal"));
        assert_eq!(map["autoOrder"], json!("qobuz-tidal"));
        assert_eq!(map["autoReplayGainMode"], json!("track"));
        assert_eq!(map["existingFileCheckMode"], json!("isrc"));
    }
}

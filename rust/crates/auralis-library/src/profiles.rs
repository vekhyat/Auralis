//! Library Target Profiles.
//!
//! Controls path templates, character sanitization, case rules, and FAT-safe restrictions.

use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

pub const PROFILE_POWERAMP: &str = "poweramp";
pub const PROFILE_MEDIASTORE: &str = "mediastore";
pub const PROFILE_ROCKBOX: &str = "rockbox";
pub const PROFILE_MEDIASERVER: &str = "mediaserver";
pub const PROFILE_CUSTOM: &str = "custom";

/// ArtistMode controls how multiple artists are written for a target.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "lowercase")]
pub enum ArtistMode {
    #[default]
    Multi,
    Primary,
}

/// Profile bundles the conventions a player or device expects.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Profile {
    pub id: String,
    pub label: String,
    #[serde(rename = "folder_template")]
    pub folder_template: String,
    #[serde(rename = "file_template")]
    pub file_template: String,
    #[serde(rename = "max_cover_px")]
    pub max_cover_px: usize,
    #[serde(rename = "baseline_jpeg", default)]
    pub baseline_jpeg: bool,
    #[serde(rename = "folder_cover", default)]
    pub folder_cover: bool,
    #[serde(rename = "lrc_sidecars", default)]
    pub lrc_sidecars: bool,
    #[serde(rename = "artist_mode", default)]
    pub artist_mode: ArtistMode,
    #[serde(rename = "artist_separator", default)]
    pub artist_separator: String,
    #[serde(rename = "playlist_dir", default)]
    pub playlist_dir: String,
    #[serde(rename = "fat_safe", default)]
    pub fat_safe: bool,
    #[serde(rename = "max_component_bytes", default)]
    pub max_component_bytes: usize,
}

/// TrackFields are the tag values a profile template can reference.
#[derive(Debug, Clone, Default)]
pub struct TrackFields {
    pub title: String,
    pub artist: String,
    pub album_artist: String,
    pub album: String,
    pub year: String,
    pub track_number: usize,
    pub disc_number: usize,
    pub disc_total: usize,
}

pub fn builtin_profiles() -> Vec<Profile> {
    vec![
        Profile {
            id: PROFILE_POWERAMP.to_string(),
            label: "Poweramp".to_string(),
            folder_template: "{album_artist}/{album}".to_string(),
            file_template: "{disc}{track}. {title}".to_string(),
            max_cover_px: 1000,
            baseline_jpeg: false,
            folder_cover: true,
            lrc_sidecars: true,
            artist_mode: ArtistMode::Multi,
            artist_separator: "; ".to_string(),
            playlist_dir: "Playlists".to_string(),
            fat_safe: true,
            max_component_bytes: 255,
        },
        Profile {
            id: PROFILE_MEDIASTORE.to_string(),
            label: "Android (Samsung Music, Files)".to_string(),
            folder_template: "{album_artist}/{album}".to_string(),
            file_template: "{disc}{track}. {title}".to_string(),
            max_cover_px: 1000,
            baseline_jpeg: false,
            folder_cover: false,
            lrc_sidecars: false,
            artist_mode: ArtistMode::Primary,
            artist_separator: "; ".to_string(),
            playlist_dir: "Playlists".to_string(),
            fat_safe: true,
            max_component_bytes: 255,
        },
        Profile {
            id: PROFILE_ROCKBOX.to_string(),
            label: "Rockbox / iPod".to_string(),
            folder_template: "{album_artist}/{album}".to_string(),
            file_template: "{disc}{track}. {title}".to_string(),
            max_cover_px: 500,
            baseline_jpeg: true,
            folder_cover: true,
            lrc_sidecars: true,
            artist_mode: ArtistMode::Primary,
            artist_separator: "; ".to_string(),
            playlist_dir: "Playlists".to_string(),
            fat_safe: true,
            max_component_bytes: 255,
        },
        Profile {
            id: PROFILE_MEDIASERVER.to_string(),
            label: "Plex / Jellyfin / Navidrome".to_string(),
            folder_template: "{album_artist}/{album} ({year})".to_string(),
            file_template: "{disc}{track}. {title}".to_string(),
            max_cover_px: 0,
            baseline_jpeg: false,
            folder_cover: true,
            lrc_sidecars: true,
            artist_mode: ArtistMode::Multi,
            artist_separator: "; ".to_string(),
            playlist_dir: "Playlists".to_string(),
            fat_safe: false,
            max_component_bytes: 255,
        },
    ]
}

pub fn profiles() -> Vec<Profile> {
    builtin_profiles()
}

pub fn profile_by_id(id: &str) -> Profile {
    for p in builtin_profiles() {
        if p.id == id {
            return p;
        }
    }
    builtin_profiles().into_iter().next().unwrap()
}

impl Profile {
    /// Builds the slash-separated path (without extension) for a track under a music root.
    pub fn relative_path(&self, t: &TrackFields) -> String {
        let mut album_artist = first_non_empty(&[&t.album_artist, &t.artist, "Unknown Artist"]);
        if self.artist_mode == ArtistMode::Primary {
            album_artist = primary_artist(&album_artist);
        }
        let disc = if t.disc_total > 1 && t.disc_number > 0 {
            format!("{}-", t.disc_number)
        } else {
            String::new()
        };
        let track = if t.track_number > 0 {
            format!("{:02}", t.track_number)
        } else {
            String::new()
        };
        let mut values: BTreeMap<&str, String> = BTreeMap::new();
        values.insert("album_artist", album_artist.clone());
        values.insert("artist", first_non_empty(&[&t.artist, &album_artist]));
        values.insert("album", first_non_empty(&[&t.album, "Unknown Album"]));
        values.insert("year", t.year.clone());
        values.insert("disc", disc);
        values.insert("track", track);
        values.insert("title", first_non_empty(&[&t.title, "Unknown Title"]));

        let mut parts = Vec::new();
        for seg in self.folder_template.split('/') {
            let name = self.sanitize_component(&expand(seg, &values));
            if !name.is_empty() {
                parts.push(name);
            }
        }
        let file = expand(&self.file_template, &values);
        let trimmed_file = file.trim_start_matches(['.', ' ', '-']);
        parts.push(self.sanitize_component(trimmed_file));
        parts.join("/")
    }

    /// Makes one path component safe for the profile's target.
    pub fn sanitize_component(&self, name: &str) -> String {
        const FAT_ILLEGAL: &str = "<>:\"/\\|?*";

        let mut b = String::new();
        for r in name.chars() {
            if (r as u32) < 0x20 || (r as u32) == 0x7f {
                continue;
            }
            if r == '/' || r == '\\' {
                b.push(' ');
            } else if self.fat_safe && FAT_ILLEGAL.contains(r) {
                if r == ':' {
                    b.push_str(" -");
                } else {
                    b.push(' ');
                }
            } else {
                b.push(r);
            }
        }

        let fields: Vec<&str> = b.split_whitespace().collect();
        let mut out = fields.join(" ");

        out = out.trim_end_matches(['.', ' ']).to_string();
        out = out.trim_start_matches(' ').to_string();

        let base = out.split('.').next().unwrap_or("").to_ascii_uppercase();
        if is_reserved_windows_name(&base) {
            out = format!("_{out}");
        }

        if self.max_component_bytes > 0 {
            out = truncate_utf8(&out, self.max_component_bytes);
            out = out.trim_end_matches(['.', ' ']).to_string();
        }

        out
    }

    /// Sanitises each component of a slash-separated path.
    pub fn sanitize_relative_path(&self, rel: &str) -> String {
        let rel = rel.replace('\\', "/");
        let mut parts = Vec::new();
        for seg in rel.split('/') {
            if seg.is_empty() || seg == "." || seg == ".." {
                continue;
            }
            let clean = self.sanitize_component(seg);
            if !clean.is_empty() && clean != "." && clean != ".." {
                parts.push(clean);
            }
        }
        parts.join("/")
    }
}

fn expand(template: &str, values: &BTreeMap<&str, String>) -> String {
    let mut out = template.to_string();
    for (k, v) in values {
        out = out.replace(&format!("{{{k}}}"), v);
    }
    out = out.replace("()", "");
    out.trim().to_string()
}

pub fn primary_artist(artists: &str) -> String {
    let lower = artists.to_lowercase();
    let mut cut = artists.len();
    for sep in &[";", " feat. ", " ft. ", " featuring "] {
        if let Some(idx) = lower.find(sep) {
            if idx > 0 && idx < cut {
                cut = idx;
            }
        }
    }
    artists[..cut].trim().to_string()
}

fn first_non_empty(candidates: &[&str]) -> String {
    for c in candidates {
        let trimmed = c.trim();
        if !trimmed.is_empty() {
            return trimmed.to_string();
        }
    }
    String::new()
}

fn is_reserved_windows_name(name: &str) -> bool {
    const RESERVED: &[&str] = &[
        "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8",
        "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
    ];
    RESERVED.contains(&name)
}

fn truncate_utf8(s: &str, max_bytes: usize) -> String {
    if s.len() <= max_bytes {
        return s.to_string();
    }
    let mut cut = max_bytes;
    while cut > 0 && !s.is_char_boundary(cut) {
        cut -= 1;
    }
    s[..cut].to_string()
}

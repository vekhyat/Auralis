use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

/// Severity represents the urgency of an issue.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Severity {
    Info = 0,
    Warning = 1,
    Error = 2,
}

/// Issue represents a single defect or inconsistency detected in the library.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Issue {
    #[serde(default)]
    pub id: String,
    #[serde(rename = "rule_id")]
    pub rule_id: String,
    pub severity: Severity,
    pub path: String,
    #[serde(rename = "album_dir")]
    pub album_dir: String,
    pub message: String,
    pub fixable: bool,
}

/// Track describes a single audio file and its parsed/normalized metadata.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, Default)]
pub struct Track {
    pub path: String,
    #[serde(rename = "rel_path")]
    pub rel_path: String,
    pub format: String,
    pub title: String,
    pub artist: String,
    #[serde(rename = "album_artist")]
    pub album_artist: String,
    pub album: String,
    pub year: String,
    pub isrc: String,
    #[serde(rename = "track_number")]
    pub track_number: usize,
    #[serde(rename = "track_total")]
    pub track_total: usize,
    #[serde(rename = "disc_number")]
    pub disc_number: usize,
    #[serde(rename = "disc_total")]
    pub disc_total: usize,
    pub compilation: bool,
    #[serde(rename = "duration_seconds")]
    pub duration_seconds: f64,
    #[serde(rename = "bit_depth")]
    pub bit_depth: usize,
    pub size: i64,
    #[serde(rename = "mod_time_unix_nano")]
    pub mod_time_unix_nano: i64,
    #[serde(rename = "cover_bytes")]
    pub cover_bytes: usize,
    #[serde(rename = "cover_width")]
    pub cover_width: usize,
    #[serde(rename = "cover_height")]
    pub cover_height: usize,
    #[serde(rename = "folder_cover")]
    pub folder_cover: bool,
    pub lrc: bool,
    #[serde(rename = "track_number_raw")]
    pub track_number_raw: String,
    #[serde(rename = "disc_number_raw")]
    pub disc_number_raw: String,
}

impl Track {
    /// Returns the directory containing this track.
    pub fn album_dir(&self) -> String {
        std::path::Path::new(&self.path)
            .parent()
            .map(|p| p.to_string_lossy().to_string())
            .unwrap_or_default()
    }
}

/// Scan holds the results of a library scan.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, Default)]
pub struct Scan {
    pub root: String,
    pub tracks: Vec<Track>,
    pub dirs: Vec<String>,
    #[serde(rename = "stray_files")]
    pub stray_files: Vec<String>,
    #[serde(rename = "scanned_at")]
    pub scanned_at: DateTime<Utc>,
}

/// ScanProgress communicates scan progress to a caller.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
pub struct ScanProgress {
    pub phase: String,
    pub current: usize,
    pub total: usize,
    pub path: String,
}

/// Operation describes a proposed change to the music library.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
pub struct Operation {
    #[serde(rename = "type")]
    pub op_type: String,
    pub path: String,
    #[serde(rename = "new_path", skip_serializing_if = "Option::is_none")]
    pub new_path: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub set: Option<BTreeMap<String, String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub old: Option<BTreeMap<String, String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub delete: Option<Vec<String>>,
    #[serde(default)]
    pub reason: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(default)]
    pub size: i64,
    #[serde(rename = "mod_time_unix_nano", default)]
    pub mod_time: i64,
}

/// Plan is the ordered set of operations produced by BuildPlan.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
pub struct Plan {
    #[serde(default)]
    pub operations: Vec<Operation>,
}

/// ApplyResult reports what happened when a plan was executed.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
pub struct ApplyResult {
    #[serde(rename = "batch_id")]
    pub batch_id: String,
    pub applied: usize,
    pub skipped: usize,
    #[serde(default)]
    pub errors: Vec<String>,
}

/// PlaylistTrack holds metadata for writing playlist entries.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, Default)]
pub struct PlaylistTrack {
    pub path: String,
    pub title: String,
    pub artist: String,
    #[serde(rename = "duration_seconds")]
    pub duration_seconds: f64,
}

/// PlaylistSkip records a track that could not be written to the playlist.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PlaylistSkip {
    pub path: String,
    pub reason: String,
}

/// PlaylistResult reports the outcome of WritePlaylist.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PlaylistResult {
    pub path: String,
    pub written: usize,
    #[serde(default)]
    pub skipped: Vec<PlaylistSkip>,
}

/// PlaylistMode defines how track paths are represented in M3U8 playlists.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum PlaylistMode {
    Device,
    Root,
    Local,
    Relative,
}

/// LibraryReport summarizes a library's health and detected issues.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct LibraryReport {
    pub root: String,
    #[serde(rename = "profile_id")]
    pub profile_id: String,
    #[serde(rename = "health_score")]
    pub health_score: i64,
    #[serde(rename = "total_tracks")]
    pub total_tracks: usize,
    #[serde(rename = "total_albums")]
    pub total_albums: usize,
    #[serde(default)]
    pub issues: Vec<Issue>,
}

#[inline]
fn is_zero(v: &i64) -> bool {
    *v == 0
}

fn deserialize_old_map<'de, D>(
    deserializer: D,
) -> Result<BTreeMap<String, Option<Vec<String>>>, D::Error>
where
    D: serde::Deserializer<'de>,
{
    let opt = Option::<BTreeMap<String, Option<Vec<String>>>>::deserialize(deserializer)?;
    Ok(opt.unwrap_or_default())
}

/// JournalEntry records one applied operation for undo purposes.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
pub struct JournalEntry {
    #[serde(rename = "batch_id")]
    pub batch_id: String,
    pub time: String,
    #[serde(rename = "type")]
    pub op_type: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub path: String,
    #[serde(rename = "new_path", default, skip_serializing_if = "String::is_empty")]
    pub new_path: String,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub set: BTreeMap<String, String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub delete: Vec<String>,
    #[serde(
        default,
        skip_serializing_if = "BTreeMap::is_empty",
        deserialize_with = "deserialize_old_map"
    )]
    pub old: BTreeMap<String, Option<Vec<String>>>,
    #[serde(default, skip_serializing_if = "is_zero")]
    pub size: i64,
    #[serde(
        rename = "mod_time_unix_nano",
        default,
        skip_serializing_if = "is_zero"
    )]
    pub mod_time: i64,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub error: String,
}

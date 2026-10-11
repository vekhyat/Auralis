//! Provider domain models.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct TrackMetadata {
    pub isrc: String,
    pub title: String,
    pub artists: Vec<String>,
    pub album: String,
    pub duration_ms: u64,
    pub cover_url: Option<String>,
    pub spotify_id: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ResolvedStream {
    pub stream_url: String,
    pub format: String,
    pub bitrate_kbps: u32,
    pub is_encrypted: bool,
    pub encryption_key: Option<Vec<u8>>,
}

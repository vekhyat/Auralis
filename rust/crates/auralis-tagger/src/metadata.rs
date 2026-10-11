//! Domain metadata models and selection configuration for audio tags.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct AudioMetadata {
    pub title: Option<String>,
    pub artist: Option<String>,
    pub artists: Option<String>,
    pub album: Option<String>,
    pub album_artist: Option<String>,
    pub date: Option<String>,
    pub year: Option<u32>,
    pub track_number: Option<u32>,
    pub total_tracks: Option<u32>,
    pub disc_number: Option<u32>,
    pub total_discs: Option<u32>,
    pub genre: Option<String>,
    pub composer: Option<String>,
    pub copyright: Option<String>,
    pub publisher: Option<String>,
    pub isrc: Option<String>,
    pub upc: Option<String>,
    pub comment: Option<String>,
    pub lyrics: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MetadataTagSelection {
    pub title: bool,
    pub artist: bool,
    pub album: bool,
    pub album_artist: bool,
    pub date: bool,
    pub track_number: bool,
    pub disc_number: bool,
    pub genre: bool,
    pub composer: bool,
    pub copyright: bool,
    pub label: bool,
    pub isrc: bool,
    pub upc: bool,
    pub comment: bool,
}

impl Default for MetadataTagSelection {
    fn default() -> Self {
        Self {
            title: true,
            artist: true,
            album: true,
            album_artist: true,
            date: true,
            track_number: true,
            disc_number: true,
            genre: true,
            composer: true,
            copyright: true,
            label: true,
            isrc: true,
            upc: true,
            comment: true,
        }
    }
}

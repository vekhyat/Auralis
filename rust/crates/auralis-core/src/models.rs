//! Core domain models for Auralis.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct HistoryItem {
    pub id: String,
    pub spotify_id: String,
    pub title: String,
    pub artists: String,
    pub album: String,
    pub duration_str: String,
    pub cover_url: String,
    pub quality: String,
    pub format: String,
    pub path: String,
    pub source: String,
    pub timestamp: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct FetchHistoryItem {
    pub id: String,
    pub url: String,
    #[serde(rename = "type")]
    pub item_type: String,
    pub name: String,
    pub info: String,
    pub image: String,
    pub data: String,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub is_explicit: bool,
    pub timestamp: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct RecentFetchItem {
    pub id: String,
    pub url: String,
    #[serde(rename = "type")]
    pub item_type: String,
    pub name: String,
    pub artist: String,
    pub image: String,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub is_explicit: bool,
    pub timestamp: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct IsrcCacheEntry {
    pub track_id: String,
    pub isrc: String,
    pub updated_at: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct LibraryIndexEntry {
    pub root_key: String,
    pub path: String,
    pub path_key: String,
    pub filename: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub spotify_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub isrc: String,
    pub size: i64,
    pub modified_unix_nano: i64,
    pub metadata_indexed: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct LibraryIndexRootState {
    pub root: String,
    pub root_key: String,
    pub level: i32,
    pub scanned_at: i64,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct LibraryIndexLookupRequest {
    #[serde(default)]
    pub mode: String,
    #[serde(default)]
    pub spotify_id: String,
    #[serde(default)]
    pub isrc: String,
    #[serde(default)]
    pub filenames: Vec<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct QueueApplyRequest {
    #[serde(default)]
    pub upsert: Vec<serde_json::Value>,
    #[serde(default)]
    pub remove: Vec<String>,
    #[serde(default)]
    pub order: Vec<String>,
}

//! Spotify Web Player metadata client.
//!
//! Rust port of `backend/spotfetch.go`: web-player access tokens (TOTP based),
//! `client-token` exchange, Pathfinder queries, Spotify Web API reads, and the
//! response filters for tracks, albums, playlists, and artists.
//!
//! All parsing helpers are pure and hermetic; only the `SpotifyClient` network
//! methods perform I/O.

use std::collections::HashMap;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::error::{ProviderError, Result};

/// Default Spotify web-player client ID used for the `client-token` exchange.
pub const SPOTIFY_WEB_CLIENT_ID: &str = "d8a5dc950d20477a84061300ce696964";
/// Legacy web-player token endpoint kept for compatibility.
pub const ACCESS_TOKEN_URL: &str = "https://open.spotify.com/get_access_token";
/// Current web-player token endpoint (TOTP authenticated).
pub const API_TOKEN_URL: &str = "https://open.spotify.com/api/token";
/// Client-token exchange endpoint.
pub const CLIENT_TOKEN_URL: &str = "https://clienttoken.spotify.com/v1/clienttoken";
/// Partner (Pathfinder) query endpoint.
pub const PATHFINDER_URL: &str = "https://api-partner.spotify.com/pathfinder/v2/query";
/// Public Spotify Web API base.
pub const WEB_API_BASE: &str = "https://api.spotify.com/v1";
/// Web-player session page, used to bootstrap device/client-version info.
pub const SESSION_URL: &str = "https://open.spotify.com";

const USER_AGENT: &str = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36";
/// Grace window subtracted from cached token expiry before reuse.
const TOKEN_CACHE_SKEW: Duration = Duration::from_secs(30);
/// Fallback token lifetime when the response carries no expiry timestamp.
const DEFAULT_TOKEN_TTL: Duration = Duration::from_secs(55 * 60);
/// TOTP shared secret for the web-player token endpoint (mirrors Go source).
const SPOTIFY_TOTP_SECRET: &str = "GM3TMMJTGYZTQNZVGM4DINJZHA4TGOBYGMZTCMRTGEYDSMJRHE4TEOBUG4YTCMRUGQ4DQOJUGQYTAMRRGA2TCMJSHE3TCMBY";
/// TOTP version reported to the token endpoint (mirrors Go source).
const SPOTIFY_TOTP_VERSION: u32 = 61;

// ---------------------------------------------------------------------------
// Entity references
// ---------------------------------------------------------------------------

/// Spotify entity kinds supported by this client.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SpotifyEntityType {
    Track,
    Album,
    Playlist,
    Artist,
}

impl SpotifyEntityType {
    /// URL/URI path segment for this entity kind.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Track => "track",
            Self::Album => "album",
            Self::Playlist => "playlist",
            Self::Artist => "artist",
        }
    }

    fn from_str(s: &str) -> Option<Self> {
        match s {
            "track" => Some(Self::Track),
            "album" => Some(Self::Album),
            "playlist" => Some(Self::Playlist),
            "artist" => Some(Self::Artist),
            _ => None,
        }
    }
}

/// A parsed Spotify entity reference: kind plus bare ID.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SpotifyRef {
    pub entity: SpotifyEntityType,
    pub id: String,
}

fn valid_spotify_id(id: &str) -> bool {
    !id.is_empty() && id.chars().all(|c| c.is_ascii_alphanumeric())
}

/// Parse an `open.spotify.com` URL or a `spotify:<kind>:<id>` URI.
///
/// Accepts query strings, fragments, `intl-*` locale prefixes, and `/embed/`
/// URLs. Returns `None` for unknown kinds, bad hosts, or malformed IDs.
#[must_use]
pub fn parse_spotify_ref(input: &str) -> Option<SpotifyRef> {
    let s = input.trim();
    if s.is_empty() {
        return None;
    }
    if let Some(rest) = s.strip_prefix("spotify:") {
        let mut parts = rest.split(':');
        let kind = parts.next().unwrap_or_default();
        let id = parts.next().unwrap_or_default();
        if parts.next().is_some() {
            return None;
        }
        let entity = SpotifyEntityType::from_str(kind)?;
        if !valid_spotify_id(id) {
            return None;
        }
        return Some(SpotifyRef {
            entity,
            id: id.to_string(),
        });
    }

    let lower = s.to_lowercase();
    let marker = "open.spotify.com/";
    let pos = lower.find(marker)?;
    let mut after = &s[pos + marker.len()..];
    // Strip locale prefixes such as `intl-de/`.
    if after.len() > 6 && after.to_lowercase().starts_with("intl-") {
        if let Some(slash) = after.find('/') {
            after = &after[slash + 1..];
        }
    }
    let mut segments = after.split('/');
    let mut first = segments.next().unwrap_or_default();
    // Support `/embed/track/<id>` player URLs.
    if first.eq_ignore_ascii_case("embed") {
        first = segments.next().unwrap_or_default();
    }
    let entity = SpotifyEntityType::from_str(&first.to_lowercase())?;
    let raw_id = segments.next().unwrap_or_default();
    let id = raw_id.split(['?', '#']).next().unwrap_or_default().trim();
    if !valid_spotify_id(id) {
        return None;
    }
    Some(SpotifyRef {
        entity,
        id: id.to_string(),
    })
}

/// Build the canonical `open.spotify.com` URL for an entity reference.
#[must_use]
pub fn canonical_open_url(entity: SpotifyEntityType, id: &str) -> String {
    format!("https://open.spotify.com/{}/{}", entity.as_str(), id)
}

// ---------------------------------------------------------------------------
// Small JSON helpers
// ---------------------------------------------------------------------------

fn str_field(value: &Value, key: &str) -> String {
    value
        .get(key)
        .and_then(Value::as_str)
        .unwrap_or_default()
        .to_string()
}

fn opt_string(value: &Value, key: &str) -> Option<String> {
    value.get(key).and_then(Value::as_str).map(str::to_string)
}

fn num_f64(value: &Value) -> f64 {
    if let Some(n) = value.as_f64() {
        return n;
    }
    if let Some(n) = value.as_u64() {
        return n as f64;
    }
    if let Some(n) = value.as_i64() {
        return n as f64;
    }
    value
        .as_str()
        .and_then(|s| s.parse::<f64>().ok())
        .unwrap_or_default()
}

fn num_field(value: &Value, key: &str) -> f64 {
    value.get(key).map(num_f64).unwrap_or_default()
}

fn uint_field(value: &Value, key: &str) -> Option<u64> {
    let raw = value.get(key)?;
    if let Some(n) = raw.as_u64() {
        return Some(n);
    }
    if let Some(n) = raw.as_i64() {
        return u64::try_from(n).ok();
    }
    if let Some(n) = raw.as_f64() {
        if n >= 0.0 {
            return Some(n as u64);
        }
        return None;
    }
    raw.as_str()?.parse::<u64>().ok()
}

fn id_from_uri(uri: &str) -> String {
    uri.rsplit(':')
        .next()
        .unwrap_or_default()
        .trim()
        .to_string()
}

fn spotify_id_of(item: &Value) -> String {
    let id = str_field(item, "id");
    if !id.trim().is_empty() {
        return id.trim().to_string();
    }
    id_from_uri(&str_field(item, "uri"))
}

/// Format milliseconds as `m:ss` (mirrors Go `extractDuration`).
#[must_use]
pub fn format_duration(ms: u64) -> String {
    let total_seconds = ms / 1000;
    format!("{}:{:02}", total_seconds / 60, total_seconds % 60)
}

/// Minimal HTML entity unescape for playlist descriptions.
#[must_use]
pub fn html_unescape(input: &str) -> String {
    input
        .replace("&lt;", "<")
        .replace("&gt;", ">")
        .replace("&quot;", "\"")
        .replace("&#39;", "'")
        .replace("&#x27;", "'")
        .replace("&amp;", "&")
}

/// Strip HTML tags (used for artist biographies).
#[must_use]
pub fn strip_html_tags(input: &str) -> String {
    let mut out = String::with_capacity(input.len());
    let mut in_tag = false;
    for ch in input.chars() {
        match ch {
            '<' => in_tag = true,
            '>' => in_tag = false,
            _ if !in_tag => out.push(ch),
            _ => {}
        }
    }
    out
}

fn normalize_release_date(raw: &str) -> String {
    raw.split(['T', ' '])
        .next()
        .unwrap_or_default()
        .trim()
        .to_string()
}

fn release_year_from_date(date: &str) -> Option<i32> {
    date.split('-').next()?.trim().parse::<i32>().ok()
}

/// Extract the release date from a Pathfinder `date` object.
fn pathfinder_release_date(date: &Value) -> Option<String> {
    if let Some(iso) = date.get("isoString").and_then(Value::as_str) {
        if !iso.trim().is_empty() {
            return Some(normalize_release_date(iso));
        }
    }
    let year = date.get("year").and_then(Value::as_str).unwrap_or_default();
    if year.trim().is_empty() {
        return None;
    }
    let month = date
        .get("month")
        .and_then(Value::as_str)
        .unwrap_or_default()
        .parse::<u32>()
        .ok();
    let day = date
        .get("day")
        .and_then(Value::as_str)
        .unwrap_or_default()
        .parse::<u32>()
        .ok();
    match (month, day) {
        (Some(m), Some(d)) => {
            let year = year.trim();
            Some(format!("{year}-{m:02}-{d:02}"))
        }
        _ => Some(year.trim().to_string()),
    }
}

// ---------------------------------------------------------------------------
// Shared metadata shapes
// ---------------------------------------------------------------------------

/// A named artist with an optional Spotify ID.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ArtistRef {
    pub id: Option<String>,
    pub name: String,
}

/// Cover art in web-player resolutions.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct CoverImages {
    pub small: Option<String>,
    pub medium: Option<String>,
    pub large: Option<String>,
}

impl CoverImages {
    /// Highest-quality URL available: large, then medium, then small.
    #[must_use]
    pub fn highest(&self) -> Option<&str> {
        self.large
            .as_deref()
            .or(self.medium.as_deref())
            .or(self.small.as_deref())
    }
}

/// Join artist names with a separator (defaults to `", "` handling upstream).
#[must_use]
pub fn join_artist_names(artists: &[ArtistRef], separator: &str) -> String {
    artists
        .iter()
        .map(|a| a.name.as_str())
        .collect::<Vec<_>>()
        .join(separator)
}

/// Extract artists from a Pathfinder `artists` container (`items[].profile`).
#[must_use]
pub fn extract_artists(container: &Value) -> Vec<ArtistRef> {
    let mut artists = Vec::new();
    if let Some(items) = container.get("items").and_then(Value::as_array) {
        for item in items {
            let name = item
                .get("profile")
                .map(|p| str_field(p, "name"))
                .unwrap_or_default();
            if name.trim().is_empty() {
                continue;
            }
            let id = spotify_id_of(item);
            artists.push(ArtistRef {
                id: if id.is_empty() { None } else { Some(id) },
                name,
            });
        }
    }
    artists
}

fn extract_image_id(url: &str) -> Option<&str> {
    for prefix in ["ab67616d0000b273", "ab67616d00001e02", "ab67616d00004851"] {
        if let Some(pos) = url.find(prefix) {
            let rest = &url[pos + prefix.len()..];
            let end = rest.find(['?', '#', '/']).unwrap_or(rest.len());
            let id = &rest[..end];
            if !id.is_empty() {
                return Some(id);
            }
        }
    }
    None
}

fn collect_cover_sources(container: &Value) -> Vec<&Value> {
    if let Some(sources) = container.get("sources").and_then(Value::as_array) {
        return sources.iter().collect();
    }
    if let Some(sources) = container
        .pointer("/squareCoverImage/image/data/sources")
        .and_then(Value::as_array)
    {
        return sources.iter().collect();
    }
    Vec::new()
}

/// Extract web-player cover resolutions from a Pathfinder cover container.
///
/// Mirrors Go `extractCoverImage`: keeps sources larger than 64px, maps width
/// 300 to small and 640 to medium, and synthesizes the high-resolution URL
/// from the embedded image ID.
#[must_use]
pub fn extract_cover_images(container: &Value) -> Option<CoverImages> {
    let sources = collect_cover_sources(container);
    if sources.is_empty() {
        return None;
    }
    let mut ranked: Vec<(&str, f64)> = Vec::new();
    let mut fallback: Option<&str> = None;
    for source in sources {
        let url = source
            .get("url")
            .and_then(Value::as_str)
            .unwrap_or_default();
        if url.is_empty() {
            continue;
        }
        let mut width = num_field(source, "width");
        if width == 0.0 {
            width = num_field(source, "maxWidth");
        }
        let mut height = num_field(source, "height");
        if height == 0.0 {
            height = num_field(source, "maxHeight");
        }
        if (width > 64.0 && height > 64.0) || (width == 0.0 && height == 0.0) {
            if width == 0.0 && height == 0.0 {
                fallback.get_or_insert(url);
            } else {
                ranked.push((url, width));
            }
        }
    }
    if ranked.is_empty() && fallback.is_none() {
        return None;
    }
    ranked.sort_by(|a, b| a.1.total_cmp(&b.1));

    let mut covers = CoverImages::default();
    let mut image_id: Option<&str> = None;
    for (url, width) in &ranked {
        if *width == 300.0 && covers.small.is_none() {
            covers.small = Some((*url).to_string());
        } else if *width == 640.0 && covers.medium.is_none() {
            covers.medium = Some((*url).to_string());
        }
        if image_id.is_none() {
            image_id = extract_image_id(url);
        }
    }
    if let Some(id) = image_id {
        covers.large = Some(format!("https://i.scdn.co/image/ab67616d000082c1{id}"));
    } else if let Some((url, _)) = ranked.last() {
        // No embedded image ID: fall back to the widest source as large.
        if covers.large.is_none() {
            covers.large = Some((*url).to_string());
        }
    }
    if covers.small.is_none() && covers.medium.is_none() && covers.large.is_none() {
        if let Some(url) = fallback {
            covers.small = Some(url.to_string());
            covers.medium = Some(url.to_string());
            covers.large = Some(url.to_string());
        }
    }
    if covers.small.is_none() && covers.medium.is_none() && covers.large.is_none() {
        return None;
    }
    Some(covers)
}

/// Build cover resolutions from a Spotify Web API `images` array.
#[must_use]
pub fn cover_from_web_api_images(images: &Value) -> Option<CoverImages> {
    let items = images.as_array()?;
    let mut ranked: Vec<(&str, f64)> = Vec::new();
    for item in items {
        let url = item.get("url").and_then(Value::as_str).unwrap_or_default();
        if url.is_empty() {
            continue;
        }
        let width = num_field(item, "width");
        let height = num_field(item, "height");
        if (width > 64.0 && height > 64.0) || (width == 0.0 && height == 0.0) {
            ranked.push((url, width));
        }
    }
    if ranked.is_empty() {
        return None;
    }
    ranked.sort_by(|a, b| a.1.total_cmp(&b.1));
    let mut covers = CoverImages::default();
    for (url, width) in &ranked {
        if *width == 300.0 && covers.small.is_none() {
            covers.small = Some((*url).to_string());
        } else if *width == 640.0 && covers.medium.is_none() {
            covers.medium = Some((*url).to_string());
        }
    }
    // Highest-resolution source wins for large.
    if let Some((url, _)) = ranked.last() {
        covers.large = Some((*url).to_string());
    }
    if covers.small.is_none() {
        covers.small.clone_from(&covers.large);
    }
    if covers.medium.is_none() {
        covers.medium.clone_from(&covers.large);
    }
    Some(covers)
}

/// First image URL from playlist-style `images`/`imagesV2` containers.
fn first_image_url(container: &Value) -> Option<String> {
    fn first_url(sources: &[Value]) -> Option<String> {
        sources
            .iter()
            .filter_map(|s| opt_string(s, "url"))
            .find(|url| !url.is_empty())
    }
    if let Some(items) = container.get("items").and_then(Value::as_array) {
        for item in items {
            if let Some(sources) = item.get("sources").and_then(Value::as_array) {
                if let Some(url) = first_url(sources) {
                    return Some(url);
                }
            }
            if let Some(url) = opt_string(item, "url").filter(|url| !url.is_empty()) {
                return Some(url);
            }
        }
    }
    container
        .get("sources")
        .and_then(Value::as_array)
        .and_then(|sources| first_url(sources))
}

// ---------------------------------------------------------------------------
// Track metadata
// ---------------------------------------------------------------------------

/// Filtered Spotify track metadata.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SpotifyTrack {
    pub id: String,
    pub title: String,
    pub artists: Vec<ArtistRef>,
    pub album: AlbumRef,
    pub duration_ms: u64,
    pub duration_display: String,
    pub isrc: Option<String>,
    pub cover: Option<CoverImages>,
    pub explicit: bool,
    pub track_number: u32,
    pub disc_number: u32,
    pub play_count: Option<String>,
}

/// Compact album reference attached to a track.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AlbumRef {
    pub id: String,
    pub name: String,
    pub artists: Vec<ArtistRef>,
    pub release_date: Option<String>,
    pub release_year: Option<i32>,
    pub total_tracks: Option<u64>,
}

fn album_ref_from_pathfinder(album: &Value) -> AlbumRef {
    let mut id = str_field(album, "id");
    if id.is_empty() {
        id = id_from_uri(&str_field(album, "uri"));
    }
    let release_date = pathfinder_release_date(album.get("date").unwrap_or(&Value::Null));
    let release_year = release_date.as_deref().and_then(release_year_from_date);
    let total_tracks = album
        .get("tracks")
        .and_then(|t| uint_field(t, "totalCount"));
    AlbumRef {
        id,
        name: str_field(album, "name"),
        artists: extract_artists(album.get("artists").unwrap_or(&Value::Null)),
        release_date,
        release_year,
        total_tracks,
    }
}

fn parse_pathfinder_track(track: &Value) -> Result<SpotifyTrack> {
    let mut artists = extract_artists(track.get("artists").unwrap_or(&Value::Null));
    if artists.is_empty() {
        artists = extract_artists(track.get("firstArtist").unwrap_or(&Value::Null));
        artists.extend(extract_artists(
            track.get("otherArtists").unwrap_or(&Value::Null),
        ));
    }
    let album_value = track.get("albumOfTrack").unwrap_or(&Value::Null);
    if artists.is_empty() && !album_value.is_null() {
        artists = extract_artists(album_value.get("artists").unwrap_or(&Value::Null));
    }

    let album = if album_value.is_null() {
        AlbumRef {
            id: String::new(),
            name: String::new(),
            artists: Vec::new(),
            release_date: None,
            release_year: None,
            total_tracks: None,
        }
    } else {
        album_ref_from_pathfinder(album_value)
    };

    let mut cover = track.get("visualIdentity").and_then(extract_cover_images);
    if cover.is_none() && !album_value.is_null() {
        cover = album_value.get("coverArt").and_then(extract_cover_images);
    }

    let duration_ms = track
        .get("duration")
        .and_then(|d| uint_field(d, "totalMilliseconds"))
        .unwrap_or_default();
    let explicit = track
        .get("contentRating")
        .map(|c| str_field(c, "label") == "EXPLICIT")
        .unwrap_or(false);
    let id = spotify_id_of(track);
    if id.is_empty() {
        return Err(ProviderError::ResolutionFailed(
            "pathfinder track is missing an id".to_string(),
        ));
    }
    Ok(SpotifyTrack {
        id,
        title: str_field(track, "name"),
        artists,
        album,
        duration_ms,
        duration_display: format_duration(duration_ms),
        isrc: None,
        cover,
        explicit,
        track_number: num_field(track, "trackNumber") as u32,
        disc_number: {
            let disc = num_field(track, "discNumber") as u32;
            if disc == 0 {
                1
            } else {
                disc
            }
        },
        play_count: opt_string(track, "playcount"),
    })
}

fn web_api_artists(value: &Value) -> Vec<ArtistRef> {
    value
        .get("artists")
        .and_then(Value::as_array)
        .map(|items| {
            items
                .iter()
                .filter_map(|a| {
                    let name = a.get("name").and_then(Value::as_str).unwrap_or_default();
                    if name.trim().is_empty() {
                        return None;
                    }
                    let id = a
                        .get("id")
                        .and_then(Value::as_str)
                        .filter(|s| !s.is_empty())
                        .map(str::to_string)
                        .or_else(|| {
                            let uri = a.get("uri").and_then(Value::as_str).unwrap_or_default();
                            let id = id_from_uri(uri);
                            if id.is_empty() {
                                None
                            } else {
                                Some(id)
                            }
                        });
                    Some(ArtistRef {
                        id,
                        name: name.to_string(),
                    })
                })
                .collect()
        })
        .unwrap_or_default()
}

fn parse_web_api_track(track: &Value) -> Result<SpotifyTrack> {
    let id = spotify_id_of(track);
    if id.is_empty() {
        return Err(ProviderError::ResolutionFailed(
            "web api track is missing an id".to_string(),
        ));
    }
    let album_value = track.get("album").unwrap_or(&Value::Null);
    let mut album_id = str_field(album_value, "id");
    if album_id.is_empty() {
        album_id = id_from_uri(&str_field(album_value, "uri"));
    }
    let release_date = opt_string(album_value, "release_date")
        .map(|d| normalize_release_date(&d))
        .filter(|d| !d.is_empty());
    let release_year = release_date.as_deref().and_then(release_year_from_date);
    let cover = album_value
        .get("images")
        .and_then(cover_from_web_api_images);
    let duration_ms = uint_field(track, "duration_ms").unwrap_or_default();
    let isrc = track
        .get("external_ids")
        .and_then(|e| opt_string(e, "isrc"))
        .filter(|s| !s.trim().is_empty());
    Ok(SpotifyTrack {
        id,
        title: str_field(track, "name"),
        artists: web_api_artists(track),
        album: AlbumRef {
            id: album_id,
            name: str_field(album_value, "name"),
            artists: web_api_artists(album_value),
            release_date,
            release_year,
            total_tracks: album_value
                .get("total_tracks")
                .and_then(uint_field_for_value),
        },
        duration_ms,
        duration_display: format_duration(duration_ms),
        isrc,
        cover,
        explicit: track
            .get("explicit")
            .and_then(Value::as_bool)
            .unwrap_or(false),
        track_number: uint_field(track, "track_number").unwrap_or_default() as u32,
        disc_number: uint_field(track, "disc_number").unwrap_or(1) as u32,
        play_count: None,
    })
}

fn uint_field_for_value(value: &Value) -> Option<u64> {
    if let Some(n) = value.as_u64() {
        return Some(n);
    }
    if let Some(n) = value.as_i64() {
        return u64::try_from(n).ok();
    }
    if let Some(n) = value.as_f64() {
        if n >= 0.0 {
            return Some(n as u64);
        }
    }
    None
}

/// Parse track metadata from either a Pathfinder (`data.trackUnion`) or a
/// Spotify Web API track payload.
pub fn parse_track(payload: &Value) -> Result<SpotifyTrack> {
    if let Some(track) = payload.pointer("/data/trackUnion") {
        if !track.is_null() {
            return parse_pathfinder_track(track);
        }
    }
    // Pathfinder responses are sometimes passed unwrapped in tests.
    match payload.get("trackUnion").filter(|track| !track.is_null()) {
        Some(track) => parse_pathfinder_track(track),
        None => parse_web_api_track(payload),
    }
}

// ---------------------------------------------------------------------------
// Album metadata
// ---------------------------------------------------------------------------

/// A single track inside an album listing.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SpotifyAlbumTrack {
    pub id: String,
    pub title: String,
    pub artists: Vec<ArtistRef>,
    pub duration_ms: u64,
    pub duration_display: String,
    pub explicit: bool,
    pub disc_number: u32,
    pub track_number: u32,
    pub play_count: Option<String>,
}

/// Filtered Spotify album metadata.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SpotifyAlbum {
    pub id: String,
    pub title: String,
    pub artists: Vec<ArtistRef>,
    pub cover: Option<CoverImages>,
    pub release_date: Option<String>,
    pub release_year: Option<i32>,
    pub label: Option<String>,
    pub total_discs: u32,
    pub tracks: Vec<SpotifyAlbumTrack>,
}

fn pathfinder_album_track(track: &Value, index: usize) -> Option<SpotifyAlbumTrack> {
    let id = spotify_id_of(track);
    if id.is_empty() {
        return None;
    }
    let duration_ms = track
        .get("duration")
        .and_then(|d| uint_field(d, "totalMilliseconds"))
        .unwrap_or_default();
    let disc = num_field(track, "discNumber") as u32;
    Some(SpotifyAlbumTrack {
        id,
        title: str_field(track, "name"),
        artists: extract_artists(track.get("artists").unwrap_or(&Value::Null)),
        duration_ms,
        duration_display: format_duration(duration_ms),
        explicit: track
            .get("contentRating")
            .map(|c| str_field(c, "label") == "EXPLICIT")
            .unwrap_or(false),
        disc_number: if disc == 0 { 1 } else { disc },
        track_number: {
            let n = num_field(track, "trackNumber") as u32;
            if n == 0 {
                index as u32 + 1
            } else {
                n
            }
        },
        play_count: opt_string(track, "playcount"),
    })
}

fn parse_pathfinder_album(album: &Value) -> Result<SpotifyAlbum> {
    let id = spotify_id_of(album);
    if id.is_empty() {
        return Err(ProviderError::ResolutionFailed(
            "pathfinder album is missing an id".to_string(),
        ));
    }
    let mut tracks = Vec::new();
    if let Some(items) = album.pointer("/tracksV2/items").and_then(Value::as_array) {
        for (index, item) in items.iter().enumerate() {
            if let Some(track) = item.get("track") {
                if let Some(parsed) = pathfinder_album_track(track, index) {
                    tracks.push(parsed);
                }
            }
        }
    }
    let release_date = pathfinder_release_date(album.get("date").unwrap_or(&Value::Null));
    let release_year = release_date.as_deref().and_then(release_year_from_date);
    let label = opt_string(album, "label").filter(|s| !s.trim().is_empty());
    Ok(SpotifyAlbum {
        id,
        title: str_field(album, "name"),
        artists: extract_artists(album.get("artists").unwrap_or(&Value::Null)),
        cover: album.get("coverArt").and_then(extract_cover_images),
        release_date,
        release_year,
        label,
        total_discs: album
            .get("discs")
            .and_then(|d| uint_field(d, "totalCount"))
            .unwrap_or(1) as u32,
        tracks,
    })
}

fn web_api_album_track(track: &Value, index: usize) -> Option<SpotifyAlbumTrack> {
    let id = spotify_id_of(track);
    if id.is_empty() {
        return None;
    }
    let duration_ms = uint_field(track, "duration_ms").unwrap_or_default();
    Some(SpotifyAlbumTrack {
        id,
        title: str_field(track, "name"),
        artists: web_api_artists(track),
        duration_ms,
        duration_display: format_duration(duration_ms),
        explicit: track
            .get("explicit")
            .and_then(Value::as_bool)
            .unwrap_or(false),
        disc_number: uint_field(track, "disc_number").unwrap_or(1) as u32,
        track_number: uint_field(track, "track_number").unwrap_or(index as u64 + 1) as u32,
        play_count: None,
    })
}

fn parse_web_api_album(album: &Value) -> Result<SpotifyAlbum> {
    let id = spotify_id_of(album);
    if id.is_empty() {
        return Err(ProviderError::ResolutionFailed(
            "web api album is missing an id".to_string(),
        ));
    }
    let mut tracks = Vec::new();
    if let Some(items) = album
        .get("tracks")
        .and_then(|t| t.get("items"))
        .and_then(Value::as_array)
    {
        for (index, track) in items.iter().enumerate() {
            if let Some(parsed) = web_api_album_track(track, index) {
                tracks.push(parsed);
            }
        }
    }
    let release_date = opt_string(album, "release_date")
        .map(|d| normalize_release_date(&d))
        .filter(|d| !d.is_empty());
    let release_year = release_date.as_deref().and_then(release_year_from_date);
    Ok(SpotifyAlbum {
        id,
        title: str_field(album, "name"),
        artists: web_api_artists(album),
        cover: album.get("images").and_then(cover_from_web_api_images),
        release_date,
        release_year,
        label: opt_string(album, "label").filter(|s| !s.trim().is_empty()),
        total_discs: 1,
        tracks,
    })
}

/// Parse album metadata from a Pathfinder (`data.albumUnion`) or Web API payload.
pub fn parse_album(payload: &Value) -> Result<SpotifyAlbum> {
    if let Some(album) = payload.pointer("/data/albumUnion") {
        if !album.is_null() {
            return parse_pathfinder_album(album);
        }
    }
    if let Some(album) = payload.get("albumUnion") {
        return parse_pathfinder_album(album);
    }
    parse_web_api_album(payload)
}

// ---------------------------------------------------------------------------
// Playlist metadata
// ---------------------------------------------------------------------------

/// Playlist owner profile.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PlaylistOwner {
    pub name: String,
    pub avatar: Option<String>,
}

/// A single track inside a playlist listing.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SpotifyPlaylistTrack {
    pub id: String,
    pub title: String,
    pub artists: Vec<ArtistRef>,
    pub album: String,
    pub album_id: String,
    pub duration_ms: u64,
    pub duration_display: String,
    pub explicit: bool,
    pub play_count: Option<String>,
    pub added_by: Option<String>,
    pub cover: Option<CoverImages>,
}

/// Filtered Spotify playlist metadata.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SpotifyPlaylist {
    pub id: String,
    pub name: String,
    pub description: String,
    pub owner: Option<PlaylistOwner>,
    pub cover_url: Option<String>,
    pub total_tracks: u64,
    pub followers: u64,
    pub tracks: Vec<SpotifyPlaylistTrack>,
}

fn pathfinder_playlist_track(item: &Value) -> Option<SpotifyPlaylistTrack> {
    let track = item.pointer("/itemV2/data")?;
    if track.is_null() {
        return None;
    }
    let title = str_field(track, "name");
    if title.trim().is_empty() {
        return None;
    }
    let artists = extract_artists(track.get("artists").unwrap_or(&Value::Null));
    let duration_ms = track
        .get("trackDuration")
        .and_then(|d| uint_field(d, "totalMilliseconds"))
        .unwrap_or_default();
    let album_value = track.get("albumOfTrack").unwrap_or(&Value::Null);
    let album_id = if album_value.is_null() {
        String::new()
    } else {
        spotify_id_of(album_value)
    };
    let (mut rank, mut added_by) = (None, None);
    if let Some(attrs) = item.get("attributes").and_then(Value::as_array) {
        for attr in attrs {
            match str_field(attr, "key").as_str() {
                "rank" => rank = Some(str_field(attr, "value")),
                "addedBy" | "added_by" => added_by = Some(str_field(attr, "value")),
                _ => {}
            }
        }
    }
    Some(SpotifyPlaylistTrack {
        id: spotify_id_of(track),
        title,
        artists,
        album: str_field(album_value, "name"),
        album_id,
        duration_ms,
        duration_display: format_duration(duration_ms),
        explicit: track
            .get("contentRating")
            .map(|c| str_field(c, "label") == "EXPLICIT")
            .unwrap_or(false),
        play_count: rank.filter(|s| !s.is_empty()),
        added_by: added_by.filter(|s| !s.is_empty()),
        cover: if album_value.is_null() {
            None
        } else {
            album_value.get("coverArt").and_then(extract_cover_images)
        },
    })
}

fn parse_pathfinder_playlist(playlist: &Value) -> Result<SpotifyPlaylist> {
    let owner = playlist
        .pointer("/ownerV2/data")
        .filter(|data| !data.is_null())
        .map(|data| PlaylistOwner {
            name: str_field(data, "name"),
            avatar: data
                .get("avatar")
                .and_then(|a| a.get("sources"))
                .and_then(Value::as_array)
                .and_then(|sources| sources.first())
                .and_then(|s| opt_string(s, "url")),
        });
    let mut images = playlist.get("images").unwrap_or(&Value::Null).clone();
    if images.is_null() {
        images = playlist.get("imagesV2").unwrap_or(&Value::Null).clone();
    }
    let cover_url = first_image_url(&images);
    let mut tracks = Vec::new();
    if let Some(items) = playlist.pointer("/content/items").and_then(Value::as_array) {
        for item in items {
            if let Some(parsed) = pathfinder_playlist_track(item) {
                tracks.push(parsed);
            }
        }
    }
    let total = playlist
        .get("content")
        .and_then(|c| uint_field(c, "totalCount"))
        .unwrap_or(tracks.len() as u64);
    let followers = match playlist.get("followers") {
        Some(Value::Object(map)) => map
            .get("totalCount")
            .map(num_f64)
            .unwrap_or_default()
            .max(0.0) as u64,
        Some(other) => num_f64(other).max(0.0) as u64,
        None => 0,
    };
    let id = spotify_id_of(playlist);
    if id.is_empty() {
        return Err(ProviderError::ResolutionFailed(
            "pathfinder playlist is missing an id".to_string(),
        ));
    }
    Ok(SpotifyPlaylist {
        id,
        name: str_field(playlist, "name"),
        description: html_unescape(&str_field(playlist, "description")),
        owner,
        cover_url,
        total_tracks: total,
        followers,
        tracks,
    })
}

fn web_api_playlist_track(item: &Value) -> Option<SpotifyPlaylistTrack> {
    let track = item.get("track").unwrap_or(item);
    if track.is_null() {
        return None;
    }
    let title = str_field(track, "name");
    if title.trim().is_empty() {
        return None;
    }
    let album_value = track.get("album").unwrap_or(&Value::Null);
    let duration_ms = uint_field(track, "duration_ms").unwrap_or_default();
    let added_by = item
        .get("added_by")
        .and_then(|a| {
            a.get("id")
                .and_then(Value::as_str)
                .or_else(|| a.get("display_name").and_then(Value::as_str))
        })
        .filter(|s| !s.is_empty())
        .map(str::to_string);
    Some(SpotifyPlaylistTrack {
        id: spotify_id_of(track),
        title,
        artists: web_api_artists(track),
        album: str_field(album_value, "name"),
        album_id: spotify_id_of(album_value),
        duration_ms,
        duration_display: format_duration(duration_ms),
        explicit: track
            .get("explicit")
            .and_then(Value::as_bool)
            .unwrap_or(false),
        play_count: None,
        added_by,
        cover: album_value
            .get("images")
            .and_then(cover_from_web_api_images),
    })
}

fn parse_web_api_playlist(playlist: &Value) -> Result<SpotifyPlaylist> {
    let id = spotify_id_of(playlist);
    if id.is_empty() {
        return Err(ProviderError::ResolutionFailed(
            "web api playlist is missing an id".to_string(),
        ));
    }
    let cover_url = playlist
        .get("images")
        .and_then(Value::as_array)
        .and_then(|images| images.first())
        .and_then(|img| opt_string(img, "url"));
    let mut tracks = Vec::new();
    if let Some(items) = playlist
        .get("tracks")
        .and_then(|t| t.get("items"))
        .and_then(Value::as_array)
    {
        for item in items {
            if let Some(parsed) = web_api_playlist_track(item) {
                tracks.push(parsed);
            }
        }
    }
    let total = playlist
        .get("tracks")
        .and_then(|t| uint_field(t, "total"))
        .unwrap_or(tracks.len() as u64);
    Ok(SpotifyPlaylist {
        id,
        name: str_field(playlist, "name"),
        description: html_unescape(&str_field(playlist, "description")),
        owner: playlist.get("owner").map(|o| PlaylistOwner {
            name: o
                .get("display_name")
                .and_then(Value::as_str)
                .or_else(|| o.get("id").and_then(Value::as_str))
                .unwrap_or_default()
                .to_string(),
            avatar: None,
        }),
        cover_url,
        total_tracks: total,
        followers: playlist
            .get("followers")
            .and_then(|f| uint_field(f, "total"))
            .unwrap_or_default(),
        tracks,
    })
}

/// Parse playlist metadata from a Pathfinder (`data.playlistV2`) or Web API payload.
pub fn parse_playlist(payload: &Value) -> Result<SpotifyPlaylist> {
    if let Some(playlist) = payload.pointer("/data/playlistV2") {
        if !playlist.is_null() {
            return parse_pathfinder_playlist(playlist);
        }
    }
    if let Some(playlist) = payload.get("playlistV2") {
        return parse_pathfinder_playlist(playlist);
    }
    parse_web_api_playlist(payload)
}

// ---------------------------------------------------------------------------
// Artist metadata
// ---------------------------------------------------------------------------

/// An album entry inside artist discography listings.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ArtistAlbum {
    pub id: String,
    pub name: String,
    pub cover_url: Option<String>,
    pub release_date: Option<String>,
    pub release_year: Option<i32>,
    pub total_tracks: Option<u64>,
    pub album_type: Option<String>,
}

/// Filtered Spotify artist metadata.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SpotifyArtist {
    pub id: String,
    pub name: String,
    pub verified: bool,
    pub biography: Option<String>,
    pub avatar_url: Option<String>,
    pub header_url: Option<String>,
    pub followers: Option<u64>,
    pub monthly_listeners: Option<u64>,
    pub top_tracks: Vec<SpotifyTrack>,
    pub albums: Vec<ArtistAlbum>,
}

fn artist_album_from_release(release: &Value) -> Option<ArtistAlbum> {
    if release.is_null() {
        return None;
    }
    let id = spotify_id_of(release);
    if id.is_empty() {
        return None;
    }
    let release_date = pathfinder_release_date(release.get("date").unwrap_or(&Value::Null));
    let release_year = release_date.as_deref().and_then(release_year_from_date);
    let cover_url = release
        .get("coverArt")
        .and_then(extract_cover_images)
        .and_then(|c| c.medium.or(c.small).or(c.large));
    Some(ArtistAlbum {
        id,
        name: str_field(release, "name"),
        cover_url,
        release_date,
        release_year,
        total_tracks: release
            .get("tracks")
            .and_then(|t| uint_field(t, "totalCount")),
        album_type: opt_string(release, "type").filter(|s| !s.is_empty()),
    })
}

fn parse_pathfinder_artist(artist: &Value) -> Result<SpotifyArtist> {
    let id = spotify_id_of(artist);
    if id.is_empty() {
        return Err(ProviderError::ResolutionFailed(
            "pathfinder artist is missing an id".to_string(),
        ));
    }
    let profile = artist.get("profile").unwrap_or(&Value::Null);
    let biography = profile
        .get("biography")
        .and_then(|b| opt_string(b, "text"))
        .map(|text| html_unescape(&strip_html_tags(&text)).trim().to_string())
        .filter(|s| !s.is_empty());
    let visuals = artist.get("visuals").unwrap_or(&Value::Null);
    let avatar_url = visuals
        .get("avatarImage")
        .and_then(extract_cover_images)
        .and_then(|c| c.medium.or(c.small).or(c.large));
    let header_url = artist
        .pointer("/headerImage/data/sources")
        .and_then(Value::as_array)
        .and_then(|sources| sources.first())
        .and_then(|s| opt_string(s, "url"));
    let stats = artist.get("stats").unwrap_or(&Value::Null);
    let mut albums = Vec::new();
    if let Some(items) = artist
        .pointer("/discography/all/items")
        .and_then(Value::as_array)
    {
        for item in items {
            let release = item
                .get("releases")
                .and_then(|r| r.get("items"))
                .and_then(Value::as_array)
                .and_then(|list| list.first())
                .or_else(|| item.get("album"));
            if let Some(parsed) = release.and_then(artist_album_from_release) {
                albums.push(parsed);
            }
        }
    }
    // Some Pathfinder shapes embed top tracks directly.
    let mut top_tracks = Vec::new();
    if let Some(items) = artist
        .get("topTracks")
        .and_then(Value::as_array)
        .or_else(|| {
            artist
                .get("topTracksV2")
                .and_then(|t| t.get("items"))
                .and_then(Value::as_array)
        })
    {
        for item in items {
            let track_value = item.get("track").unwrap_or(item);
            if let Ok(track) =
                parse_web_api_track(track_value).or_else(|_| parse_pathfinder_track(track_value))
            {
                top_tracks.push(track);
            }
        }
    }
    Ok(SpotifyArtist {
        id,
        name: str_field(profile, "name"),
        verified: profile
            .get("verified")
            .and_then(Value::as_bool)
            .unwrap_or(false),
        biography,
        avatar_url,
        header_url,
        followers: stats
            .get("followers")
            .map(num_f64)
            .map(|n| n.max(0.0) as u64),
        monthly_listeners: stats
            .get("monthlyListeners")
            .map(num_f64)
            .map(|n| n.max(0.0) as u64),
        top_tracks,
        albums,
    })
}

fn parse_web_api_artist(artist: &Value) -> Result<SpotifyArtist> {
    let id = spotify_id_of(artist);
    if id.is_empty() {
        return Err(ProviderError::ResolutionFailed(
            "web api artist is missing an id".to_string(),
        ));
    }
    let name = artist
        .pointer("/profile/name")
        .and_then(Value::as_str)
        .or_else(|| artist.get("name").and_then(Value::as_str))
        .unwrap_or_default()
        .to_string();
    let avatar_url = artist
        .get("visualIdentity")
        .and_then(extract_cover_images)
        .and_then(|c| c.medium.or(c.small).or(c.large))
        .or_else(|| {
            artist
                .get("images")
                .and_then(cover_from_web_api_images)
                .and_then(|c| c.medium.or(c.small).or(c.large))
        })
        .or_else(|| {
            artist
                .pointer("/visuals/avatarImage")
                .and_then(extract_cover_images)
                .and_then(|c| c.medium.or(c.small).or(c.large))
        });
    Ok(SpotifyArtist {
        id,
        name,
        verified: false,
        biography: None,
        avatar_url,
        header_url: None,
        followers: artist.get("followers").and_then(|f| uint_field(f, "total")),
        monthly_listeners: None,
        top_tracks: Vec::new(),
        albums: Vec::new(),
    })
}

/// Parse artist metadata from a Pathfinder (`data.artistUnion`) or Web API payload.
pub fn parse_artist(payload: &Value) -> Result<SpotifyArtist> {
    if let Some(artist) = payload.pointer("/data/artistUnion") {
        if !artist.is_null() {
            return parse_pathfinder_artist(artist);
        }
    }
    if let Some(artist) = payload.get("artistUnion") {
        return parse_pathfinder_artist(artist);
    }
    parse_web_api_artist(payload)
}

/// Parse a Web API top-tracks payload (`{"tracks": [...]}`).
#[must_use]
pub fn parse_artist_top_tracks(payload: &Value) -> Vec<SpotifyTrack> {
    let items = payload
        .get("tracks")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    items
        .iter()
        .filter_map(|t| parse_web_api_track(t).ok())
        .collect()
}

// ---------------------------------------------------------------------------
// Tokens: TOTP, access token, client token
// ---------------------------------------------------------------------------

/// Parsed web-player access token response.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccessTokenData {
    pub access_token: String,
    pub client_id: String,
    pub expires_at: Option<SystemTime>,
}

/// Current Unix time in seconds.
fn unix_seconds_now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

/// Generate a 6-digit TOTP code (RFC 6238, SHA-1, 30s period) for a base32 secret.
pub fn generate_totp_at(secret_base32: &str, unix_seconds: u64) -> Result<String> {
    use hmac::Mac as _;
    let mut padded = secret_base32.trim().trim_end_matches('=').to_uppercase();
    if padded.len() % 8 == 1 {
        return Err(ProviderError::Crypto(
            "invalid totp secret length".to_string(),
        ));
    }
    while !padded.len().is_multiple_of(8) {
        padded.push('=');
    }
    let key = data_encoding::BASE32
        .decode(padded.as_bytes())
        .map_err(|e| ProviderError::Crypto(format!("invalid totp secret: {e}")))?;
    let counter = unix_seconds / 30;
    let mut mac = hmac::Hmac::<sha1::Sha1>::new_from_slice(&key)
        .map_err(|e| ProviderError::Crypto(format!("totp hmac init failed: {e}")))?;
    mac.update(&counter.to_be_bytes());
    let digest = mac.finalize().into_bytes();
    let offset = (digest[19] & 0x0f) as usize;
    let code = ((u32::from(digest[offset]) & 0x7f) << 24)
        | (u32::from(digest[offset + 1]) << 16)
        | (u32::from(digest[offset + 2]) << 8)
        | u32::from(digest[offset + 3]);
    Ok(format!("{:06}", code % 1_000_000))
}

/// Generate the Spotify web-player TOTP code and version for a Unix timestamp.
pub fn generate_spotify_totp_at(unix_seconds: u64) -> Result<(String, u32)> {
    Ok((
        generate_totp_at(SPOTIFY_TOTP_SECRET, unix_seconds)?,
        SPOTIFY_TOTP_VERSION,
    ))
}

/// Build the token request URL for a TOTP code and version.
#[must_use]
pub fn access_token_url(totp: &str, version: u32) -> String {
    format!(
        "{API_TOKEN_URL}?reason=init&productType=web-player&totp={totp}&totpVer={version}&totpServer={totp}"
    )
}

/// Convert an `accessTokenExpirationTimestampMs` value to a `SystemTime`.
#[must_use]
pub fn token_expiry_from_ms(expiry_ms: i64) -> Option<SystemTime> {
    if expiry_ms <= 0 {
        return None;
    }
    UNIX_EPOCH.checked_add(Duration::from_millis(expiry_ms as u64))
}

/// Check cached-token freshness with the 30s reuse skew (mirrors Go).
#[must_use]
pub fn is_token_fresh(expires_at: Option<SystemTime>) -> bool {
    match expires_at {
        Some(expiry) => SystemTime::now() + TOKEN_CACHE_SKEW < expiry,
        None => false,
    }
}

/// Parse an access-token JSON response (both `/api/token` and legacy shapes).
pub fn parse_access_token_response(value: &Value) -> Result<AccessTokenData> {
    let access_token = value
        .get("accessToken")
        .and_then(Value::as_str)
        .or_else(|| value.get("access_token").and_then(Value::as_str))
        .unwrap_or_default();
    let mut client_id = value
        .get("clientId")
        .and_then(Value::as_str)
        .or_else(|| value.get("client_id").and_then(Value::as_str))
        .unwrap_or_default()
        .to_string();
    if access_token.is_empty() {
        return Err(ProviderError::Auth(
            "access token response did not include required fields".to_string(),
        ));
    }
    if client_id.is_empty() {
        client_id = SPOTIFY_WEB_CLIENT_ID.to_string();
    }
    let expires_at = value
        .get("accessTokenExpirationTimestampMs")
        .map(num_f64)
        .filter(|ms| *ms > 0.0)
        .and_then(|ms| token_expiry_from_ms(ms as i64))
        .or_else(|| {
            value
                .get("expires_in")
                .map(num_f64)
                .filter(|secs| *secs > 0.0)
                .and_then(|secs| SystemTime::now().checked_add(Duration::from_secs(secs as u64)))
        })
        .or_else(|| SystemTime::now().checked_add(DEFAULT_TOKEN_TTL));
    Ok(AccessTokenData {
        access_token: access_token.to_string(),
        client_id,
        expires_at,
    })
}

/// Build the `client-token` exchange payload.
#[must_use]
pub fn client_token_payload(client_id: &str, device_id: &str, client_version: &str) -> Value {
    serde_json::json!({
        "client_data": {
            "client_version": client_version,
            "client_id": client_id,
            "js_sdk_data": {
                "device_brand": "unknown",
                "device_model": "unknown",
                "os": "windows",
                "os_version": "NT 10.0",
                "device_id": device_id,
                "device_type": "computer",
            },
        },
    })
}

/// Parse a `client-token` JSON response into the granted token.
pub fn parse_client_token_response(value: &Value) -> Result<String> {
    if str_field(value, "response_type") != "RESPONSE_GRANTED_TOKEN_RESPONSE" {
        return Err(ProviderError::Auth(
            "invalid client token response type".to_string(),
        ));
    }
    let token = value
        .get("granted_token")
        .map(|t| str_field(t, "token"))
        .unwrap_or_default();
    if token.is_empty() {
        return Err(ProviderError::Auth(
            "client token response did not include a token".to_string(),
        ));
    }
    Ok(token)
}

/// Extract `clientVersion` from the web-player session HTML bootstrap config.
#[must_use]
pub fn parse_client_version_from_html(html: &str) -> Option<String> {
    let marker = "<script id=\"appServerConfig\" type=\"text/plain\">";
    let start = html.find(marker)? + marker.len();
    let rest = &html[start..];
    let end = rest.find("</script>")?;
    let decoded = data_encoding::BASE64
        .decode(rest[..end].trim().as_bytes())
        .ok()?;
    let value: Value = serde_json::from_slice(&decoded).ok()?;
    value
        .get("clientVersion")
        .and_then(Value::as_str)
        .map(str::to_string)
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

#[derive(Debug, Default)]
struct TokenState {
    access_token: String,
    client_id: String,
    client_token: String,
    device_id: String,
    client_version: String,
    expires_at: Option<SystemTime>,
    cookies: HashMap<String, String>,
}

impl TokenState {
    fn access_token_valid(&self) -> bool {
        !self.access_token.is_empty() && is_token_fresh(self.expires_at)
    }

    fn effective_client_id(&self) -> String {
        if self.client_id.trim().is_empty() {
            SPOTIFY_WEB_CLIENT_ID.to_string()
        } else {
            self.client_id.clone()
        }
    }
}

/// Spotify Web Player metadata client with cached tokens.
#[derive(Debug)]
pub struct SpotifyClient {
    http: reqwest::Client,
    state: tokio::sync::Mutex<TokenState>,
}

impl Default for SpotifyClient {
    fn default() -> Self {
        Self::new()
    }
}

impl SpotifyClient {
    /// Create a client with a default HTTP transport.
    #[must_use]
    pub fn new() -> Self {
        Self::with_client(reqwest::Client::new())
    }

    /// Create a client with a custom HTTP transport (useful for tests).
    #[must_use]
    pub fn with_client(http: reqwest::Client) -> Self {
        Self {
            http,
            state: tokio::sync::Mutex::new(TokenState::default()),
        }
    }

    /// Currently cached access token, if any.
    pub async fn cached_access_token(&self) -> Option<String> {
        let state = self.state.lock().await;
        if state.access_token.is_empty() {
            None
        } else {
            Some(state.access_token.clone())
        }
    }

    /// Currently cached client token, if any.
    pub async fn cached_client_token(&self) -> Option<String> {
        let state = self.state.lock().await;
        if state.client_token.is_empty() {
            None
        } else {
            Some(state.client_token.clone())
        }
    }

    /// Store an access-token response in the cache.
    pub async fn store_access_token(&self, data: &AccessTokenData) {
        let mut state = self.state.lock().await;
        state.access_token = data.access_token.clone();
        state.client_id = data.client_id.clone();
        state.expires_at = data.expires_at;
    }

    /// Ensure a fresh web-player access token, using the shared cache window.
    pub async fn ensure_access_token(&self) -> Result<String> {
        {
            let state = self.state.lock().await;
            if state.access_token_valid() {
                return Ok(state.access_token.clone());
            }
        }
        let now = unix_seconds_now();
        let mut last_error: Option<ProviderError> = None;
        for offset in [0_i64, -30, 30] {
            let moment = if offset < 0 {
                now.saturating_sub(offset.unsigned_abs())
            } else {
                now.saturating_add(offset as u64)
            };
            let (totp, version) = match generate_spotify_totp_at(moment) {
                Ok(pair) => pair,
                Err(e) => {
                    last_error = Some(e);
                    continue;
                }
            };
            match self.request_access_token(&totp, version).await {
                Ok(data) => {
                    let mut state = self.state.lock().await;
                    state.access_token = data.access_token.clone();
                    state.client_id = data.client_id.clone();
                    state.expires_at = data.expires_at;
                    return Ok(data.access_token);
                }
                Err(e) => {
                    last_error = Some(e);
                    tokio::time::sleep(Duration::from_millis(400)).await;
                }
            }
        }
        Err(last_error
            .unwrap_or_else(|| ProviderError::Auth("access token request failed".to_string())))
    }

    async fn request_access_token(&self, totp: &str, version: u32) -> Result<AccessTokenData> {
        let url = access_token_url(totp, version);
        let response = self
            .http
            .get(&url)
            .header(reqwest::header::USER_AGENT, USER_AGENT)
            .header(
                reqwest::header::CONTENT_TYPE,
                "application/json;charset=UTF-8",
            )
            .send()
            .await?;
        let status = response.status();
        let cookies = extract_cookies(&response);
        let body = response.text().await.unwrap_or_default();
        if !status.is_success() {
            let details: String = body.trim().chars().take(200).collect();
            let suffix = if details.is_empty() {
                String::new()
            } else {
                format!(" | {details}")
            };
            return Err(ProviderError::Auth(format!(
                "access token request failed: HTTP {status}{suffix}"
            )));
        }
        let value: Value = serde_json::from_str(&body)?;
        let data = parse_access_token_response(&value)?;
        if !cookies.is_empty() {
            let mut state = self.state.lock().await;
            for (name, cookie_value) in cookies {
                if name == "sp_t" {
                    state.device_id = cookie_value.clone();
                }
                state.cookies.insert(name, cookie_value);
            }
        }
        Ok(data)
    }

    /// Fetch the session page to bootstrap device ID and client version.
    pub async fn refresh_session_info(&self) -> Result<()> {
        let cookie_header = {
            let state = self.state.lock().await;
            state
                .cookies
                .iter()
                .map(|(k, v)| format!("{k}={v}"))
                .collect::<Vec<_>>()
                .join("; ")
        };
        let mut request = self
            .http
            .get(SESSION_URL)
            .header(reqwest::header::USER_AGENT, USER_AGENT);
        if !cookie_header.is_empty() {
            request = request.header(reqwest::header::COOKIE, cookie_header);
        }
        let response = request.send().await?;
        if !response.status().is_success() {
            return Err(ProviderError::Auth(format!(
                "session initialization failed: HTTP {}",
                response.status()
            )));
        }
        let cookies = extract_cookies(&response);
        let html = response.text().await.unwrap_or_default();
        let mut state = self.state.lock().await;
        if let Some(version) = parse_client_version_from_html(&html) {
            state.client_version = version;
        }
        for (name, value) in cookies {
            if name == "sp_t" {
                state.device_id = value.clone();
            }
            state.cookies.insert(name, value);
        }
        Ok(())
    }

    /// Ensure a granted client token, bootstrapping session info as needed.
    pub async fn ensure_client_token(&self) -> Result<String> {
        {
            let state = self.state.lock().await;
            if !state.client_token.is_empty() {
                return Ok(state.client_token.clone());
            }
        }
        let (_client_id, device_id, client_version) = {
            let state = self.state.lock().await;
            (
                state.effective_client_id(),
                state.device_id.clone(),
                state.client_version.clone(),
            )
        };
        if device_id.is_empty() || client_version.is_empty() {
            self.refresh_session_info().await?;
            self.ensure_access_token().await?;
        }
        let (client_id, device_id, client_version) = {
            let state = self.state.lock().await;
            (
                state.effective_client_id(),
                state.device_id.clone(),
                state.client_version.clone(),
            )
        };
        // `effective_client_id` falls back to the well-known web-player client
        // ID when the token response did not provide one.
        let payload = client_token_payload(&client_id, &device_id, &client_version);
        let response = self
            .http
            .post(CLIENT_TOKEN_URL)
            .header(reqwest::header::USER_AGENT, USER_AGENT)
            .header(reqwest::header::CONTENT_TYPE, "application/json")
            .header(reqwest::header::ACCEPT, "application/json")
            .json(&payload)
            .send()
            .await?;
        if !response.status().is_success() {
            return Err(ProviderError::Auth(format!(
                "client token request failed: HTTP {}",
                response.status()
            )));
        }
        let value: Value = response.json().await?;
        let token = parse_client_token_response(&value)?;
        {
            let mut state = self.state.lock().await;
            state.client_token = token.clone();
        }
        Ok(token)
    }

    /// Run a Pathfinder query with automatic token refresh on auth failures.
    pub async fn query_pathfinder(&self, payload: &Value) -> Result<Value> {
        let (access_token, client_token, client_version) = self.tokens().await?;
        match self
            .post_pathfinder(payload, &access_token, &client_token, &client_version)
            .await
        {
            Ok(value) => Ok(value),
            Err(ProviderError::Auth(_)) => {
                self.invalidate_tokens().await;
                let (access_token, client_token, client_version) = self.tokens().await?;
                self.post_pathfinder(payload, &access_token, &client_token, &client_version)
                    .await
            }
            Err(e) => Err(e),
        }
    }

    async fn tokens(&self) -> Result<(String, String, String)> {
        let access_token = self.ensure_access_token().await?;
        let client_token = self.ensure_client_token().await?;
        let client_version = {
            let state = self.state.lock().await;
            state.client_version.clone()
        };
        Ok((access_token, client_token, client_version))
    }

    async fn invalidate_tokens(&self) {
        let mut state = self.state.lock().await;
        state.access_token.clear();
        state.client_token.clear();
        state.expires_at = None;
    }

    async fn post_pathfinder(
        &self,
        payload: &Value,
        access_token: &str,
        client_token: &str,
        client_version: &str,
    ) -> Result<Value> {
        let response = self
            .http
            .post(PATHFINDER_URL)
            .header(reqwest::header::USER_AGENT, USER_AGENT)
            .header(reqwest::header::CONTENT_TYPE, "application/json")
            .header(
                reqwest::header::AUTHORIZATION,
                format!("Bearer {access_token}"),
            )
            .header("Client-Token", client_token)
            .header("Spotify-App-Version", client_version)
            .json(payload)
            .send()
            .await?;
        let status = response.status();
        let body = response.text().await.unwrap_or_default();
        if status == reqwest::StatusCode::UNAUTHORIZED || status == reqwest::StatusCode::FORBIDDEN {
            return Err(ProviderError::Auth(format!(
                "pathfinder query unauthorized: HTTP {status}"
            )));
        }
        if !status.is_success() {
            let details: String = body.chars().take(200).collect();
            return Err(ProviderError::ResolutionFailed(format!(
                "API query failed: HTTP {status} | {details}"
            )));
        }
        Ok(serde_json::from_str(&body)?)
    }

    async fn web_api_get(&self, path: &str) -> Result<Value> {
        let token = self.ensure_access_token().await?;
        let url = format!("{WEB_API_BASE}{path}");
        let response = self
            .http
            .get(&url)
            .header(reqwest::header::USER_AGENT, USER_AGENT)
            .bearer_auth(token)
            .send()
            .await?;
        let status = response.status();
        let body = response.text().await.unwrap_or_default();
        if !status.is_success() {
            let details: String = body.chars().take(200).collect();
            return Err(ProviderError::ResolutionFailed(format!(
                "spotify web api request failed: HTTP {status} | {details}"
            )));
        }
        Ok(serde_json::from_str(&body)?)
    }

    /// Fetch track metadata by Spotify ID.
    pub async fn fetch_track(&self, id: &str) -> Result<SpotifyTrack> {
        let payload = self.web_api_get(&format!("/tracks/{id}")).await?;
        parse_track(&payload)
    }

    /// Fetch album metadata by Spotify ID.
    pub async fn fetch_album(&self, id: &str) -> Result<SpotifyAlbum> {
        let payload = self.web_api_get(&format!("/albums/{id}")).await?;
        parse_album(&payload)
    }

    /// Fetch playlist metadata by Spotify ID.
    pub async fn fetch_playlist(&self, id: &str) -> Result<SpotifyPlaylist> {
        let payload = self.web_api_get(&format!("/playlists/{id}")).await?;
        parse_playlist(&payload)
    }

    /// Fetch artist metadata (profile plus top tracks) by Spotify ID.
    pub async fn fetch_artist(&self, id: &str) -> Result<SpotifyArtist> {
        let payload = self.web_api_get(&format!("/artists/{id}")).await?;
        let mut artist = parse_artist(&payload)?;
        if let Ok(top) = self
            .web_api_get(&format!("/artists/{id}/top-tracks?market=US"))
            .await
        {
            artist.top_tracks = parse_artist_top_tracks(&top);
        }
        Ok(artist)
    }

    /// Fetch metadata for any parsed entity reference.
    pub async fn fetch_by_url(&self, url_or_uri: &str) -> Result<SpotifyEntity> {
        let reference = parse_spotify_ref(url_or_uri).ok_or_else(|| {
            ProviderError::ResolutionFailed(format!("not a spotify url: {url_or_uri}"))
        })?;
        match reference.entity {
            SpotifyEntityType::Track => Ok(SpotifyEntity::Track(Box::new(
                self.fetch_track(&reference.id).await?,
            ))),
            SpotifyEntityType::Album => Ok(SpotifyEntity::Album(Box::new(
                self.fetch_album(&reference.id).await?,
            ))),
            SpotifyEntityType::Playlist => Ok(SpotifyEntity::Playlist(Box::new(
                self.fetch_playlist(&reference.id).await?,
            ))),
            SpotifyEntityType::Artist => Ok(SpotifyEntity::Artist(Box::new(
                self.fetch_artist(&reference.id).await?,
            ))),
        }
    }
}

/// Any fetched Spotify entity (variants are boxed to keep the enum small).
#[derive(Debug, Clone)]
pub enum SpotifyEntity {
    Track(Box<SpotifyTrack>),
    Album(Box<SpotifyAlbum>),
    Playlist(Box<SpotifyPlaylist>),
    Artist(Box<SpotifyArtist>),
}

fn extract_cookies(response: &reqwest::Response) -> Vec<(String, String)> {
    response
        .headers()
        .get_all(reqwest::header::SET_COOKIE)
        .iter()
        .filter_map(|value| value.to_str().ok())
        .filter_map(|raw| {
            let pair = raw.split(';').next()?.trim();
            let (name, cookie_value) = pair.split_once('=')?;
            let name = name.trim();
            let cookie_value = cookie_value.trim();
            if name.is_empty() || cookie_value.is_empty() {
                return None;
            }
            Some((name.to_string(), cookie_value.to_string()))
        })
        .collect()
}

// ---------------------------------------------------------------------------
// Tests (offline and hermetic)
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    const TRACK_ID: &str = "4uLU6hMCjMI75M1A2tKUQp";
    const ARTIST_ID: &str = "1Xyo4u8uXC1ZmMpatF05PJ";
    const ALBUM_ID: &str = "1A2GTg8t5ViNmzgdSpJ6bA";
    const PLAYLIST_ID: &str = "37i9dQZF1DXcBWIGoYBM5M";

    fn pathfinder_track_payload() -> Value {
        json!({
            "data": {
                "trackUnion": {
                    "id": TRACK_ID,
                    "uri": format!("spotify:track:{TRACK_ID}"),
                    "name": "Never Gonna Give You Up",
                    "trackNumber": 1,
                    "discNumber": 1,
                    "playcount": "1500000000",
                    "duration": { "totalMilliseconds": 213_000 },
                    "contentRating": { "label": "NONE" },
                    "artists": {
                        "items": [
                            { "uri": format!("spotify:artist:{ARTIST_ID}"), "profile": { "name": "Rick Astley" } }
                        ]
                    },
                    "albumOfTrack": {
                        "id": ALBUM_ID,
                        "uri": format!("spotify:album:{ALBUM_ID}"),
                        "name": "Whenever You Need Somebody",
                        "date": { "isoString": "1987-11-16T00:00:00Z" },
                        "tracks": { "totalCount": 10 },
                        "artists": {
                            "items": [
                                { "uri": format!("spotify:artist:{ARTIST_ID}"), "profile": { "name": "Rick Astley" } }
                            ]
                        },
                        "coverArt": {
                            "sources": [
                                { "url": "https://i.scdn.co/image/ab67616d0000b273111111111111111111111111", "width": 300, "height": 300 },
                                { "url": "https://i.scdn.co/image/ab67616d0000b273222222222222222222222222", "width": 640, "height": 640 },
                                { "url": "https://i.scdn.co/image/small", "width": 64, "height": 64 }
                            ]
                        }
                    }
                }
            }
        })
    }

    fn web_api_track_payload() -> Value {
        json!({
            "id": TRACK_ID,
            "uri": format!("spotify:track:{TRACK_ID}"),
            "name": "Blinding Lights",
            "duration_ms": 202_040,
            "explicit": true,
            "track_number": 9,
            "disc_number": 1,
            "external_ids": { "isrc": "USUG11904206" },
            "artists": [
                { "id": "1Xyo4u8uXC1ZmMpatF05PJ", "name": "The Weeknd" }
            ],
            "album": {
                "id": ALBUM_ID,
                "name": "After Hours",
                "release_date": "2020-03-20",
                "total_tracks": 14,
                "images": [
                    { "url": "https://i.scdn.co/image/large", "width": 640, "height": 640 },
                    { "url": "https://i.scdn.co/image/medium", "width": 300, "height": 300 },
                    { "url": "https://i.scdn.co/image/tiny", "width": 64, "height": 64 }
                ],
                "artists": [
                    { "id": "1Xyo4u8uXC1ZmMpatF05PJ", "name": "The Weeknd" }
                ]
            }
        })
    }

    fn pathfinder_album_payload() -> Value {
        json!({
            "data": {
                "albumUnion": {
                    "uri": format!("spotify:album:{ALBUM_ID}"),
                    "name": "After Hours",
                    "label": "XO Records",
                    "date": { "isoString": "2020-03-20T00:00:00Z" },
                    "discs": { "totalCount": 1 },
                    "artists": {
                        "items": [
                            { "uri": "spotify:artist:1Xyo4u8uXC1ZmMpatF05PJ", "profile": { "name": "The Weeknd" } }
                        ]
                    },
                    "coverArt": {
                        "sources": [
                            { "url": "https://i.scdn.co/image/ab67616d0000b273aaa", "width": 300, "height": 300 },
                            { "url": "https://i.scdn.co/image/ab67616d0000b273bbb", "width": 640, "height": 640 }
                        ]
                    },
                    "tracksV2": {
                        "items": [
                            { "track": {
                                "uri": "spotify:track:track00000000000000001",
                                "name": "Alone Again",
                                "trackNumber": 1,
                                "discNumber": 1,
                                "duration": { "totalMilliseconds": 250_000 },
                                "playcount": "100",
                                "contentRating": { "label": "EXPLICIT" },
                                "artists": { "items": [ { "uri": "spotify:artist:1Xyo4u8uXC1ZmMpatF05PJ", "profile": { "name": "The Weeknd" } } ] }
                            } },
                            { "track": {
                                "uri": "spotify:track:track00000000000000002",
                                "name": "Too Late",
                                "trackNumber": 2,
                                "discNumber": 1,
                                "duration": { "totalMilliseconds": 240_000 },
                                "playcount": "200",
                                "contentRating": { "label": "NONE" },
                                "artists": { "items": [ { "uri": "spotify:artist:1Xyo4u8uXC1ZmMpatF05PJ", "profile": { "name": "The Weeknd" } } ] }
                            } }
                        ]
                    }
                }
            }
        })
    }

    fn pathfinder_playlist_payload() -> Value {
        json!({
            "data": {
                "playlistV2": {
                    "uri": format!("spotify:playlist:{PLAYLIST_ID}"),
                    "name": "Test Playlist",
                    "description": "Hits &amp; classics",
                    "followers": { "totalCount": 42 },
                    "ownerV2": { "data": { "name": "spotify", "avatar": { "sources": [ { "url": "https://i.scdn.co/avatar" } ] } } },
                    "images": { "items": [ { "sources": [ { "url": "https://i.scdn.co/image/playlist" } ] } ] },
                    "content": {
                        "totalCount": 1,
                        "items": [
                            {
                                "attributes": [ { "key": "rank", "value": "999" } ],
                                "itemV2": { "data": {
                                    "uri": format!("spotify:track:{TRACK_ID}"),
                                    "id": TRACK_ID,
                                    "name": "Blinding Lights",
                                    "discNumber": 1,
                                    "trackDuration": { "totalMilliseconds": 202_040 },
                                    "contentRating": { "label": "NONE" },
                                    "artists": { "items": [ { "uri": "spotify:artist:1Xyo4u8uXC1ZmMpatF05PJ", "profile": { "name": "The Weeknd" } } ] },
                                    "albumOfTrack": {
                                        "uri": format!("spotify:album:{ALBUM_ID}"),
                                        "name": "After Hours",
                                        "coverArt": { "sources": [ { "url": "https://i.scdn.co/image/ab67616d0000b273ccc", "width": 300, "height": 300 } ] },
                                        "artists": { "items": [ { "uri": "spotify:artist:1Xyo4u8uXC1ZmMpatF05PJ", "profile": { "name": "The Weeknd" } } ] }
                                    }
                                } }
                            }
                        ]
                    }
                }
            }
        })
    }

    fn pathfinder_artist_payload() -> Value {
        json!({
            "data": {
                "artistUnion": {
                    "uri": format!("spotify:artist:{ARTIST_ID}"),
                    "profile": { "name": "The Weeknd", "verified": true },
                    "stats": { "followers": 10_000, "monthlyListeners": 5_000 },
                    "visuals": { "avatarImage": { "sources": [ { "url": "https://i.scdn.co/image/ab67616d0000b273ddd", "width": 640, "height": 640 } ] } },
                    "discography": { "all": { "totalCount": 1, "items": [ { "releases": { "items": [ {
                        "uri": format!("spotify:album:{ALBUM_ID}"),
                        "name": "After Hours",
                        "type": "ALBUM",
                        "date": { "isoString": "2020-03-20T00:00:00Z" },
                        "tracks": { "totalCount": 14 },
                        "coverArt": { "sources": [ { "url": "https://i.scdn.co/image/ab67616d0000b273eee", "width": 640, "height": 640 } ] }
                    } ] } } ] } }
                }
            }
        })
    }

    #[test]
    fn parses_open_urls_for_all_entity_kinds() {
        let cases = [
            (
                "https://open.spotify.com/track/4uLU6hMCjMI75M1A2tKUQp?si=abc",
                SpotifyEntityType::Track,
                "4uLU6hMCjMI75M1A2tKUQp",
            ),
            (
                "https://open.spotify.com/album/1A2GTg8t5ViNmzgdSpJ6bA",
                SpotifyEntityType::Album,
                "1A2GTg8t5ViNmzgdSpJ6bA",
            ),
            (
                "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M#frag",
                SpotifyEntityType::Playlist,
                "37i9dQZF1DXcBWIGoYBM5M",
            ),
            (
                "https://open.spotify.com/artist/1Xyo4u8uXC1ZmMpatF05PJ/",
                SpotifyEntityType::Artist,
                "1Xyo4u8uXC1ZmMpatF05PJ",
            ),
            (
                "https://open.spotify.com/intl-de/track/4uLU6hMCjMI75M1A2tKUQp",
                SpotifyEntityType::Track,
                "4uLU6hMCjMI75M1A2tKUQp",
            ),
            (
                "https://open.spotify.com/embed/track/4uLU6hMCjMI75M1A2tKUQp",
                SpotifyEntityType::Track,
                "4uLU6hMCjMI75M1A2tKUQp",
            ),
        ];
        for (input, entity, id) in cases {
            let parsed = parse_spotify_ref(input).expect("url should parse");
            assert_eq!(parsed.entity, entity);
            assert_eq!(parsed.id, id);
        }
    }

    #[test]
    fn parses_spotify_uris() {
        let parsed = parse_spotify_ref("spotify:track:4uLU6hMCjMI75M1A2tKUQp").expect("uri parses");
        assert_eq!(parsed.entity, SpotifyEntityType::Track);
        assert_eq!(parsed.id, "4uLU6hMCjMI75M1A2tKUQp");

        let parsed =
            parse_spotify_ref("spotify:artist:1Xyo4u8uXC1ZmMpatF05PJ").expect("uri parses");
        assert_eq!(parsed.entity, SpotifyEntityType::Artist);
    }

    #[test]
    fn rejects_invalid_references() {
        for input in [
            "",
            "not a url",
            "https://example.com/track/abc123",
            "https://open.spotify.com/episode/abc123",
            "spotify:track:",
            "spotify:track:abc:extra",
            "spotify:show:abc123",
            "spotify:track:not-valid!!",
            "https://open.spotify.com/track/",
        ] {
            assert!(parse_spotify_ref(input).is_none(), "should reject {input}");
        }
    }

    #[test]
    fn canonical_url_round_trips() {
        let reference = parse_spotify_ref("spotify:album:1A2GTg8t5ViNmzgdSpJ6bA").expect("parses");
        assert_eq!(
            canonical_open_url(reference.entity, &reference.id),
            "https://open.spotify.com/album/1A2GTg8t5ViNmzgdSpJ6bA"
        );
    }

    #[test]
    fn formats_durations() {
        assert_eq!(format_duration(0), "0:00");
        assert_eq!(format_duration(61_000), "1:01");
        assert_eq!(format_duration(202_040), "3:22");
        assert_eq!(format_duration(3_600_000), "60:00");
    }

    #[test]
    fn extracts_cover_resolutions() {
        let container = json!({
            "sources": [
                { "url": "https://i.scdn.co/image/ab67616d0000b273aaa", "width": 300, "height": 300 },
                { "url": "https://i.scdn.co/image/ab67616d0000b273bbb", "width": 640, "height": 640 },
                { "url": "https://i.scdn.co/image/tiny", "width": 64, "height": 64 }
            ]
        });
        let covers = extract_cover_images(&container).expect("covers");
        assert_eq!(
            covers.small.as_deref(),
            Some("https://i.scdn.co/image/ab67616d0000b273aaa")
        );
        assert_eq!(
            covers.medium.as_deref(),
            Some("https://i.scdn.co/image/ab67616d0000b273bbb")
        );
        assert_eq!(
            covers.large.as_deref(),
            Some("https://i.scdn.co/image/ab67616d000082c1aaa")
        );
        assert_eq!(covers.highest(), covers.large.as_deref());
    }

    #[test]
    fn cover_extraction_handles_square_nesting_and_fallback() {
        let nested = json!({
            "squareCoverImage": {
                "image": { "data": { "sources": [ { "url": "https://cdn/cover", "width": 0, "height": 0 } ] } }
            }
        });
        let covers = extract_cover_images(&nested).expect("fallback covers");
        assert_eq!(covers.highest(), Some("https://cdn/cover"));
        assert!(extract_cover_images(&json!({})).is_none());
        assert!(extract_cover_images(&json!({ "sources": [] })).is_none());
    }

    #[test]
    fn web_api_images_pick_highest_resolution() {
        let images = json!([
            { "url": "https://cdn/small", "width": 300, "height": 300 },
            { "url": "https://cdn/large", "width": 640, "height": 640 }
        ]);
        let covers = cover_from_web_api_images(&images).expect("covers");
        assert_eq!(covers.highest(), Some("https://cdn/large"));
    }

    #[test]
    fn parses_pathfinder_track() {
        let track = parse_track(&pathfinder_track_payload()).expect("track parses");
        assert_eq!(track.id, TRACK_ID);
        assert_eq!(track.title, "Never Gonna Give You Up");
        assert_eq!(join_artist_names(&track.artists, ", "), "Rick Astley");
        assert_eq!(track.album.name, "Whenever You Need Somebody");
        assert_eq!(track.album.release_year, Some(1987));
        assert_eq!(track.duration_ms, 213_000);
        assert_eq!(track.duration_display, "3:33");
        assert!(!track.explicit);
        assert_eq!(track.disc_number, 1);
        assert_eq!(track.play_count.as_deref(), Some("1500000000"));
        assert!(track.isrc.is_none());
        let cover = track.cover.expect("cover");
        assert!(cover.highest().is_some());
    }

    #[test]
    fn parses_web_api_track_with_isrc_and_cover() {
        let track = parse_track(&web_api_track_payload()).expect("track parses");
        assert_eq!(track.id, TRACK_ID);
        assert_eq!(track.title, "Blinding Lights");
        assert_eq!(track.isrc.as_deref(), Some("USUG11904206"));
        assert!(track.explicit);
        assert_eq!(track.duration_display, "3:22");
        assert_eq!(track.album.release_year, Some(2020));
        assert_eq!(track.album.total_tracks, Some(14));
        assert_eq!(
            track.cover.as_ref().and_then(|c| c.highest()),
            Some("https://i.scdn.co/image/large")
        );
    }

    #[test]
    fn parses_pathfinder_album() {
        let album = parse_album(&pathfinder_album_payload()).expect("album parses");
        assert_eq!(album.id, ALBUM_ID);
        assert_eq!(album.title, "After Hours");
        assert_eq!(join_artist_names(&album.artists, ", "), "The Weeknd");
        assert_eq!(album.release_year, Some(2020));
        assert_eq!(album.label.as_deref(), Some("XO Records"));
        assert_eq!(album.tracks.len(), 2);
        assert_eq!(album.tracks[0].title, "Alone Again");
        assert!(album.tracks[0].explicit);
        assert!(!album.tracks[1].explicit);
        assert!(album.cover.as_ref().and_then(|c| c.highest()).is_some());
    }

    #[test]
    fn parses_web_api_album() {
        let payload = json!({
            "id": ALBUM_ID,
            "name": "After Hours",
            "label": "XO",
            "release_date": "2020-03-20",
            "artists": [{ "id": ARTIST_ID, "name": "The Weeknd" }],
            "images": [{ "url": "https://cdn/cover", "width": 640, "height": 640 }],
            "tracks": { "items": [
                { "id": "track1", "name": "Alone Again", "duration_ms": 250_000, "explicit": false, "disc_number": 1, "track_number": 1,
                  "artists": [{ "id": ARTIST_ID, "name": "The Weeknd" }] }
            ] }
        });
        let album = parse_album(&payload).expect("album parses");
        assert_eq!(album.tracks.len(), 1);
        assert_eq!(album.release_year, Some(2020));
        assert_eq!(album.tracks[0].duration_display, "4:10");
    }

    #[test]
    fn parses_pathfinder_playlist() {
        let playlist = parse_playlist(&pathfinder_playlist_payload()).expect("playlist parses");
        assert_eq!(playlist.id, PLAYLIST_ID);
        assert_eq!(playlist.name, "Test Playlist");
        assert_eq!(playlist.description, "Hits & classics");
        assert_eq!(playlist.followers, 42);
        assert_eq!(playlist.total_tracks, 1);
        assert_eq!(
            playlist.owner.as_ref().map(|o| o.name.as_str()),
            Some("spotify")
        );
        assert_eq!(playlist.tracks.len(), 1);
        assert_eq!(playlist.tracks[0].title, "Blinding Lights");
        assert_eq!(playlist.tracks[0].play_count.as_deref(), Some("999"));
        assert_eq!(playlist.tracks[0].album, "After Hours");
    }

    #[test]
    fn parses_web_api_playlist_with_owner() {
        let payload = json!({
            "id": PLAYLIST_ID,
            "name": "Mixed",
            "description": "A &lt;mix&gt;",
            "followers": { "total": 7 },
            "owner": { "display_name": "owner-name", "id": "owner-id" },
            "images": [{ "url": "https://cdn/pl" }],
            "tracks": {
                "total": 1,
                "items": [
                    { "added_by": { "id": "friend-id" }, "track": {
                        "id": TRACK_ID,
                        "name": "Song",
                        "duration_ms": 180_000,
                        "explicit": false,
                        "artists": [{ "id": ARTIST_ID, "name": "The Weeknd" }],
                        "album": { "id": ALBUM_ID, "name": "After Hours", "images": [] }
                    } }
                ]
            }
        });
        let playlist = parse_playlist(&payload).expect("playlist parses");
        assert_eq!(playlist.description, "A <mix>");
        assert_eq!(playlist.tracks[0].added_by.as_deref(), Some("friend-id"));
        assert_eq!(
            playlist.owner.as_ref().map(|o| o.name.as_str()),
            Some("owner-name")
        );
    }

    #[test]
    fn parses_pathfinder_artist() {
        let artist = parse_artist(&pathfinder_artist_payload()).expect("artist parses");
        assert_eq!(artist.id, ARTIST_ID);
        assert_eq!(artist.name, "The Weeknd");
        assert!(artist.verified);
        assert_eq!(artist.followers, Some(10_000));
        assert_eq!(artist.monthly_listeners, Some(5_000));
        assert_eq!(artist.albums.len(), 1);
        assert_eq!(artist.albums[0].name, "After Hours");
        assert_eq!(artist.albums[0].release_year, Some(2020));
        assert!(artist.avatar_url.is_some());
    }

    #[test]
    fn parses_web_api_artist_and_top_tracks() {
        let payload = json!({
            "id": ARTIST_ID,
            "name": "The Weeknd",
            "followers": { "total": 99 },
            "images": [{ "url": "https://cdn/avatar", "width": 640, "height": 640 }]
        });
        let artist = parse_artist(&payload).expect("artist parses");
        assert_eq!(artist.followers, Some(99));

        let top = json!({ "tracks": [web_api_track_payload()] });
        let tracks = parse_artist_top_tracks(&top);
        assert_eq!(tracks.len(), 1);
        assert_eq!(tracks[0].title, "Blinding Lights");
    }

    #[test]
    fn rejects_payloads_without_ids() {
        assert!(parse_track(&json!({})).is_err());
        assert!(parse_album(&json!({"data": {}})).is_err());
        assert!(parse_playlist(&json!({"data": {}})).is_err());
        assert!(parse_artist(&json!({"data": {}})).is_err());
    }

    #[test]
    fn totp_matches_rfc6238_vector_and_is_stable() {
        // RFC 6238 SHA-1 secret "12345678901234567890", T=59 -> 94287082.
        let code = generate_totp_at("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", 59).expect("totp");
        assert_eq!(code, "287082");
        let (a, version) = generate_spotify_totp_at(1_700_000_000).expect("spotify totp");
        let (b, _) = generate_spotify_totp_at(1_700_000_000).expect("spotify totp");
        assert_eq!(version, SPOTIFY_TOTP_VERSION);
        assert_eq!(a, b);
        assert_eq!(a.len(), 6);
        assert!(a.bytes().all(|c| c.is_ascii_digit()));
        assert!(generate_totp_at("!!!not-base32!!!", 59).is_err());
    }

    #[test]
    fn access_token_url_carries_totp_params() {
        let url = access_token_url("123456", 61);
        assert!(url.starts_with(API_TOKEN_URL));
        assert!(url.contains("totp=123456"));
        assert!(url.contains("totpVer=61"));
    }

    #[test]
    fn access_token_response_parsing_and_freshness() {
        let future_ms = (unix_seconds_now() + 3600) * 1000;
        let value = json!({
            "accessToken": "tok",
            "clientId": "cid",
            "accessTokenExpirationTimestampMs": future_ms,
        });
        let data = parse_access_token_response(&value).expect("parses");
        assert_eq!(data.access_token, "tok");
        assert_eq!(data.client_id, "cid");
        assert!(is_token_fresh(data.expires_at));

        // Legacy snake_case shape falls back to the default client ID and TTL.
        let legacy = json!({ "access_token": "tok2" });
        let data = parse_access_token_response(&legacy).expect("parses");
        assert_eq!(data.client_id, SPOTIFY_WEB_CLIENT_ID);

        assert!(parse_access_token_response(&json!({})).is_err());
        assert!(!is_token_fresh(None));
        assert!(!is_token_fresh(Some(SystemTime::now())));
    }

    #[test]
    fn client_token_response_parsing() {
        let value = json!({
            "response_type": "RESPONSE_GRANTED_TOKEN_RESPONSE",
            "granted_token": { "token": "ct" },
        });
        assert_eq!(parse_client_token_response(&value).expect("parses"), "ct");
        assert!(parse_client_token_response(&json!({ "response_type": "NOPE" })).is_err());
        assert!(parse_client_token_response(&json!({
            "response_type": "RESPONSE_GRANTED_TOKEN_RESPONSE",
        }))
        .is_err());
    }

    #[test]
    fn client_version_parses_from_session_html() {
        let config = serde_json::json!({ "clientVersion": "1.2.3" }).to_string();
        let encoded = data_encoding::BASE64.encode(config.as_bytes());
        let html = format!(
            "<html><script id=\"appServerConfig\" type=\"text/plain\">{encoded}</script></html>"
        );
        assert_eq!(
            parse_client_version_from_html(&html).as_deref(),
            Some("1.2.3")
        );
        assert!(parse_client_version_from_html("<html></html>").is_none());
    }

    #[test]
    fn client_token_payload_shape() {
        let payload = client_token_payload("cid", "dev", "1.0");
        assert_eq!(
            payload
                .pointer("/client_data/client_id")
                .and_then(Value::as_str),
            Some("cid")
        );
        assert_eq!(
            payload
                .pointer("/client_data/js_sdk_data/device_id")
                .and_then(Value::as_str),
            Some("dev")
        );
    }

    #[tokio::test]
    async fn client_caches_tokens_offline() {
        let client = SpotifyClient::new();
        assert!(client.cached_access_token().await.is_none());
        let data = AccessTokenData {
            access_token: "tok".to_string(),
            client_id: "cid".to_string(),
            expires_at: Some(SystemTime::now() + Duration::from_secs(3600)),
        };
        client.store_access_token(&data).await;
        assert_eq!(client.cached_access_token().await.as_deref(), Some("tok"));
    }
}

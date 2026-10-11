//! Port of `backend/isrc_finder.go` and `backend/isrc_cache.go`.
//!
//! Spotify track ISRC and album UPC resolver, anonymous access token management,
//! base62 GID conversion, raw metadata fetching, and local ISRC caching via
//! `auralis_store::IsrcStore`.

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, RwLock};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use auralis_core::download_cancel::CancellationToken;
use serde::{de::DeserializeOwned, Deserialize, Serialize};

use crate::bounded_body::{read_bounded_response, MAX_PROVIDER_JSON_BYTES};
use crate::error::{ProviderError, Result};
use crate::songlink::{first_isrc_match, SONGLINK_USER_AGENT};

pub const SPOTIFY_SESSION_TOKEN_URL: &str = "https://open.spotify.com/api/token";
pub const SPOTIFY_GID_METADATA_URL: &str =
    "https://spclient.wg.spotify.com/metadata/4/{}/{}?market=from_token";
pub const SPOTIFY_BASE62_ALPHABET: &str =
    "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ";
pub const SPOTIFY_TOKEN_CACHE_FILE: &str = ".isrc-finder-token.json";

static OVERRIDE_SESSION_TOKEN_URL: RwLock<Option<String>> = RwLock::new(None);
static OVERRIDE_GID_METADATA_URL: RwLock<Option<String>> = RwLock::new(None);
static OVERRIDE_TOKEN_CACHE_PATH: RwLock<Option<PathBuf>> = RwLock::new(None);

pub fn set_spotify_session_token_url_for_test(url: Option<String>) {
    if let Ok(mut lock) = OVERRIDE_SESSION_TOKEN_URL.write() {
        *lock = url;
    }
}

pub fn set_spotify_gid_metadata_url_for_test(url: Option<String>) {
    if let Ok(mut lock) = OVERRIDE_GID_METADATA_URL.write() {
        *lock = url;
    }
}

pub fn set_spotify_token_cache_path_for_test(path: Option<PathBuf>) {
    if let Ok(mut lock) = OVERRIDE_TOKEN_CACHE_PATH.write() {
        *lock = path;
    }
}

fn get_session_token_url() -> String {
    OVERRIDE_SESSION_TOKEN_URL
        .read()
        .ok()
        .and_then(|lock| lock.clone())
        .unwrap_or_else(|| SPOTIFY_SESSION_TOKEN_URL.to_string())
}

fn get_gid_metadata_url(entity_type: &str, gid: &str) -> String {
    if let Some(base) = OVERRIDE_GID_METADATA_URL
        .read()
        .ok()
        .and_then(|lock| lock.clone())
    {
        return format!("{base}/{entity_type}/{gid}?market=from_token");
    }
    format!("https://spclient.wg.spotify.com/metadata/4/{entity_type}/{gid}?market=from_token")
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct SpotifyTrackIdentifiers {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub isrc: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub upc: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct SpotifyAnonymousToken {
    #[serde(rename = "accessToken")]
    pub access_token: String,
    #[serde(rename = "accessTokenExpirationTimestampMs")]
    pub access_token_expiration_timestamp_ms: i64,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct SpotifyTrackRawData {
    #[serde(default)]
    pub album: SpotifyTrackRawAlbum,
    #[serde(default, rename = "external_id")]
    pub external_id: Vec<SpotifyExternalId>,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct SpotifyTrackRawAlbum {
    #[serde(default)]
    pub gid: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct SpotifyExternalId {
    #[serde(default, rename = "type")]
    pub id_type: String,
    #[serde(default)]
    pub id: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct SpotifyAlbumRawData {
    #[serde(default, rename = "external_id")]
    pub external_id: Vec<SpotifyExternalId>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SpotifyHttpStatusError {
    pub status_code: u16,
    pub details: String,
}

impl std::fmt::Display for SpotifyHttpStatusError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "request failed: {}", self.details)
    }
}

impl std::error::Error for SpotifyHttpStatusError {}

impl From<SpotifyHttpStatusError> for ProviderError {
    fn from(err: SpotifyHttpStatusError) -> Self {
        ProviderError::Other(err.to_string())
    }
}

pub fn is_spotify_auth_failure(err: &(dyn std::error::Error + 'static)) -> bool {
    if let Some(status_err) = err.downcast_ref::<SpotifyHttpStatusError>() {
        status_err.status_code == 400
            || status_err.status_code == 401
            || status_err.status_code == 403
    } else {
        false
    }
}

fn is_auth_failure_status(code: u16) -> bool {
    code == 400 || code == 401 || code == 403
}

// Arbitrary-length big uint for base62 to hex conversion matching Go's big.Int
struct BigUint(Vec<u32>);

impl BigUint {
    fn new() -> Self {
        Self(vec![0])
    }

    fn mul_add(&mut self, mul: u32, add: u32) {
        let mut carry = add as u64;
        for digit in self.0.iter_mut() {
            let prod = (*digit as u64) * (mul as u64) + carry;
            *digit = (prod & 0xFFFF_FFFF) as u32;
            carry = prod >> 32;
        }
        while carry > 0 {
            self.0.push((carry & 0xFFFF_FFFF) as u32);
            carry >>= 32;
        }
    }

    fn to_hex(&self) -> String {
        let mut i = self.0.len();
        while i > 1 && self.0[i - 1] == 0 {
            i -= 1;
        }
        let mut s = format!("{:x}", self.0[i - 1]);
        for &digit in self.0[..i - 1].iter().rev() {
            s.push_str(&format!("{:08x}", digit));
        }
        s
    }
}

/// Converts a base62 Spotify entity ID to a 32-character lowercase hex GID (Go's `spotifyEntityIDToGID`).
pub fn spotify_entity_id_to_gid(entity_id: &str) -> Result<String> {
    if entity_id.is_empty() {
        return Err(ProviderError::Other("entity ID is empty".to_string()));
    }

    let mut val = BigUint::new();

    for char in entity_id.chars() {
        let index = SPOTIFY_BASE62_ALPHABET.find(char);
        let Some(idx) = index else {
            return Err(ProviderError::Other(format!(
                "invalid base62 character: {:?}",
                char.to_string()
            )));
        };
        val.mul_add(62, idx as u32);
    }

    let mut hex_value = val.to_hex();
    if hex_value.len() < 32 {
        hex_value = format!("{:0>32}", hex_value);
    }

    Ok(hex_value)
}

/// Converts a Spotify track ID to a 32-character lowercase hex GID (Go's `spotifyTrackIDToGID`).
pub fn spotify_track_id_to_gid(track_id: &str) -> Result<String> {
    spotify_entity_id_to_gid(track_id)
}

/// Extracts a 22-character Spotify track ID from an ID, URI, or URL (Go's `extractSpotifyTrackID`).
pub fn extract_spotify_track_id(value: &str) -> Result<String> {
    let value = value.trim();
    if value.is_empty() {
        return Err(ProviderError::Other("track input is required".to_string()));
    }

    if value.starts_with("spotify:track:") {
        if let Some(last_colon) = value.rfind(':') {
            return Ok(value[last_colon + 1..].to_string());
        }
    }

    if let Ok(parsed) = url::Url::parse(value) {
        if parsed.scheme() == "http" || parsed.scheme() == "https" {
            let path_clean = parsed.path().trim_matches('/');
            let parts: Vec<&str> = path_clean.split('/').collect();
            if parts.len() >= 2 && parts[0] == "track" {
                return Ok(parts[1].to_string());
            }
            return Err(ProviderError::Other(
                "expected URL like https://open.spotify.com/track/<id>".to_string(),
            ));
        }
    }

    if value.len() == 22 {
        return Ok(value.to_string());
    }

    Err(ProviderError::Other(
        "track must be a Spotify track ID, URL, or URI".to_string(),
    ))
}

/// Resolves path to the token cache file (Go's `spotifyTokenCachePath`).
pub fn spotify_token_cache_path() -> Result<PathBuf> {
    if let Ok(lock) = OVERRIDE_TOKEN_CACHE_PATH.read() {
        if let Some(ref path) = *lock {
            return Ok(path.clone());
        }
    }
    let app_dir = auralis_config::paths::ensure_app_dir()?;
    Ok(app_dir.join(SPOTIFY_TOKEN_CACHE_FILE))
}

/// Checks whether a cached Spotify token is still valid (Go's `spotifyTokenIsValid`).
pub fn spotify_token_is_valid(token: Option<&SpotifyAnonymousToken>) -> bool {
    let Some(t) = token else {
        return false;
    };
    if t.access_token.is_empty() || t.access_token_expiration_timestamp_ms == 0 {
        return false;
    }

    let now_ms = match SystemTime::now().duration_since(UNIX_EPOCH) {
        Ok(d) => d.as_millis() as i64,
        Err(_) => return false,
    };

    now_ms < t.access_token_expiration_timestamp_ms - 30_000
}

/// Loads cached Spotify token from disk (Go's `loadSpotifyCachedToken`).
pub fn load_spotify_cached_token() -> Result<Option<SpotifyAnonymousToken>> {
    let cache_path = spotify_token_cache_path()?;
    match std::fs::read(&cache_path) {
        Ok(body) => match serde_json::from_slice::<SpotifyAnonymousToken>(&body) {
            Ok(token) => Ok(Some(token)),
            Err(err) => Err(ProviderError::Other(format!(
                "failed to read token cache: {err}"
            ))),
        },
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(err) => Err(ProviderError::Other(format!(
            "failed to read token cache: {err}"
        ))),
    }
}

/// Saves Spotify token to disk (Go's `saveSpotifyCachedToken`).
pub fn save_spotify_cached_token(token: &SpotifyAnonymousToken) -> Result<()> {
    let cache_path = spotify_token_cache_path()?;
    if let Some(parent) = cache_path.parent() {
        std::fs::create_dir_all(parent).map_err(|e| {
            ProviderError::Other(format!("failed to create token cache directory: {e}"))
        })?;
    }

    let body = auralis_core::gojson::to_vec_pretty(token)?;
    std::fs::write(&cache_path, body)
        .map_err(|e| ProviderError::Other(format!("failed to write token cache: {e}")))?;
    Ok(())
}

static SPOTIFY_ANONYMOUS_TOKEN_MU: tokio::sync::Mutex<()> = tokio::sync::Mutex::const_new(());

/// Requests an anonymous Spotify web player access token (Go's `requestSpotifyAnonymousAccessToken`).
pub async fn request_spotify_anonymous_access_token(
    client: &reqwest::Client,
    force_refresh: bool,
) -> Result<String> {
    let _guard = SPOTIFY_ANONYMOUS_TOKEN_MU.lock().await;

    if !force_refresh {
        if let Ok(Some(cached_token)) = load_spotify_cached_token() {
            if spotify_token_is_valid(Some(&cached_token)) {
                return Ok(cached_token.access_token);
            }
        }
    }

    let (generated_totp, version) = crate::spotify_totp::generate_spotify_totp_now()
        .map_err(|e| ProviderError::Other(format!("failed to generate Spotify TOTP: {e}")))?;

    // Keys sorted alphabetically matching Go's url.Values.Encode()
    let query = format!(
        "productType=web-player&reason=init&totp={}&totpServer={}&totpVer={}",
        generated_totp, generated_totp, version
    );

    let session_url = get_session_token_url();
    let target_url = format!("{session_url}?{query}");

    let token: SpotifyAnonymousToken = request_spotify_json(client, &target_url, None).await?;

    save_spotify_cached_token(&token)?;

    Ok(token.access_token)
}

/// Executes a GET request and reads the bounded body (Go's `requestSpotifyBytes`).
pub async fn request_spotify_bytes(
    client: &reqwest::Client,
    target_url: &str,
    headers: Option<&HashMap<String, String>>,
) -> std::result::Result<Vec<u8>, SpotifyHttpStatusError> {
    let mut builder = client.get(target_url);

    if let Some(h) = headers {
        for (key, val) in h {
            builder = builder.header(key, val);
        }
    }

    let resp = match builder.send().await {
        Ok(r) => r,
        Err(err) => {
            return Err(SpotifyHttpStatusError {
                status_code: 0,
                details: err.to_string(),
            });
        }
    };

    let status = resp.status();
    let body = match read_bounded_response(resp, MAX_PROVIDER_JSON_BYTES).await {
        Ok(b) => b,
        Err(err) => {
            return Err(SpotifyHttpStatusError {
                status_code: status.as_u16(),
                details: err.to_string(),
            });
        }
    };

    if !status.is_success() {
        let mut details = String::from_utf8_lossy(&body).trim().to_string();
        if details.is_empty() {
            details = status.to_string();
        }
        return Err(SpotifyHttpStatusError {
            status_code: status.as_u16(),
            details,
        });
    }

    Ok(body)
}

/// Executes a GET request and parses the JSON response (Go's `requestSpotifyJSON`).
pub async fn request_spotify_json<T: DeserializeOwned>(
    client: &reqwest::Client,
    target_url: &str,
    headers: Option<&HashMap<String, String>>,
) -> Result<T> {
    let body = request_spotify_bytes(client, target_url, headers)
        .await
        .map_err(ProviderError::from)?;

    serde_json::from_slice(&body)
        .map_err(|err| ProviderError::Other(format!("failed to parse JSON response: {err}")))
}

/// Fetches raw metadata by 32-character hex GID (Go's `fetchSpotifyRawMetadataByGID`).
pub async fn fetch_spotify_raw_metadata_by_gid(
    client: &reqwest::Client,
    entity_type: &str,
    gid: &str,
) -> Result<Vec<u8>> {
    if gid.is_empty() {
        return Err(ProviderError::Other("GID is empty".to_string()));
    }

    let access_token = request_spotify_anonymous_access_token(client, false).await?;

    let target_url = get_gid_metadata_url(entity_type, gid);
    let mut headers = HashMap::new();
    headers.insert(
        "authorization".to_string(),
        format!("Bearer {access_token}"),
    );
    headers.insert("accept".to_string(), "application/json".to_string());
    headers.insert("user-agent".to_string(), SONGLINK_USER_AGENT.to_string());

    match request_spotify_bytes(client, &target_url, Some(&headers)).await {
        Ok(body) => Ok(body),
        Err(err) => {
            if !is_auth_failure_status(err.status_code) {
                return Err(ProviderError::from(err));
            }

            let refreshed_token = request_spotify_anonymous_access_token(client, true).await?;
            headers.insert(
                "authorization".to_string(),
                format!("Bearer {refreshed_token}"),
            );

            request_spotify_bytes(client, &target_url, Some(&headers))
                .await
                .map_err(ProviderError::from)
        }
    }
}

/// Fetches raw track metadata from Spotify (Go's `fetchSpotifyTrackRawData`).
pub async fn fetch_spotify_track_raw_data(
    client: &reqwest::Client,
    spotify_track_id: &str,
) -> Result<Vec<u8>> {
    let gid = spotify_track_id_to_gid(spotify_track_id)?;
    fetch_spotify_raw_metadata_by_gid(client, "track", &gid).await
}

/// Fetches raw album metadata from Spotify (Go's `fetchSpotifyAlbumRawData`).
pub async fn fetch_spotify_album_raw_data(
    client: &reqwest::Client,
    spotify_album_id: &str,
) -> Result<Vec<u8>> {
    let gid = spotify_entity_id_to_gid(spotify_album_id)?;
    fetch_spotify_raw_metadata_by_gid(client, "album", &gid).await
}

/// Extracts track identifiers (ISRC and album UPC) from Spotify raw track payload (Go's `extractSpotifyTrackIdentifiers`).
pub async fn extract_spotify_track_identifiers(
    client: Option<&reqwest::Client>,
    payload: &[u8],
) -> Result<SpotifyTrackIdentifiers> {
    let track: SpotifyTrackRawData = serde_json::from_slice(payload).map_err(|err| {
        ProviderError::Other(format!("failed to decode Spotify track metadata: {err}"))
    })?;

    let mut identifiers = SpotifyTrackIdentifiers::default();

    for external_id in &track.external_id {
        if external_id.id_type.trim().eq_ignore_ascii_case("isrc") {
            let isrc = first_isrc_match(&external_id.id);
            if !isrc.is_empty() {
                identifiers.isrc = isrc;
                break;
            }
        }
    }

    if identifiers.isrc.is_empty() {
        let payload_str = String::from_utf8_lossy(payload);
        identifiers.isrc = first_isrc_match(&payload_str);
    }

    let album_gid = track.album.gid.trim();
    if let Some(c) = client {
        if !album_gid.is_empty() {
            if let Ok(album_payload) =
                fetch_spotify_raw_metadata_by_gid(c, "album", album_gid).await
            {
                if let Ok(upc) = extract_spotify_album_upc(&album_payload) {
                    identifiers.upc = upc;
                }
            }
        }
    }

    Ok(identifiers)
}

/// Extracts ISRC from Spotify track metadata bytes (Go's `extractSpotifyTrackISRC`).
pub async fn extract_spotify_track_isrc(payload: &[u8]) -> Result<String> {
    let identifiers = extract_spotify_track_identifiers(None, payload).await?;
    if !identifiers.isrc.is_empty() {
        return Ok(identifiers.isrc);
    }
    Err(ProviderError::Other(
        "ISRC not found in Spotify track metadata".to_string(),
    ))
}

/// Extracts UPC from Spotify album metadata bytes (Go's `extractSpotifyAlbumUPC`).
pub fn extract_spotify_album_upc(payload: &[u8]) -> Result<String> {
    let album: SpotifyAlbumRawData = serde_json::from_slice(payload).map_err(|err| {
        ProviderError::Other(format!("failed to decode Spotify album metadata: {err}"))
    })?;

    for external_id in &album.external_id {
        if external_id.id_type.trim().eq_ignore_ascii_case("upc") {
            let upc = external_id.id.trim();
            if !upc.is_empty() {
                return Ok(upc.to_string());
            }
        }
    }

    Err(ProviderError::Other(
        "UPC not found in Spotify album metadata".to_string(),
    ))
}

/// Looks up an album's UPC on Spotify (Go's `lookupSpotifyAlbumUPC`).
pub async fn lookup_spotify_album_upc(album_id: &str) -> Result<String> {
    let normalized = album_id.trim();
    if normalized.is_empty() {
        return Err(ProviderError::Other(
            "spotify album ID is required".to_string(),
        ));
    }

    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(30))
        .build()?;

    let payload = fetch_spotify_album_raw_data(&client, normalized).await?;
    extract_spotify_album_upc(&payload)
}

/// Merges incoming identifiers into target (Go's `mergeSpotifyTrackIdentifiers`).
pub fn merge_spotify_track_identifiers(
    target: &mut SpotifyTrackIdentifiers,
    incoming: SpotifyTrackIdentifiers,
) {
    if !incoming.isrc.is_empty() {
        target.isrc = incoming.isrc.trim().to_string();
    }
    if !incoming.upc.is_empty() {
        target.upc = incoming.upc.trim().to_string();
    }
}

/// Writes resolved ISRC into cache (Go's `cacheResolvedSpotifyTrackISRC`).
pub fn cache_resolved_spotify_track_isrc(track_id: &str, resolved_track_id: &str, isrc: &str) {
    if let Err(err) = put_cached_isrc(track_id, isrc) {
        println!("Warning: failed to write ISRC cache: {err}");
    }
    if !resolved_track_id.is_empty() && resolved_track_id != track_id {
        if let Err(err) = put_cached_isrc(resolved_track_id, isrc) {
            println!("Warning: failed to write ISRC cache for resolved track ID: {err}");
        }
    }
}

/// Resolves track identifiers for a Spotify track ID (Go's `GetSpotifyTrackIdentifiersDirect`).
pub async fn get_spotify_track_identifiers_direct(
    spotify_track_id: &str,
) -> Result<SpotifyTrackIdentifiers> {
    get_spotify_track_identifiers_with_context(spotify_track_id, None).await
}

/// Resolves track identifiers with optional cancellation (Go's `GetSpotifyTrackIdentifiersWithContext`).
pub async fn get_spotify_track_identifiers_with_context(
    spotify_track_id: &str,
    cancel_token: Option<&CancellationToken>,
) -> Result<SpotifyTrackIdentifiers> {
    if let Some(token) = cancel_token {
        if token.is_cancelled() {
            return Err(ProviderError::Cancelled(
                auralis_core::download_cancel::DownloadCancelled,
            ));
        }
    }
    auralis_core::download_cancel::check_download_cancelled()?;

    let normalized_track_id = extract_spotify_track_id(spotify_track_id)?;

    let mut identifiers = SpotifyTrackIdentifiers::default();

    match get_cached_isrc(&normalized_track_id) {
        Ok(cached) => {
            if !cached.is_empty() {
                println!("Found ISRC in cache: {cached}");
                identifiers.isrc = cached;
            }
        }
        Err(err) => {
            println!("Warning: failed to read ISRC cache: {err}");
        }
    }

    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(30))
        .build()?;

    let mut metadata_err: Option<ProviderError> = None;

    match fetch_spotify_track_raw_data(&client, &normalized_track_id).await {
        Ok(payload) => match extract_spotify_track_identifiers(Some(&client), &payload).await {
            Ok(metadata_identifiers) => {
                merge_spotify_track_identifiers(&mut identifiers, metadata_identifiers);
                if !identifiers.isrc.is_empty() {
                    println!(
                        "Found identifiers via Spotify metadata: isrc={} upc={}",
                        identifiers.isrc, identifiers.upc
                    );
                    cache_resolved_spotify_track_isrc(&normalized_track_id, "", &identifiers.isrc);
                }
            }
            Err(extract_err) => {
                metadata_err = Some(extract_err);
            }
        },
        Err(err) => {
            metadata_err = Some(err);
        }
    }

    if !identifiers.isrc.is_empty() || !identifiers.upc.is_empty() {
        return Ok(identifiers);
    }

    if let Some(err) = metadata_err {
        return Err(err);
    }

    Err(ProviderError::Other(format!(
        "no Spotify identifiers found for track {normalized_track_id}"
    )))
}

/// Looks up a track's ISRC on Spotify (Go's `(s *SongLinkClient) lookupSpotifyISRC`).
pub async fn lookup_spotify_isrc(spotify_track_id: &str) -> Result<String> {
    lookup_spotify_isrc_with_context(spotify_track_id, None).await
}

/// Looks up a track's ISRC with cancellation token.
pub async fn lookup_spotify_isrc_with_context(
    spotify_track_id: &str,
    cancel_token: Option<&CancellationToken>,
) -> Result<String> {
    let identifiers =
        get_spotify_track_identifiers_with_context(spotify_track_id, cancel_token).await?;
    if identifiers.isrc.is_empty() {
        return Err(ProviderError::Other(format!(
            "no Spotify ISRC found for track {}",
            spotify_track_id.trim()
        )));
    }
    Ok(identifiers.isrc)
}

// =========================================================================
// ISRC Cache: thin layer over auralis_store::IsrcStore (backend/isrc_cache.go)
// =========================================================================

static ISRC_STORE: Mutex<Option<Arc<auralis_store::IsrcStore>>> = Mutex::new(None);

/// Initializes the global ISRC store using default app directory (Go's `InitISRCCacheDB`).
pub fn init_isrc_cache_db() -> Result<()> {
    let mut lock = ISRC_STORE
        .lock()
        .map_err(|e| ProviderError::Other(format!("failed to lock ISRC store: {e}")))?;
    if lock.is_some() {
        return Ok(());
    }

    let db_path = default_isrc_cache_path()?;
    let store = auralis_store::IsrcStore::open(&db_path)
        .map_err(|e| ProviderError::Other(e.to_string()))?;
    *lock = Some(Arc::new(store));
    Ok(())
}

/// Go: `filepath.Join(EnsureAppDir(), isrcCacheDBFile)`. Unit tests never
/// touch the user's real app directory.
fn default_isrc_cache_path() -> Result<std::path::PathBuf> {
    #[cfg(test)]
    {
        Ok(std::env::temp_dir().join(format!(
            "auralis-test-isrc-cache-{}.redb",
            std::process::id()
        )))
    }
    #[cfg(not(test))]
    {
        let app_dir = auralis_config::paths::ensure_app_dir()?;
        Ok(app_dir.join(auralis_core::constants::ISRC_CACHE_REDB_FILE))
    }
}

/// Serializes tests that swap the process-wide ISRC store.
#[cfg(test)]
pub(crate) static ISRC_STORE_TEST_LOCK: Mutex<()> = Mutex::new(());

/// Initializes the global ISRC store at a specific file path (for testing).
pub fn init_isrc_cache_db_at(path: &Path) -> Result<()> {
    let mut lock = ISRC_STORE
        .lock()
        .map_err(|e| ProviderError::Other(format!("failed to lock ISRC store: {e}")))?;
    let store =
        auralis_store::IsrcStore::open(path).map_err(|e| ProviderError::Other(e.to_string()))?;
    *lock = Some(Arc::new(store));
    Ok(())
}

/// Closes the global ISRC store (Go's `CloseISRCCacheDB`).
pub fn close_isrc_cache_db() {
    if let Ok(mut lock) = ISRC_STORE.lock() {
        *lock = None;
    }
}

/// Runs an operation with the global ISRC store (Go's `withISRCCacheDB`).
pub fn with_isrc_cache_db<F, R>(operation: F) -> Result<R>
where
    F: FnOnce(&auralis_store::IsrcStore) -> Result<R>,
{
    init_isrc_cache_db()?;
    let lock = ISRC_STORE
        .lock()
        .map_err(|e| ProviderError::Other(format!("failed to lock ISRC store: {e}")))?;
    match lock.as_ref() {
        Some(store) => operation(store),
        None => Err(ProviderError::Other(
            "ISRC cache database is not available".to_string(),
        )),
    }
}

/// Retrieves a cached ISRC by track ID (Go's `GetCachedISRC`).
pub fn get_cached_isrc(track_id: &str) -> Result<String> {
    let normalized = track_id.trim();
    if normalized.is_empty() {
        return Ok(String::new());
    }

    with_isrc_cache_db(|store| match store.get(normalized) {
        Ok(Some(isrc)) => Ok(isrc.trim().to_uppercase()),
        Ok(None) => Ok(String::new()),
        Err(e) => Err(ProviderError::Other(e.to_string())),
    })
}

/// Caches an ISRC for a track ID (Go's `PutCachedISRC`).
pub fn put_cached_isrc(track_id: &str, isrc: &str) -> Result<()> {
    let normalized_track_id = track_id.trim();
    let normalized_isrc = isrc.trim().to_uppercase();
    if normalized_track_id.is_empty() || normalized_isrc.is_empty() {
        return Ok(());
    }

    with_isrc_cache_db(|store| {
        store
            .put(normalized_track_id, &normalized_isrc)
            .map_err(|e| ProviderError::Other(e.to_string()))
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_spotify_entity_id_to_gid_vectors() {
        assert_eq!(
            spotify_entity_id_to_gid("").unwrap_err().to_string(),
            "entity ID is empty"
        );

        let zero_gid = spotify_entity_id_to_gid("0").unwrap();
        assert_eq!(zero_gid, "00000000000000000000000000000000");

        let a_gid = spotify_entity_id_to_gid("a").unwrap();
        assert_eq!(a_gid, "0000000000000000000000000000000a");

        let bad_char = spotify_entity_id_to_gid("bad!char").unwrap_err();
        assert!(bad_char.to_string().contains("invalid base62 character"));

        // Realistic 22-char track ID: "2EqlS6tkEnglzr7tkKAAYD"
        let gid = spotify_track_id_to_gid("2EqlS6tkEnglzr7tkKAAYD").unwrap();
        assert_eq!(gid.len(), 32);
        assert!(gid.chars().all(|c| c.is_ascii_hexdigit()));
    }

    #[test]
    fn test_extract_spotify_track_id() {
        assert_eq!(
            extract_spotify_track_id("").unwrap_err().to_string(),
            "track input is required"
        );

        // Raw 22 chars
        assert_eq!(
            extract_spotify_track_id("2EqlS6tkEnglzr7tkKAAYD").unwrap(),
            "2EqlS6tkEnglzr7tkKAAYD"
        );

        // URI
        assert_eq!(
            extract_spotify_track_id("spotify:track:2EqlS6tkEnglzr7tkKAAYD").unwrap(),
            "2EqlS6tkEnglzr7tkKAAYD"
        );

        // URL
        assert_eq!(
            extract_spotify_track_id(
                "https://open.spotify.com/track/2EqlS6tkEnglzr7tkKAAYD?si=123"
            )
            .unwrap(),
            "2EqlS6tkEnglzr7tkKAAYD"
        );

        // Invalid URL
        assert!(
            extract_spotify_track_id("https://open.spotify.com/album/2EqlS6tkEnglzr7tkKAAYD")
                .is_err()
        );

        // Invalid string
        assert_eq!(
            extract_spotify_track_id("too-short")
                .unwrap_err()
                .to_string(),
            "track must be a Spotify track ID, URL, or URI"
        );
    }

    #[test]
    fn test_spotify_token_validity() {
        assert!(!spotify_token_is_valid(None));

        let empty = SpotifyAnonymousToken::default();
        assert!(!spotify_token_is_valid(Some(&empty)));

        let now_ms = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_millis() as i64;

        let expired = SpotifyAnonymousToken {
            access_token: "tok".to_string(),
            access_token_expiration_timestamp_ms: now_ms + 10_000, // < 30_000
        };
        assert!(!spotify_token_is_valid(Some(&expired)));

        let valid = SpotifyAnonymousToken {
            access_token: "tok".to_string(),
            access_token_expiration_timestamp_ms: now_ms + 60_000,
        };
        assert!(spotify_token_is_valid(Some(&valid)));
    }

    #[test]
    fn test_extract_spotify_track_and_album_identifiers() {
        let track_json = include_bytes!("../tests/fixtures/spotify_isrc/track_metadata.json");
        let album_json = include_bytes!("../tests/fixtures/spotify_isrc/album_metadata.json");

        // Extract UPC
        let upc = extract_spotify_album_upc(album_json).unwrap();
        assert_eq!(upc, "00602508123456");

        // Extract ISRC synchronously without client
        let rt = tokio::runtime::Runtime::new().unwrap();
        let isrc = rt.block_on(extract_spotify_track_isrc(track_json)).unwrap();
        assert_eq!(isrc, "GBAYE0601690");
    }

    #[test]
    fn test_merge_spotify_track_identifiers() {
        let mut target = SpotifyTrackIdentifiers {
            isrc: "OLDISRC".to_string(),
            upc: String::new(),
        };

        let incoming = SpotifyTrackIdentifiers {
            isrc: "NEWISRC".to_string(),
            upc: "NEWUPC".to_string(),
        };

        merge_spotify_track_identifiers(&mut target, incoming);
        assert_eq!(target.isrc, "NEWISRC");
        assert_eq!(target.upc, "NEWUPC");
    }

    #[test]
    fn test_isrc_cache_get_put_redb_tempdir() {
        let _guard = ISRC_STORE_TEST_LOCK
            .lock()
            .unwrap_or_else(|e| e.into_inner());
        close_isrc_cache_db();
        let temp = tempfile::tempdir().unwrap();
        let db_path = temp.path().join("test_isrc_cache.redb");

        init_isrc_cache_db_at(&db_path).unwrap();

        // Empty track ID returns empty string
        assert_eq!(get_cached_isrc("").unwrap(), "");
        assert_eq!(get_cached_isrc("   ").unwrap(), "");

        // Empty put is a no-op
        put_cached_isrc("", "ISRC123").unwrap();
        put_cached_isrc("track1", "").unwrap();

        // Cache miss
        assert_eq!(get_cached_isrc("track-abc").unwrap(), "");

        // Cache put & hit
        put_cached_isrc("track-abc", "gbaye0601690").unwrap();
        assert_eq!(get_cached_isrc("track-abc").unwrap(), "GBAYE0601690");

        // Whitespace and case normalization
        assert_eq!(get_cached_isrc("  track-abc  ").unwrap(), "GBAYE0601690");

        close_isrc_cache_db();
    }

    #[tokio::test]
    async fn test_spotify_token_and_raw_metadata_loopback() {
        use tokio::io::{AsyncReadExt, AsyncWriteExt};

        let temp = tempfile::tempdir().unwrap();
        let cache_file = temp.path().join(".isrc-finder-token.json");
        set_spotify_token_cache_path_for_test(Some(cache_file));

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        let track_json = include_str!("../tests/fixtures/spotify_isrc/track_metadata.json");
        let album_json = include_str!("../tests/fixtures/spotify_isrc/album_metadata.json");

        tokio::spawn(async move {
            // 1. Token request
            let (mut socket, _) = listener.accept().await.unwrap();
            let mut buf = vec![0u8; 2048];
            let _ = socket.read(&mut buf).await.unwrap();
            let token_resp = r#"{"accessToken":"mock-token-123","accessTokenExpirationTimestampMs":9999999999999}"#;
            let resp = format!(
                "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                token_resp.len(),
                token_resp
            );
            socket.write_all(resp.as_bytes()).await.unwrap();
            let _ = socket.shutdown().await;

            // 2. Track metadata request
            let (mut socket, _) = listener.accept().await.unwrap();
            let mut buf = vec![0u8; 2048];
            let _ = socket.read(&mut buf).await.unwrap();
            let resp = format!(
                "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                track_json.len(),
                track_json
            );
            socket.write_all(resp.as_bytes()).await.unwrap();
            let _ = socket.shutdown().await;

            // 3. Album metadata request
            let (mut socket, _) = listener.accept().await.unwrap();
            let mut buf = vec![0u8; 2048];
            let _ = socket.read(&mut buf).await.unwrap();
            let resp = format!(
                "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                album_json.len(),
                album_json
            );
            socket.write_all(resp.as_bytes()).await.unwrap();
            let _ = socket.shutdown().await;
        });

        set_spotify_session_token_url_for_test(Some(format!("http://{addr}/api/token")));
        set_spotify_gid_metadata_url_for_test(Some(format!("http://{addr}/metadata/4")));

        let identifiers = get_spotify_track_identifiers_direct("2EqlS6tkEnglzr7tkKAAYD")
            .await
            .unwrap();

        assert_eq!(identifiers.isrc, "GBAYE0601690");
        assert_eq!(identifiers.upc, "00602508123456");

        set_spotify_session_token_url_for_test(None);
        set_spotify_gid_metadata_url_for_test(None);
        set_spotify_token_cache_path_for_test(None);
    }
}

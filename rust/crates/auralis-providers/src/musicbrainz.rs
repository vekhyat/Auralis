//! Port of `backend/musicbrainz.go`.
//!
//! MusicBrainz metadata client, status tracking, throttle/rate limiting,
//! response parsing, and genre extraction.

use std::collections::HashMap;
use std::sync::{LazyLock, Mutex, RwLock};
use std::time::Duration;

use auralis_core::models::Metadata;
use serde::{Deserialize, Serialize};
use tokio::sync::watch;
use tokio::time::Instant;

use crate::error::{ProviderError, Result};

pub const MUSICBRAINZ_API_BASE: &str = "https://musicbrainz.org/ws/2";
pub const MUSICBRAINZ_REQUEST_TIMEOUT: Duration = Duration::from_secs(10);
pub const MUSICBRAINZ_REQUEST_RETRIES: usize = 3;
pub const MUSICBRAINZ_REQUEST_RETRY_WAIT: Duration = Duration::from_secs(3);
pub const MUSICBRAINZ_MIN_REQUEST_INTERVAL: Duration = Duration::from_millis(1100);
pub const MUSICBRAINZ_THROTTLE_COOLDOWN_ON_503: Duration = Duration::from_secs(5);
pub const MUSICBRAINZ_STATUS_CHECK_SKIP_WINDOW: Duration = Duration::from_secs(5 * 60);

static APP_VERSION: RwLock<String> = RwLock::new(String::new());

/// Sets the product version used in the MusicBrainz User-Agent header.
pub fn set_app_version(version: &str) {
    if let Ok(mut lock) = APP_VERSION.write() {
        *lock = version.to_string();
    }
}

/// Gets the product version used in the MusicBrainz User-Agent header, defaulting to "Unknown".
pub fn get_app_version() -> String {
    if let Ok(lock) = APP_VERSION.read() {
        if !lock.is_empty() {
            return lock.clone();
        }
    }
    "Unknown".to_string()
}

/// MusicBrainz status code error matching Go's `musicBrainzStatusError`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MusicBrainzStatusError {
    pub status_code: u16,
}

impl std::fmt::Display for MusicBrainzStatusError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "MusicBrainz API returned status: {}", self.status_code)
    }
}

impl std::error::Error for MusicBrainzStatusError {}

#[derive(Debug, Default)]
pub struct MusicBrainzThrottle {
    pub next_request: Option<Instant>,
    pub blocked_till: Option<Instant>,
}

#[derive(Debug, Default)]
pub struct MusicBrainzStatus {
    pub last_checked_at: Option<std::time::Instant>,
    pub last_checked_online: bool,
}

type InflightSender = watch::Sender<Option<std::result::Result<Metadata, String>>>;
type InflightMap = HashMap<String, InflightSender>;

/// Process-wide state for MusicBrainz cache, in-flight deduplication, rate limiting, and status.
pub struct MusicBrainzState {
    pub cache: RwLock<HashMap<String, Metadata>>,
    pub inflight: tokio::sync::Mutex<InflightMap>,
    pub throttle: Mutex<MusicBrainzThrottle>,
    pub status: RwLock<MusicBrainzStatus>,
    pub api_base: RwLock<Option<String>>,
}

impl MusicBrainzState {
    pub fn new() -> Self {
        Self {
            cache: RwLock::new(HashMap::new()),
            inflight: tokio::sync::Mutex::new(HashMap::new()),
            throttle: Mutex::new(MusicBrainzThrottle::default()),
            status: RwLock::new(MusicBrainzStatus::default()),
            api_base: RwLock::new(None),
        }
    }

    pub fn reset_for_test(&self) {
        if let Ok(mut c) = self.cache.write() {
            c.clear();
        }
        if let Ok(mut t) = self.throttle.lock() {
            *t = MusicBrainzThrottle::default();
        }
        if let Ok(mut s) = self.status.write() {
            *s = MusicBrainzStatus::default();
        }
        if let Ok(mut b) = self.api_base.write() {
            *b = None;
        }
        if let Ok(mut inf) = self.inflight.try_lock() {
            inf.clear();
        }
    }
}

impl Default for MusicBrainzState {
    fn default() -> Self {
        Self::new()
    }
}

static STATE: LazyLock<MusicBrainzState> = LazyLock::new(MusicBrainzState::new);

/// Sets the result of the MusicBrainz status check (Go's `SetMusicBrainzStatusCheckResult`).
pub fn set_musicbrainz_status_check_result(online: bool) {
    if let Ok(mut guard) = STATE.status.write() {
        guard.last_checked_at = Some(std::time::Instant::now());
        guard.last_checked_online = online;
    }
}

/// Checks whether MusicBrainz metadata fetches should be skipped (Go's `ShouldSkipMusicBrainzMetadataFetch`).
pub fn should_skip_musicbrainz_metadata_fetch() -> bool {
    if let Ok(guard) = STATE.status.read() {
        if let Some(last_checked_at) = guard.last_checked_at {
            if guard.last_checked_online {
                return false;
            }
            return last_checked_at.elapsed() <= MUSICBRAINZ_STATUS_CHECK_SKIP_WINDOW;
        }
    }
    false
}

/// Computes the cache key for MusicBrainz metadata lookup (Go's `musicBrainzCacheKey`).
pub fn musicbrainz_cache_key(isrc: &str, use_single_genre: bool) -> String {
    let mut separator = get_separator().trim().to_string();
    if separator.is_empty() {
        separator = ";".to_string();
    }
    format!(
        "{}|{}|{}",
        isrc.trim().to_uppercase(),
        use_single_genre,
        separator
    )
}

/// Waits for a rate-limit slot (Go's `waitForMusicBrainzRequestSlot`).
pub async fn wait_for_musicbrainz_request_slot() {
    let wait_duration = {
        let mut throttle = match STATE.throttle.lock() {
            Ok(t) => t,
            Err(p) => p.into_inner(),
        };
        let now = Instant::now();
        let mut ready_at = throttle.next_request.unwrap_or(now);
        if let Some(blocked) = throttle.blocked_till {
            if blocked > ready_at {
                ready_at = blocked;
            }
        }
        if ready_at < now {
            ready_at = now;
        }
        throttle.next_request = Some(ready_at + MUSICBRAINZ_MIN_REQUEST_INTERVAL);
        if ready_at > now {
            ready_at - now
        } else {
            Duration::ZERO
        }
    };

    if !wait_duration.is_zero() {
        tokio::time::sleep(wait_duration).await;
    }
}

/// Records a 503 throttle cooldown (Go's `noteMusicBrainzThrottle`).
pub fn note_musicbrainz_throttle() {
    let mut throttle = match STATE.throttle.lock() {
        Ok(t) => t,
        Err(p) => p.into_inner(),
    };
    let now = Instant::now();
    let cooldown_until = now + MUSICBRAINZ_THROTTLE_COOLDOWN_ON_503;
    let blocked_till = throttle.blocked_till.unwrap_or(now);
    if cooldown_until > blocked_till {
        throttle.blocked_till = Some(cooldown_until);
    }
    let next_request = throttle.next_request.unwrap_or(now);
    let effective_blocked = throttle.blocked_till.unwrap();
    if next_request < effective_blocked {
        throttle.next_request = Some(effective_blocked);
    }
}

/// Determines if a MusicBrainz request should be retried (Go's `shouldRetryMusicBrainzRequest`).
pub fn should_retry_musicbrainz_request(err: &ProviderError) -> bool {
    match err {
        ProviderError::Other(msg) => {
            if let Some(rest) = msg.strip_prefix("MusicBrainz API returned status: ") {
                if let Ok(status) = rest.trim().parse::<u16>() {
                    return status == 503 || status >= 500;
                }
            }
            true
        }
        ProviderError::Http(_) => true,
        ProviderError::Io(_) => true,
        _ => true,
    }
}

/// Overrides the MusicBrainz API base URL for testing.
pub fn set_musicbrainz_api_base_for_test(base: Option<String>) {
    if let Ok(mut lock) = STATE.api_base.write() {
        *lock = base;
    }
}

/// Clears state for testing.
pub fn reset_musicbrainz_state_for_test() {
    STATE.reset_for_test();
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MusicBrainzRecordingResponse {
    #[serde(default)]
    pub recordings: Vec<MusicBrainzRecording>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MusicBrainzRecording {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub title: String,
    #[serde(default)]
    pub length: Option<i64>,
    #[serde(default)]
    pub releases: Vec<MusicBrainzRelease>,
    #[serde(default, rename = "artist-credit")]
    pub artist_credit: Vec<MusicBrainzArtistCredit>,
    #[serde(default)]
    pub tags: Vec<MusicBrainzTag>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MusicBrainzRelease {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub title: String,
    #[serde(default)]
    pub status: Option<String>,
    #[serde(default, rename = "release-group")]
    pub release_group: Option<MusicBrainzReleaseGroup>,
    #[serde(default)]
    pub date: Option<String>,
    #[serde(default)]
    pub country: Option<String>,
    #[serde(default)]
    pub media: Vec<MusicBrainzMedia>,
    #[serde(default, rename = "label-info")]
    pub label_info: Vec<MusicBrainzLabelInfo>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MusicBrainzReleaseGroup {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub title: String,
    #[serde(default, rename = "primary-type")]
    pub primary_type: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MusicBrainzMedia {
    #[serde(default)]
    pub format: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MusicBrainzLabelInfo {
    #[serde(default)]
    pub label: Option<MusicBrainzLabel>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MusicBrainzLabel {
    #[serde(default)]
    pub name: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MusicBrainzArtistCredit {
    #[serde(default)]
    pub artist: MusicBrainzArtist,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MusicBrainzArtist {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub name: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct MusicBrainzTag {
    #[serde(default)]
    pub count: i64,
    #[serde(default)]
    pub name: String,
}

/// Queries recordings from the MusicBrainz API (Go's `queryMusicBrainzRecordings`).
pub async fn query_musicbrainz_recordings(
    client: &reqwest::Client,
    query: &str,
) -> Result<MusicBrainzRecordingResponse> {
    wait_for_musicbrainz_request_slot().await;

    let base = STATE
        .api_base
        .read()
        .ok()
        .and_then(|b| b.clone())
        .unwrap_or_else(|| MUSICBRAINZ_API_BASE.to_string());

    let encoded_query = url::form_urlencoded::byte_serialize(query.as_bytes()).collect::<String>();
    let target_url = format!(
        "{base}/recording?query={encoded_query}&fmt=json&inc=releases+artist-credits+tags+media+release-groups+labels"
    );

    let user_agent = format!(
        "Auralis/{} ( https://github.com/vekhyat/Auralis )",
        get_app_version()
    );

    let resp = client
        .get(&target_url)
        .header(reqwest::header::USER_AGENT, user_agent)
        .header(reqwest::header::ACCEPT, "application/json")
        .send()
        .await?;

    if resp.status() == reqwest::StatusCode::SERVICE_UNAVAILABLE {
        note_musicbrainz_throttle();
    }

    if !resp.status().is_success() {
        return Err(ProviderError::Other(format!(
            "MusicBrainz API returned status: {}",
            resp.status().as_u16()
        )));
    }

    let payload: MusicBrainzRecordingResponse = resp.json().await?;
    Ok(payload)
}

/// Fetches metadata from MusicBrainz by ISRC (Go's `FetchMusicBrainzMetadata`).
pub async fn fetch_musicbrainz_metadata(
    isrc: &str,
    _title: &str,
    _artist: &str,
    _album: &str,
    use_single_genre: bool,
    _embed_genre: bool,
) -> Result<Metadata> {
    if should_skip_musicbrainz_metadata_fetch() {
        return Err(ProviderError::Other(
            "skipping MusicBrainz metadata fetch because status check indicated service is offline"
                .to_string(),
        ));
    }

    let cache_key = musicbrainz_cache_key(isrc, use_single_genre);

    if let Ok(cache) = STATE.cache.read() {
        if let Some(cached) = cache.get(&cache_key) {
            return Ok(cached.clone());
        }
    }

    let (mut rx, is_leader) = {
        let mut inflight = STATE.inflight.lock().await;
        if let Some(sender) = inflight.get(&cache_key) {
            (sender.subscribe(), false)
        } else {
            let (tx, rx) = watch::channel(None);
            inflight.insert(cache_key.clone(), tx);
            (rx, true)
        }
    };

    if !is_leader {
        while rx.borrow().is_none() {
            if rx.changed().await.is_err() {
                return Err(ProviderError::Other(
                    "inflight call closed without result".to_string(),
                ));
            }
        }
        let res = rx.borrow().as_ref().unwrap().clone();
        return res.map_err(ProviderError::Other);
    }

    let execute_fetch = async {
        let client = reqwest::Client::builder()
            .timeout(MUSICBRAINZ_REQUEST_TIMEOUT)
            .build()?;

        let query = format!("isrc:{}", isrc.trim());
        let mut payload: Option<MusicBrainzRecordingResponse> = None;
        let mut last_err: Option<ProviderError> = None;

        for attempt in 0..MUSICBRAINZ_REQUEST_RETRIES {
            match query_musicbrainz_recordings(&client, &query).await {
                Ok(data) => {
                    payload = Some(data);
                    last_err = None;
                    break;
                }
                Err(err) => {
                    if !should_retry_musicbrainz_request(&err) {
                        return Err(err);
                    }
                    last_err = Some(err);
                    if attempt < MUSICBRAINZ_REQUEST_RETRIES - 1 {
                        tokio::time::sleep(MUSICBRAINZ_REQUEST_RETRY_WAIT).await;
                    }
                }
            }
        }

        let payload = match payload {
            Some(p) => p,
            None => {
                return Err(last_err.unwrap_or_else(|| {
                    ProviderError::Other("MusicBrainz request failed".to_string())
                }));
            }
        };

        if payload.recordings.is_empty() {
            return Err(ProviderError::Other(format!(
                "no recordings found in MusicBrainz for ISRC: {isrc}"
            )));
        }

        let recording = &payload.recordings[0];
        let mut meta = Metadata::default();

        if use_single_genre {
            let mut max_count: i64 = -1;
            let mut best_tag = String::new();

            for tag in &recording.tags {
                if tag.count > max_count {
                    max_count = tag.count;
                    best_tag = tag.name.clone();
                }
            }

            if !best_tag.is_empty() {
                meta.genre = title_case(&best_tag);
            }
        } else {
            let mut genres = Vec::new();
            for tag in &recording.tags {
                genres.push(title_case(&tag.name));
            }
            if !genres.is_empty() {
                if genres.len() > 5 {
                    genres.truncate(5);
                }
                meta.genre = genres.join(&get_separator());
            }
        }

        if meta.genre.is_empty() {
            return Err(ProviderError::Other(
                "no genre tags found in MusicBrainz".to_string(),
            ));
        }

        if let Ok(mut cache) = STATE.cache.write() {
            cache.insert(cache_key.clone(), meta.clone());
        }

        Ok(meta)
    };

    let result = execute_fetch.await;

    // Clean up inflight map and broadcast result to all waiters
    let sender = {
        let mut inflight = STATE.inflight.lock().await;
        inflight.remove(&cache_key)
    };
    if let Some(tx) = sender {
        let outcome = match &result {
            Ok(m) => Ok(m.clone()),
            Err(e) => Err(e.to_string()),
        };
        let _ = tx.send(Some(outcome));
    }

    result
}

fn get_separator() -> String {
    if let Ok(settings) = auralis_config::SettingsStore::default().load() {
        if let Some(val) = settings.get("separator").and_then(|v| v.as_str()) {
            if val == "comma" {
                return ", ".to_string();
            }
            if val == "semicolon" {
                return "; ".to_string();
            }
        }
    }
    "; ".to_string()
}

fn title_case(s: &str) -> String {
    let mut result = String::with_capacity(s.len());
    let mut capitalize_next = true;
    for c in s.chars() {
        if c.is_alphanumeric() {
            if capitalize_next {
                result.extend(c.to_uppercase());
                capitalize_next = false;
            } else {
                result.push(c);
            }
        } else {
            result.push(c);
            capitalize_next = true;
        }
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;

    static TEST_MUTEX: tokio::sync::Mutex<()> = tokio::sync::Mutex::const_new(());

    #[test]
    fn test_musicbrainz_status_error() {
        let err = MusicBrainzStatusError { status_code: 503 };
        assert_eq!(err.to_string(), "MusicBrainz API returned status: 503");
        let err404 = MusicBrainzStatusError { status_code: 404 };
        assert_eq!(err404.to_string(), "MusicBrainz API returned status: 404");
    }

    #[test]
    fn test_musicbrainz_status_check() {
        reset_musicbrainz_state_for_test();
        assert!(!should_skip_musicbrainz_metadata_fetch());

        set_musicbrainz_status_check_result(true);
        assert!(!should_skip_musicbrainz_metadata_fetch());

        set_musicbrainz_status_check_result(false);
        assert!(should_skip_musicbrainz_metadata_fetch());

        reset_musicbrainz_state_for_test();
    }

    #[test]
    fn test_musicbrainz_cache_key() {
        let key1 = musicbrainz_cache_key("gbaye0601690", false);
        assert!(key1.starts_with("GBAYE0601690|false|"));

        let key2 = musicbrainz_cache_key("  GBAYE0601690  ", true);
        assert!(key2.starts_with("GBAYE0601690|true|"));
    }

    #[test]
    fn test_should_retry_musicbrainz_request() {
        let err503 = ProviderError::Other("MusicBrainz API returned status: 503".to_string());
        assert!(should_retry_musicbrainz_request(&err503));

        let err500 = ProviderError::Other("MusicBrainz API returned status: 500".to_string());
        assert!(should_retry_musicbrainz_request(&err500));

        let err404 = ProviderError::Other("MusicBrainz API returned status: 404".to_string());
        assert!(!should_retry_musicbrainz_request(&err404));

        let err400 = ProviderError::Other("MusicBrainz API returned status: 400".to_string());
        assert!(!should_retry_musicbrainz_request(&err400));
    }

    #[test]
    fn test_title_case() {
        assert_eq!(title_case("rock"), "Rock");
        assert_eq!(title_case("classic rock"), "Classic Rock");
        assert_eq!(title_case("blues-rock"), "Blues-Rock");
        assert_eq!(title_case("post-punk / new wave"), "Post-Punk / New Wave");
    }

    #[tokio::test]
    async fn test_note_musicbrainz_throttle() {
        let _lock = TEST_MUTEX.lock().await;
        reset_musicbrainz_state_for_test();
        note_musicbrainz_throttle();
        {
            let guard = STATE.throttle.lock().unwrap();
            assert!(guard.blocked_till.is_some());
            assert!(guard.next_request.is_some());
        }
        reset_musicbrainz_state_for_test();
    }

    #[tokio::test]
    async fn test_fetch_musicbrainz_metadata_loopback() {
        let _lock = TEST_MUTEX.lock().await;
        reset_musicbrainz_state_for_test();

        use tokio::io::{AsyncReadExt, AsyncWriteExt};
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        let fixture = include_str!("../tests/fixtures/musicbrainz/come_together.json");

        tokio::spawn(async move {
            let (mut socket, _) = listener.accept().await.unwrap();
            let mut buf = vec![0u8; 2048];
            let _ = socket.read(&mut buf).await.unwrap();
            let response = format!(
                "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                fixture.len(),
                fixture
            );
            socket.write_all(response.as_bytes()).await.unwrap();
            let _ = socket.shutdown().await;
        });

        set_musicbrainz_api_base_for_test(Some(format!("http://{addr}")));

        // Single genre test
        let meta_single = fetch_musicbrainz_metadata(
            "GBAYE0601690",
            "Come Together",
            "The Beatles",
            "Abbey Road",
            true,
            true,
        )
        .await
        .unwrap();

        assert_eq!(meta_single.genre, "Rock");

        // Now test multi genre (cache key differs because use_single_genre = false)
        // Set up listener for second request on new port to avoid TIME_WAIT
        let listener2 = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr2 = listener2.local_addr().unwrap();
        tokio::spawn(async move {
            let (mut socket, _) = listener2.accept().await.unwrap();
            let mut buf = vec![0u8; 2048];
            let _ = socket.read(&mut buf).await.unwrap();
            let response = format!(
                "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                fixture.len(),
                fixture
            );
            socket.write_all(response.as_bytes()).await.unwrap();
            let _ = socket.shutdown().await;
        });
        set_musicbrainz_api_base_for_test(Some(format!("http://{addr2}")));

        let meta_multi = fetch_musicbrainz_metadata(
            "GBAYE0601690",
            "Come Together",
            "The Beatles",
            "Abbey Road",
            false,
            true,
        )
        .await
        .unwrap();

        // Must cap at 5 genres joined with separator
        assert!(meta_multi.genre.starts_with("Rock"));
        assert!(meta_multi.genre.contains("Classic Rock"));
        assert!(meta_multi.genre.contains("Blues Rock"));

        reset_musicbrainz_state_for_test();
    }

    #[tokio::test]
    async fn test_fetch_musicbrainz_metadata_empty_recordings() {
        let _lock = TEST_MUTEX.lock().await;
        reset_musicbrainz_state_for_test();

        use tokio::io::{AsyncReadExt, AsyncWriteExt};
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        let fixture = include_str!("../tests/fixtures/musicbrainz/empty.json");

        tokio::spawn(async move {
            let (mut socket, _) = listener.accept().await.unwrap();
            let mut buf = vec![0u8; 2048];
            let _ = socket.read(&mut buf).await.unwrap();
            let response = format!(
                "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                fixture.len(),
                fixture
            );
            socket.write_all(response.as_bytes()).await.unwrap();
            let _ = socket.shutdown().await;
        });

        set_musicbrainz_api_base_for_test(Some(format!("http://{addr}")));

        let err =
            fetch_musicbrainz_metadata("UNKNOWN00000", "Unknown", "Unknown", "Unknown", true, true)
                .await
                .unwrap_err();

        assert_eq!(
            err.to_string(),
            "no recordings found in MusicBrainz for ISRC: UNKNOWN00000"
        );
        reset_musicbrainz_state_for_test();
    }

    #[tokio::test]
    async fn test_fetch_musicbrainz_metadata_no_genre_tags() {
        let _lock = TEST_MUTEX.lock().await;
        reset_musicbrainz_state_for_test();

        use tokio::io::{AsyncReadExt, AsyncWriteExt};
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        let fixture = include_str!("../tests/fixtures/musicbrainz/no_tags.json");

        tokio::spawn(async move {
            let (mut socket, _) = listener.accept().await.unwrap();
            let mut buf = vec![0u8; 2048];
            let _ = socket.read(&mut buf).await.unwrap();
            let response = format!(
                "HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                fixture.len(),
                fixture
            );
            socket.write_all(response.as_bytes()).await.unwrap();
            let _ = socket.shutdown().await;
        });

        set_musicbrainz_api_base_for_test(Some(format!("http://{addr}")));

        let err =
            fetch_musicbrainz_metadata("NOTAGS00000", "No Tags", "Unknown", "Unknown", true, true)
                .await
                .unwrap_err();

        assert_eq!(err.to_string(), "no genre tags found in MusicBrainz");
        reset_musicbrainz_state_for_test();
    }
}

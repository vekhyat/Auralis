//! Zarz resolve client and protocol grant coordinator.

use std::sync::{LazyLock, Mutex};
use std::time::Duration;

use regex::Regex;
use serde::Deserialize;
use tokio::sync::mpsc;

use crate::error::{ProviderError, Result};

pub const ZARZ_RESOLVE_URL: &str = "https://api.zarz.moe/v1/resolve";

static AMAZON_ALBUM_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"/albums/[A-Z0-9]{10}/(B[0-9A-Z]{9})").expect("valid regex"));
static AMAZON_TRACK_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"/tracks/(B[0-9A-Z]{9})").expect("valid regex"));
static REDELIVER_GRANT_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r#"var redeliverGrant = ("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')"#)
        .expect("valid regex")
});

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ResolvedTrackLinks {
    pub isrc: String,
    pub tidal_url: String,
    pub amazon_url: String,
    pub qobuz_url: String,
    pub deezer_url: String,
}

impl ResolvedTrackLinks {
    fn has_any_link(&self) -> bool {
        !self.tidal_url.is_empty()
            || !self.amazon_url.is_empty()
            || !self.qobuz_url.is_empty()
            || !self.deezer_url.is_empty()
    }
}

#[derive(Debug, Deserialize)]
struct ResolveBody {
    success: bool,
    #[serde(default)]
    isrc: String,
    #[serde(default, rename = "songUrls")]
    song_urls: std::collections::HashMap<String, serde_json::Value>,
}

pub struct ZarzResolver;

impl ZarzResolver {
    pub fn parse_resolve_body(body: &[u8]) -> Result<ResolvedTrackLinks> {
        let parsed: ResolveBody = serde_json::from_slice(body).map_err(|err| {
            ProviderError::ResolutionFailed(format!("invalid zarz resolve response: {err}"))
        })?;
        if !parsed.success {
            return Err(ProviderError::ResolutionFailed(
                "zarz resolve returned success=false".to_string(),
            ));
        }

        let mut links = ResolvedTrackLinks {
            isrc: parsed.isrc.trim().to_uppercase(),
            ..ResolvedTrackLinks::default()
        };
        for (key, raw) in parsed.song_urls {
            let value = extract_resolve_url_value(&raw);
            if value.is_empty() {
                continue;
            }
            match key.trim().to_lowercase().as_str() {
                "tidal" if links.tidal_url.is_empty() => {
                    links.tidal_url = value;
                }
                "amazonmusic" | "amazon" if links.amazon_url.is_empty() => {
                    let normalized = normalize_amazon_music_url(&value);
                    links.amazon_url = if normalized.is_empty() {
                        value
                    } else {
                        normalized
                    };
                }
                "qobuz" if links.qobuz_url.is_empty() => {
                    links.qobuz_url = value;
                }
                "deezer" if links.deezer_url.is_empty() => {
                    links.deezer_url = normalize_deezer_track_url(&value);
                }
                _ => {}
            }
        }

        if !links.has_any_link() {
            return Err(ProviderError::ResolutionFailed(
                "zarz resolve returned no platform links".to_string(),
            ));
        }
        Ok(links)
    }

    pub async fn resolve(spotify_track_id: &str) -> Result<ResolvedTrackLinks> {
        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(20))
            .build()?;
        let payload = serde_json::json!({
            "platform": "spotify",
            "type": "track",
            "id": spotify_track_id,
        });
        let resp = client
            .post(ZARZ_RESOLVE_URL)
            .header("Content-Type", "application/json")
            .header("Accept", "application/json")
            .header("User-Agent", "Auralis/1.0 tidal-web@1.1.0")
            .json(&payload)
            .send()
            .await
            .map_err(|err| ProviderError::Other(format!("zarz resolve failed: {err}")))?;
        if resp.status() != reqwest::StatusCode::OK {
            return Err(ProviderError::Other(format!(
                "zarz resolve returned HTTP {}",
                resp.status()
            )));
        }
        let body = resp.bytes().await?;
        Self::parse_resolve_body(&body)
    }
}

fn extract_resolve_url_value(raw: &serde_json::Value) -> String {
    match raw {
        serde_json::Value::String(value) => value.trim().to_string(),
        serde_json::Value::Array(items) => {
            for candidate in items {
                if let serde_json::Value::String(value) = candidate {
                    let cleaned = value.trim();
                    if !cleaned.is_empty() {
                        return cleaned.to_string();
                    }
                }
            }
            String::new()
        }
        _ => String::new(),
    }
}

pub fn normalize_amazon_music_url(raw_url: &str) -> String {
    let amazon_url = raw_url.trim();
    if amazon_url.is_empty() {
        return String::new();
    }

    if let Some(idx) = amazon_url.find("trackAsin=") {
        let rest = &amazon_url[idx + "trackAsin=".len()..];
        let track_asin = rest.split('&').next().unwrap_or("");
        if !track_asin.is_empty() {
            return format!("https://music.amazon.com/tracks/{track_asin}?musicTerritory=US");
        }
    }

    if let Some(caps) = AMAZON_ALBUM_RE.captures(amazon_url) {
        return format!(
            "https://music.amazon.com/tracks/{}?musicTerritory=US",
            &caps[1]
        );
    }

    if let Some(caps) = AMAZON_TRACK_RE.captures(amazon_url) {
        return format!(
            "https://music.amazon.com/tracks/{}?musicTerritory=US",
            &caps[1]
        );
    }

    String::new()
}

pub fn normalize_deezer_track_url(raw_url: &str) -> String {
    match extract_deezer_track_id(raw_url) {
        Some(track_id) => format!("https://www.deezer.com/track/{track_id}"),
        None => raw_url.trim().to_string(),
    }
}

fn extract_deezer_track_id(raw_url: &str) -> Option<String> {
    let clean = raw_url.trim();
    if clean.is_empty() {
        return None;
    }
    let mut parts = clean.splitn(2, "/track/");
    parts.next();
    let rest = parts.next()?;
    let track_id = rest
        .split('?')
        .next()
        .unwrap_or("")
        .trim_matches(|c| c == '/' || c == ' ');
    if track_id.is_empty() {
        None
    } else {
        Some(track_id.to_string())
    }
}

// ---------------------------------------------------------------------------
// Protocol grant coordinator
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ZarzGrant {
    pub grant: String,
    pub state: String,
    pub from_url: bool,
}

pub fn extract_zarz_grant(raw: &str) -> String {
    parse_zarz_grant(raw).grant
}

pub fn parse_zarz_grant(raw: &str) -> ZarzGrant {
    let raw = raw.trim();
    if raw.is_empty() {
        return ZarzGrant::default();
    }

    if raw.len() >= 6 && raw[..6].eq_ignore_ascii_case("grant=") {
        let value = query_unescape(&raw[6..]);
        return ZarzGrant {
            grant: value.trim().to_string(),
            ..ZarzGrant::default()
        }
        .with_from_url();
    }

    if let Ok(url) = url::Url::parse(raw) {
        if !url.scheme().is_empty() {
            let mut grant = String::new();
            let mut state = String::new();
            for (key, value) in url.query_pairs() {
                match key.as_ref() {
                    "grant" => grant = value.trim().to_string(),
                    "state" => state = value.trim().to_string(),
                    _ => {}
                }
            }
            if !grant.is_empty() {
                return ZarzGrant {
                    grant,
                    state,
                    from_url: true,
                };
            }
            return ZarzGrant::default();
        }
    }

    if let Some(idx) = raw.find("grant=") {
        let mut value = &raw[idx + 6..];
        for stop in ['&', '\n', ' '] {
            if let Some(pos) = value.find(stop) {
                value = &value[..pos];
            }
        }
        let decoded = query_unescape(value);
        return ZarzGrant {
            grant: decoded.trim().to_string(),
            from_url: true,
            ..ZarzGrant::default()
        };
    }

    if !raw.chars().any(char::is_whitespace) && !raw.contains("://") && raw.len() >= 16 {
        return ZarzGrant {
            grant: raw.to_string(),
            ..ZarzGrant::default()
        };
    }

    ZarzGrant::default()
}

impl ZarzGrant {
    fn with_from_url(self) -> Self {
        ZarzGrant {
            from_url: true,
            ..self
        }
    }
}

fn query_unescape(value: &str) -> String {
    url::form_urlencoded::parse(format!("q={value}").as_bytes())
        .into_owned()
        .find_map(|(key, v)| (key == "q").then_some(v))
        .unwrap_or_else(|| value.to_string())
}

struct Waiter {
    id: u64,
    sender: mpsc::UnboundedSender<String>,
}

#[derive(Default)]
struct CoordinatorState {
    expected_state: String,
    pending_grant: Option<String>,
    waiters: Vec<Waiter>,
    next_id: u64,
}

/// Coordinates delivery of Zarz protocol-handler grants to waiting flows.
#[derive(Default)]
pub struct ZarzGrantCoordinator {
    state: Mutex<CoordinatorState>,
}

impl ZarzGrantCoordinator {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn set_expected_state(&self, state: &str) {
        let mut inner = self.state.lock().expect("grant coordinator poisoned");
        inner.expected_state = state.to_string();
        inner.pending_grant = None;
    }

    pub fn clear_expected_state(&self) {
        let mut inner = self.state.lock().expect("grant coordinator poisoned");
        inner.expected_state.clear();
    }

    pub fn pending_grant(&self) -> Option<String> {
        self.state
            .lock()
            .expect("grant coordinator poisoned")
            .pending_grant
            .clone()
    }

    pub fn handle_args(&self, args: &[String]) -> bool {
        for arg in args {
            let trimmed = arg.trim().trim_matches(|c| c == '"' || c == '\'');
            let grant = extract_zarz_grant(trimmed);
            if !grant.is_empty() {
                self.deliver(trimmed);
                return true;
            }
        }
        false
    }

    pub fn deliver(&self, raw: &str) {
        let parsed = parse_zarz_grant(raw);
        if parsed.grant.is_empty() {
            return;
        }

        let waiters = {
            let mut inner = self.state.lock().expect("grant coordinator poisoned");
            if !inner.expected_state.is_empty()
                && parsed.from_url
                && !parsed.state.is_empty()
                && parsed.state != inner.expected_state
            {
                return;
            }
            if inner.waiters.is_empty() {
                if inner.expected_state.is_empty() {
                    return;
                }
                inner.pending_grant = Some(parsed.grant);
                return;
            }
            inner.pending_grant = None;
            std::mem::take(&mut inner.waiters)
        };

        for waiter in waiters {
            let _ = waiter.sender.send(parsed.grant.clone());
        }
    }

    pub async fn wait(&self, timeout: Duration) -> Result<String> {
        let (id, mut receiver) = self.register_waiter();
        let result = tokio::time::timeout(timeout, receiver.recv()).await;
        self.unregister_waiter(id);
        match result {
            Ok(Some(grant)) => Ok(grant),
            Ok(None) => Err(ProviderError::Auth("grant channel closed".to_string())),
            Err(_) => Err(ProviderError::Auth(
                "timed out waiting for zarz grant".to_string(),
            )),
        }
    }

    pub async fn wait_with_abort(
        &self,
        timeout: Duration,
        abort: impl std::future::Future<Output = ()>,
    ) -> Result<String> {
        let (id, mut receiver) = self.register_waiter();
        tokio::pin!(abort);
        let result = tokio::select! {
            grant = receiver.recv() => Ok(grant),
            _ = &mut abort => Err(ProviderError::Auth("grant window dismissed".to_string())),
            _ = tokio::time::sleep(timeout) => Err(ProviderError::Auth("timed out waiting for zarz grant".to_string())),
        };
        self.unregister_waiter(id);
        match result {
            Ok(Some(grant)) => Ok(grant),
            Ok(None) => Err(ProviderError::Auth("grant channel closed".to_string())),
            Err(err) => Err(err),
        }
    }

    fn register_waiter(&self) -> (u64, mpsc::UnboundedReceiver<String>) {
        let (sender, receiver) = mpsc::unbounded_channel();
        let mut inner = self.state.lock().expect("grant coordinator poisoned");
        if let Some(grant) = inner.pending_grant.take() {
            let _ = sender.send(grant);
        }
        let id = inner.next_id;
        inner.next_id += 1;
        inner.waiters.push(Waiter { id, sender });
        (id, receiver)
    }

    fn unregister_waiter(&self, id: u64) {
        let mut inner = self.state.lock().expect("grant coordinator poisoned");
        inner.waiters.retain(|waiter| waiter.id != id);
    }
}

/// Extracts a redelivered grant from a challenge page body.
pub fn parse_redelivered_grant(body: &[u8]) -> Option<String> {
    let text = String::from_utf8_lossy(body);
    let caps = REDELIVER_GRANT_RE.captures(&text)?;
    let quoted = &caps[1];
    let unquoted = unquote(quoted);
    Some(unquoted.trim().to_string())
}

fn unquote(quoted: &str) -> String {
    serde_json::from_str::<String>(quoted)
        .unwrap_or_else(|_| quoted.trim_matches(|c| c == '"' || c == '\'').to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_resolve_links() {
        let body = br#"{
            "success": true,
            "isrc": "usrc12345678",
            "songUrls": {
                "tidal": "https://tidal.com/track/1",
                "amazonMusic": "https://music.amazon.com/tracks/B012345678?musicTerritory=US",
                "qobuz": ["https://qobuz.com/track/1"],
                "deezer": "https://www.deezer.com/track/123?foo=bar"
            }
        }"#;
        let links = ZarzResolver::parse_resolve_body(body).expect("parses");
        assert_eq!(links.isrc, "USRC12345678");
        assert_eq!(links.tidal_url, "https://tidal.com/track/1");
        assert_eq!(
            links.amazon_url,
            "https://music.amazon.com/tracks/B012345678?musicTerritory=US"
        );
        assert_eq!(links.qobuz_url, "https://qobuz.com/track/1");
        assert_eq!(links.deezer_url, "https://www.deezer.com/track/123");
    }

    #[test]
    fn rejects_failure_body() {
        let err = ZarzResolver::parse_resolve_body(br#"{"success":false,"songUrls":{}}"#)
            .expect_err("failure");
        assert!(err.to_string().contains("success=false"));
    }

    #[test]
    fn rejects_body_without_links() {
        let err = ZarzResolver::parse_resolve_body(br#"{"success":true,"songUrls":{}}"#)
            .expect_err("no links");
        assert!(err.to_string().contains("no platform links"));
    }

    #[test]
    fn amazon_normalize_handles_track_asin_and_paths() {
        assert_eq!(
            normalize_amazon_music_url(
                "https://music.amazon.com/albums/B0ABCDEFGH?trackAsin=B012345678&marketplace=US"
            ),
            "https://music.amazon.com/tracks/B012345678?musicTerritory=US"
        );
        assert_eq!(
            normalize_amazon_music_url("https://music.amazon.com/albums/B0ABCDEFGH/B098765432"),
            "https://music.amazon.com/tracks/B098765432?musicTerritory=US"
        );
        assert_eq!(normalize_amazon_music_url("https://example.com"), "");
    }

    #[test]
    fn deezer_normalize_strips_query() {
        assert_eq!(
            normalize_deezer_track_url("https://www.deezer.com/track/987654?utm=x"),
            "https://www.deezer.com/track/987654"
        );
        assert_eq!(
            normalize_deezer_track_url("not a deezer url"),
            "not a deezer url"
        );
    }

    #[test]
    fn parses_grant_from_protocol_url() {
        let parsed = parse_zarz_grant(
            "auralis://session-grant?cb_version=v2grant&state=abc123&grant=token-value",
        );
        assert_eq!(parsed.grant, "token-value");
        assert_eq!(parsed.state, "abc123");
        assert!(parsed.from_url);
    }

    #[test]
    fn rejects_bare_protocol_without_grant() {
        let parsed = parse_zarz_grant("auralis://session-grant?state=abc123def4567890");
        assert!(parsed.grant.is_empty());
    }

    #[test]
    fn accepts_raw_token() {
        let parsed = parse_zarz_grant("abcdefghijklmnopqrstuvwxyz");
        assert_eq!(parsed.grant, "abcdefghijklmnopqrstuvwxyz");
        assert!(!parsed.from_url);
    }

    #[test]
    fn parses_grant_prefixed_value() {
        let parsed = parse_zarz_grant("grant=token%20value");
        assert_eq!(parsed.grant, "token value");
        assert!(parsed.from_url);
    }

    #[test]
    fn deliver_drops_mismatched_state_and_idle_junk() {
        let coordinator = ZarzGrantCoordinator::new();
        coordinator.set_expected_state("expected-state");
        coordinator.deliver("auralis://session-grant?state=other-state&grant=stolen");
        assert_eq!(coordinator.pending_grant(), None);

        coordinator.clear_expected_state();
        coordinator.deliver("abcdefghijklmnopqrstuvwxyz");
        assert_eq!(coordinator.pending_grant(), None);
    }

    #[tokio::test]
    async fn pending_grant_is_delivered_to_waiter() {
        let coordinator = ZarzGrantCoordinator::new();
        coordinator.set_expected_state("expected-state");
        coordinator.deliver("grant=token-value");
        assert_eq!(coordinator.pending_grant().as_deref(), Some("token-value"));
        let grant = coordinator
            .wait(Duration::from_millis(50))
            .await
            .expect("grant");
        assert_eq!(grant, "token-value");
        assert_eq!(coordinator.pending_grant(), None);
    }

    #[tokio::test]
    async fn wait_with_abort_unblocks() {
        let coordinator = ZarzGrantCoordinator::new();
        coordinator.set_expected_state("expected-state");
        let err = coordinator
            .wait_with_abort(Duration::from_secs(30), async {
                tokio::time::sleep(Duration::from_millis(20)).await;
            })
            .await
            .expect_err("aborted");
        assert!(err.to_string().contains("dismissed"));
    }

    #[tokio::test]
    async fn wait_times_out() {
        let coordinator = ZarzGrantCoordinator::new();
        coordinator.set_expected_state("expected-state");
        let err = coordinator
            .wait(Duration::from_millis(20))
            .await
            .expect_err("timeout");
        assert!(err.to_string().contains("timed out"));
    }

    #[test]
    fn parses_redelivered_grant_from_challenge_page() {
        let body = br#"<html><script>var redeliverGrant = "challenge-token-1";</script></html>"#;
        assert_eq!(
            parse_redelivered_grant(body).as_deref(),
            Some("challenge-token-1")
        );
        assert_eq!(parse_redelivered_grant(b"<html>nothing</html>"), None);
    }
}

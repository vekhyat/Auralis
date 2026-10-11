//! Antra dynamic mirror pool and manifest loader.

use std::future::Future;
use std::pin::Pin;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use serde::Deserialize;

use crate::error::{ProviderError, Result};

/// Public Antra endpoint gist. Antra ships this URL to every client so the
/// mirror pool can change without an app update.
pub const ANTRA_MANIFEST_URL: &str =
    "https://gist.githubusercontent.com/anandprtp/fdc2c16b7bfdc2d337fbc86161b79371/raw";

/// Cache lifetime for a successfully fetched manifest.
pub const ANTRA_MANIFEST_TTL: Duration = Duration::from_secs(30 * 60);

#[derive(Debug, Clone, Default, Deserialize, PartialEq, Eq)]
pub struct AntraMirrors {
    pub tidal: String,
    pub qobuz: String,
    pub deezer: String,
    pub amazon: String,
    pub apple: String,
}

#[derive(Debug, Clone, Default, Deserialize, PartialEq, Eq)]
pub struct AntraManifest {
    pub mirrors: AntraMirrors,
    pub api_key: String,
}

impl AntraManifest {
    pub fn parse(body: &[u8]) -> Result<Self> {
        let mut parsed: AntraManifest = serde_json::from_slice(body)?;
        parsed.api_key = parsed.api_key.trim().to_string();
        Ok(parsed)
    }

    pub fn mirror_url(&self, service: &str) -> Option<String> {
        let raw = match service.trim().to_lowercase().as_str() {
            "tidal" => &self.mirrors.tidal,
            "qobuz" => &self.mirrors.qobuz,
            "deezer" => &self.mirrors.deezer,
            "amazon" => &self.mirrors.amazon,
            "apple" => &self.mirrors.apple,
            _ => return None,
        };
        Some(raw.trim().trim_end_matches('/').to_string())
    }
}

type ManifestFuture = Pin<Box<dyn Future<Output = Result<AntraManifest>> + Send>>;
type ManifestFetcher = Arc<dyn Fn() -> ManifestFuture + Send + Sync>;

#[derive(Default)]
struct PoolState {
    cached: Option<AntraManifest>,
    fetched_at: Option<Instant>,
}

/// Thread-safe cached loader for the Antra endpoint manifest. On refresh
/// failure it falls back to the stale cached copy when one is available.
pub struct AntraPool {
    fetch: ManifestFetcher,
    ttl: Duration,
    state: Mutex<PoolState>,
}

impl Default for AntraPool {
    fn default() -> Self {
        Self::new()
    }
}

impl AntraPool {
    pub fn new() -> Self {
        Self::with_ttl(ANTRA_MANIFEST_TTL, || Box::pin(fetch_manifest()))
    }

    pub fn with_fetcher<F>(fetch: F) -> Self
    where
        F: Fn() -> ManifestFuture + Send + Sync + 'static,
    {
        Self::with_ttl(ANTRA_MANIFEST_TTL, fetch)
    }

    pub fn with_ttl<F>(ttl: Duration, fetch: F) -> Self
    where
        F: Fn() -> ManifestFuture + Send + Sync + 'static,
    {
        Self {
            fetch: Arc::new(fetch),
            ttl,
            state: Mutex::new(PoolState::default()),
        }
    }

    pub async fn get(&self) -> Result<AntraManifest> {
        {
            let state = self.state.lock().expect("antra pool poisoned");
            if let (Some(cached), Some(fetched_at)) = (&state.cached, state.fetched_at) {
                if fetched_at.elapsed() < self.ttl {
                    return Ok(cached.clone());
                }
            }
        }

        match (self.fetch)().await {
            Ok(manifest) => {
                let mut state = self.state.lock().expect("antra pool poisoned");
                state.cached = Some(manifest.clone());
                state.fetched_at = Some(Instant::now());
                Ok(manifest)
            }
            Err(err) => {
                let state = self.state.lock().expect("antra pool poisoned");
                if let Some(cached) = &state.cached {
                    return Ok(cached.clone());
                }
                Err(err)
            }
        }
    }

    /// Helper that returns a mirror URL for a given service.
    pub async fn mirror_url(&self, service: &str) -> Option<String> {
        let manifest = self.get().await.ok()?;
        manifest.mirror_url(service)
    }

    /// Force the cached entry to appear `age` old. Test/debug helper.
    #[cfg(test)]
    fn prime(&self, manifest: AntraManifest, age: Duration) {
        let mut state = self.state.lock().expect("antra pool poisoned");
        state.cached = Some(manifest);
        state.fetched_at = Some(Instant::now().checked_sub(age).unwrap_or_else(Instant::now));
    }
}

async fn fetch_manifest() -> Result<AntraManifest> {
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(8))
        .build()?;
    let resp = client.get(ANTRA_MANIFEST_URL).send().await?;
    if resp.status() != reqwest::StatusCode::OK {
        return Err(ProviderError::MirrorUnavailable(format!(
            "antra manifest HTTP {}",
            resp.status()
        )));
    }
    let body = resp.bytes().await?;
    AntraManifest::parse(&body)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};

    const SAMPLE: &str = r#"{
        "mirrors": {
            "tidal": "https://tidal.example.com/",
            "qobuz": " https://qobuz.example.com/api ",
            "deezer": "https://deezer.example.com",
            "amazon": "https://amazon.example.com/",
            "apple": "https://apple.example.com"
        },
        "api_key": "  test-key  "
    }"#;

    fn sample_manifest() -> AntraManifest {
        AntraManifest::parse(SAMPLE.as_bytes()).expect("sample parses")
    }

    #[test]
    fn parse_mock_manifest_json() {
        let manifest = sample_manifest();
        assert_eq!(manifest.api_key, "test-key");
        assert_eq!(manifest.mirrors.tidal, "https://tidal.example.com/");
        assert_eq!(manifest.mirrors.apple, "https://apple.example.com");
    }

    #[test]
    fn mirror_url_normalizes_whitespace_and_trailing_slash() {
        let manifest = sample_manifest();
        assert_eq!(
            manifest.mirror_url(" Tidal "),
            Some("https://tidal.example.com".to_string())
        );
        assert_eq!(
            manifest.mirror_url("QOBUZ"),
            Some("https://qobuz.example.com/api".to_string())
        );
        assert_eq!(manifest.mirror_url("spotify"), None);
    }

    #[tokio::test]
    async fn cache_hit_within_ttl_skips_fetch() {
        let calls = Arc::new(AtomicUsize::new(0));
        let counter = calls.clone();
        let pool = AntraPool::with_fetcher(move || {
            counter.fetch_add(1, Ordering::SeqCst);
            Box::pin(async { Ok(sample_manifest()) })
        });
        pool.prime(sample_manifest(), Duration::from_secs(60));
        let got = pool.get().await.expect("cached manifest");
        assert_eq!(got.api_key, "test-key");
        assert_eq!(calls.load(Ordering::SeqCst), 0);
    }

    #[tokio::test]
    async fn expired_cache_triggers_refresh() {
        let calls = Arc::new(AtomicUsize::new(0));
        let counter = calls.clone();
        let pool = AntraPool::with_fetcher(move || {
            counter.fetch_add(1, Ordering::SeqCst);
            Box::pin(async {
                Ok(AntraManifest::parse(
                    br#"{"mirrors":{"tidal":"https://new.example.com","qobuz":"","deezer":"","amazon":"","apple":""},"api_key":"new"}"#,
                )
                .expect("fresh parses"))
            })
        });
        pool.prime(
            sample_manifest(),
            ANTRA_MANIFEST_TTL + Duration::from_secs(1),
        );
        let got = pool.get().await.expect("refreshed manifest");
        assert_eq!(got.api_key, "new");
        assert_eq!(calls.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn refresh_failure_falls_back_to_stale_cache() {
        let pool = AntraPool::with_fetcher(|| {
            Box::pin(async { Err(ProviderError::Other("down".into())) })
        });
        pool.prime(
            sample_manifest(),
            ANTRA_MANIFEST_TTL + Duration::from_secs(5),
        );
        let got = pool.get().await.expect("stale fallback");
        assert_eq!(got.api_key, "test-key");
    }

    #[tokio::test]
    async fn refresh_failure_without_cache_errors() {
        let pool = AntraPool::with_fetcher(|| {
            Box::pin(async { Err(ProviderError::Other("down".into())) })
        });
        let err = pool.get().await.expect_err("no cache");
        assert!(err.to_string().contains("down"));
    }

    #[tokio::test]
    async fn pool_mirror_url_helper() {
        let pool = AntraPool::with_fetcher(|| Box::pin(async { Ok(sample_manifest()) }));
        assert_eq!(
            pool.mirror_url("tidal").await,
            Some("https://tidal.example.com".to_string())
        );
        assert_eq!(pool.mirror_url("unknown").await, None);
    }
}

//! SongLink / Odesli metadata client and scrapers (port of `backend/songlink.go`).

use std::future::Future;
use std::pin::Pin;
use std::sync::{Arc, LazyLock};
use std::time::Duration;

use auralis_core::download_cancel::{CancellationToken, DownloadCancelled};
use regex::Regex;
use serde::{Deserialize, Serialize};

use crate::bounded_body::read_bounded_response;
use crate::error::{ProviderError, Result};
use crate::zarz::{normalize_amazon_music_url, normalize_deezer_track_url, ResolvedTrackLinks};

pub const SONGLINK_USER_AGENT: &str = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36";

static ISRC_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\b([A-Z]{2}[A-Z0-9]{3}\d{7})\b").unwrap());

static SONGLINK_NEXT_DATA_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r#"(?is)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>"#).unwrap()
});

static QOBUZ_TRACK_URL_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"/(?:track|tracks)/([0-9]+)").unwrap());

static SPOTIFY_TRACK_URL_PATTERN: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(?i)(?:https?://)?open\.spotify\.com(?:/(?:intl-[a-z]{2}|[a-z]{2}(?:-[a-z]{2})?))?/track/([a-zA-Z0-9]{22})",
    )
    .unwrap()
});

static SPOTIFY_TRACK_URI_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?i)spotify:track:([a-zA-Z0-9]{22})").unwrap());

static SPOTIFY_TRACK_ID_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[a-zA-Z0-9]{22}$").unwrap());

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct SongLinkURLs {
    #[serde(default, rename = "tidal_url")]
    pub tidal_url: String,
    #[serde(default, rename = "amazon_url")]
    pub amazon_url: String,
    #[serde(default, rename = "isrc")]
    pub isrc: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct TrackAvailability {
    #[serde(rename = "spotify_id")]
    pub spotify_id: String,
    pub tidal: bool,
    pub amazon: bool,
    pub qobuz: bool,
    pub deezer: bool,
    #[serde(
        rename = "tidal_url",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub tidal_url: String,
    #[serde(
        rename = "amazon_url",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub amazon_url: String,
    #[serde(
        rename = "qobuz_url",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub qobuz_url: String,
    #[serde(
        rename = "deezer_url",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub deezer_url: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct SongLinkScrapeResult {
    pub isrc: String,
    pub tidal_url: String,
    pub amazon_url: String,
    pub deezer_url: String,
    pub qobuz_url: String,
}

#[derive(Debug, Deserialize)]
struct SongLinkNextData {
    props: SongLinkProps,
}

#[derive(Debug, Deserialize)]
struct SongLinkProps {
    #[serde(rename = "pageProps")]
    page_props: SongLinkPageProps,
}

#[derive(Debug, Deserialize)]
struct SongLinkPageProps {
    #[serde(rename = "pageData")]
    page_data: SongLinkPageData,
}

#[derive(Debug, Deserialize)]
struct SongLinkPageData {
    #[serde(rename = "entityData")]
    entity_data: SongLinkEntityData,
    #[serde(default)]
    sections: Vec<SongLinkSection>,
}

#[derive(Debug, Deserialize)]
struct SongLinkEntityData {
    #[serde(default)]
    isrc: String,
}

#[derive(Debug, Deserialize)]
struct SongLinkSection {
    #[serde(default)]
    links: Vec<SongLinkSectionLink>,
}

#[derive(Debug, Deserialize)]
struct SongLinkSectionLink {
    #[serde(default)]
    platform: String,
    #[serde(default)]
    url: String,
}

pub type AsyncLookupFn =
    Arc<dyn Fn(&str) -> Pin<Box<dyn Future<Output = Result<String>> + Send>> + Send + Sync>;
pub type AsyncAvailabilityFn =
    Arc<dyn Fn(&str) -> Pin<Box<dyn Future<Output = (bool, String)> + Send>> + Send + Sync>;
pub type RecordOutcomeFn =
    Arc<dyn Fn(&str, &str, &str, Duration, bool, Option<&str>) + Send + Sync>;
pub type RankSourcesFn = Arc<dyn Fn(&[&str], &str) -> Vec<String> + Send + Sync>;
pub type AsyncZarzResolveFn = Arc<
    dyn Fn(&str) -> Pin<Box<dyn Future<Output = Result<ResolvedTrackLinks>> + Send>> + Send + Sync,
>;

/// Sends an HTTP request with cancellation support.
pub(crate) async fn send_request(
    req: reqwest::RequestBuilder,
    cancel_token: Option<&CancellationToken>,
) -> Result<reqwest::Response> {
    if let Some(token) = cancel_token {
        if token.is_cancelled() {
            return Err(ProviderError::Cancelled(DownloadCancelled));
        }
        tokio::select! {
            biased;
            _ = token.cancelled() => Err(ProviderError::Cancelled(DownloadCancelled)),
            res = req.send() => res.map_err(|e| ProviderError::Other(e.to_string())),
        }
    } else {
        req.send()
            .await
            .map_err(|e| ProviderError::Other(e.to_string()))
    }
}

/// Helper that preserves cancellation errors when formatting error prefixes.
pub(crate) fn map_http_error(err: ProviderError, prefix: &str) -> ProviderError {
    match err {
        ProviderError::Cancelled(c) => ProviderError::Cancelled(c),
        other => ProviderError::Other(format!("{prefix}: {other}")),
    }
}

#[derive(Clone)]
pub struct SongLinkClient {
    pub client: reqwest::Client,
    pub cancel_token: Option<CancellationToken>,
    pub spotify_isrc_lookup: Option<AsyncLookupFn>,
    pub tidal_isrc_lookup: Option<AsyncLookupFn>,
    pub qobuz_availability_checker: Option<AsyncAvailabilityFn>,
    pub record_source_outcome: Option<RecordOutcomeFn>,
    pub ranked_source_ids: Option<RankSourcesFn>,
    pub songlink_base_url: Option<String>,
    pub songstats_base_url: Option<String>,
    pub deezer_base_url: Option<String>,
    pub zarz_resolve: Option<AsyncZarzResolveFn>,
}

impl Default for SongLinkClient {
    fn default() -> Self {
        Self::new()
    }
}

impl SongLinkClient {
    /// Creates a new SongLink client matching Go's `NewSongLinkClient()`.
    pub fn new() -> Self {
        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(30))
            .build()
            .unwrap_or_else(|_| reqwest::Client::new());
        Self {
            client,
            cancel_token: None,
            spotify_isrc_lookup: None,
            tidal_isrc_lookup: None,
            qobuz_availability_checker: None,
            record_source_outcome: None,
            ranked_source_ids: None,
            songlink_base_url: None,
            songstats_base_url: None,
            deezer_base_url: None,
            zarz_resolve: None,
        }
    }

    /// Creates a new SongLink client bound to an explicit cancellation token.
    pub fn with_context(cancel_token: CancellationToken) -> Self {
        Self {
            cancel_token: Some(cancel_token),
            ..Self::new()
        }
    }

    /// Sets a custom HTTP client.
    pub fn with_http_client(mut self, client: reqwest::Client) -> Self {
        self.client = client;
        self
    }

    /// Attaches an external Spotify ISRC lookup function (unported `backend/isrc_finder.go`).
    pub fn with_spotify_isrc_lookup<F, Fut>(mut self, lookup: F) -> Self
    where
        F: Fn(&str) -> Fut + Send + Sync + 'static,
        Fut: Future<Output = Result<String>> + Send + 'static,
    {
        self.spotify_isrc_lookup = Some(Arc::new(move |id| Box::pin(lookup(id))));
        self
    }

    /// Attaches an external Tidal ISRC lookup function (unported `backend/tidal.go`).
    pub fn with_tidal_isrc_lookup<F, Fut>(mut self, lookup: F) -> Self
    where
        F: Fn(&str) -> Fut + Send + Sync + 'static,
        Fut: Future<Output = Result<String>> + Send + 'static,
    {
        self.tidal_isrc_lookup = Some(Arc::new(move |isrc| Box::pin(lookup(isrc))));
        self
    }

    /// Attaches an external Qobuz availability checker (unported `backend/qobuz.go`).
    pub fn with_qobuz_availability_checker<F, Fut>(mut self, checker: F) -> Self
    where
        F: Fn(&str) -> Fut + Send + Sync + 'static,
        Fut: Future<Output = (bool, String)> + Send + 'static,
    {
        self.qobuz_availability_checker = Some(Arc::new(move |isrc| Box::pin(checker(isrc))));
        self
    }

    /// Attaches an external source benchmark recorder (unported `backend/source_priority.go`).
    pub fn with_record_source_outcome<F>(mut self, recorder: F) -> Self
    where
        F: Fn(&str, &str, &str, Duration, bool, Option<&str>) + Send + Sync + 'static,
    {
        self.record_source_outcome = Some(Arc::new(recorder));
        self
    }

    /// Attaches an external source ranker function (unported `backend/source_priority.go`).
    pub fn with_ranked_source_ids<F>(mut self, ranker: F) -> Self
    where
        F: Fn(&[&str], &str) -> Vec<String> + Send + Sync + 'static,
    {
        self.ranked_source_ids = Some(Arc::new(ranker));
        self
    }

    /// Configures custom base URL for song.link scraping (for testing).
    pub fn with_songlink_base_url(mut self, base: impl Into<String>) -> Self {
        self.songlink_base_url = Some(base.into());
        self
    }

    /// Configures custom base URL for songstats (for testing).
    pub fn with_songstats_base_url(mut self, base: impl Into<String>) -> Self {
        self.songstats_base_url = Some(base.into());
        self
    }

    /// Configures custom base URL for Deezer API (for testing).
    pub fn with_deezer_base_url(mut self, base: impl Into<String>) -> Self {
        self.deezer_base_url = Some(base.into());
        self
    }

    /// Attaches an external Zarz resolve hook (for testing or injection).
    pub fn with_zarz_resolve<F, Fut>(mut self, resolve: F) -> Self
    where
        F: Fn(&str) -> Fut + Send + Sync + 'static,
        Fut: Future<Output = Result<ResolvedTrackLinks>> + Send + 'static,
    {
        self.zarz_resolve = Some(Arc::new(move |id| Box::pin(resolve(id))));
        self
    }

    /// Scrapes a song.link / odesli page for entity ISRC and streaming links.
    pub async fn scrape_song_link_page(
        &self,
        page_url: &str,
        region: &str,
    ) -> Result<SongLinkScrapeResult> {
        let mut target_url = page_url.to_string();
        if let Some(base) = &self.songlink_base_url {
            if target_url.starts_with("https://song.link") {
                target_url = format!("{}{}", base, &target_url["https://song.link".len()..]);
            }
        }

        if !region.is_empty() {
            let sep = if target_url.contains('?') { '&' } else { '?' };
            let query = url::form_urlencoded::Serializer::new(String::new())
                .append_pair("country", region)
                .finish();
            target_url.push(sep);
            target_url.push_str(&query);
        }

        let req = self
            .client
            .get(&target_url)
            .header("User-Agent", SONGLINK_USER_AGENT)
            .header(
                "Accept",
                "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
            )
            .header("Accept-Language", "en-US,en;q=0.9,id;q=0.8")
            .header("Upgrade-Insecure-Requests", "1")
            .header("Sec-Fetch-Dest", "document")
            .header("Sec-Fetch-Mode", "navigate")
            .header("Sec-Fetch-Site", "none")
            .header("Sec-Fetch-User", "?1");

        let resp = send_request(req, self.cancel_token.as_ref())
            .await
            .map_err(|e| map_http_error(e, "failed to call song.link"))?;

        let status = resp.status();
        if status != reqwest::StatusCode::OK {
            let body_preview = read_bounded_response(resp, 256).await.unwrap_or_default();
            let preview_str = String::from_utf8_lossy(&body_preview).trim().to_string();
            return Err(ProviderError::Other(format!(
                "song.link returned status {} ({})",
                status.as_u16(),
                preview_str
            )));
        }

        let body = read_bounded_response(resp, 8 << 20).await.map_err(|err| {
            ProviderError::Other(format!("failed to read song.link response: {err}"))
        })?;

        let body_str = String::from_utf8_lossy(&body);
        let caps = SONGLINK_NEXT_DATA_REGEX
            .captures(&body_str)
            .ok_or_else(|| ProviderError::Other("song.link __NEXT_DATA__ not found".to_string()))?;

        let json_str = &caps[1];
        let parsed: SongLinkNextData = serde_json::from_str(json_str).map_err(|err| {
            ProviderError::Other(format!("failed to decode song.link __NEXT_DATA__: {err}"))
        })?;

        let page_data = parsed.props.page_props.page_data;
        let mut result = SongLinkScrapeResult {
            isrc: page_data.entity_data.isrc.trim().to_uppercase(),
            ..SongLinkScrapeResult::default()
        };

        for section in page_data.sections {
            for link in section.links {
                let raw_url = link.url.trim();
                if raw_url.is_empty() {
                    continue;
                }
                match link.platform.as_str() {
                    "tidal" if result.tidal_url.is_empty() => {
                        result.tidal_url = raw_url.to_string();
                    }
                    "amazonMusic" if result.amazon_url.is_empty() => {
                        result.amazon_url = raw_url.to_string();
                    }
                    "deezer" if result.deezer_url.is_empty() => {
                        result.deezer_url = raw_url.to_string();
                    }
                    "qobuz" if result.qobuz_url.is_empty() => {
                        result.qobuz_url = raw_url.to_string();
                    }
                    _ => {}
                }
            }
        }

        Ok(result)
    }

    /// Queries Deezer public API by track ISRC and returns normalized track URL.
    pub async fn lookup_deezer_track_url_by_isrc(&self, isrc: &str) -> Result<String> {
        let base = self
            .deezer_base_url
            .as_deref()
            .unwrap_or("https://api.deezer.com");
        let api_url = format!("{base}/track/isrc:{}", isrc.trim().to_uppercase());

        let req = self
            .client
            .get(&api_url)
            .header("User-Agent", SONGLINK_USER_AGENT);

        let resp = send_request(req, self.cancel_token.as_ref())
            .await
            .map_err(|e| map_http_error(e, "failed to call Deezer ISRC API"))?;

        if resp.status() != reqwest::StatusCode::OK {
            return Err(ProviderError::Other(format!(
                "Deezer ISRC API returned status {}",
                resp.status().as_u16()
            )));
        }

        #[derive(Deserialize)]
        struct DeezerIsrcPayload {
            #[serde(default)]
            id: i64,
            #[serde(default)]
            link: String,
        }

        let body = resp.bytes().await.map_err(|e| {
            ProviderError::Other(format!("failed to read Deezer ISRC response: {e}"))
        })?;

        let payload: DeezerIsrcPayload = serde_json::from_slice(&body).map_err(|e| {
            ProviderError::Other(format!("failed to decode Deezer ISRC response: {e}"))
        })?;

        if !payload.link.is_empty() {
            return Ok(normalize_deezer_track_url(&payload.link));
        }
        if payload.id > 0 {
            return Ok(normalize_deezer_track_url(&format!(
                "https://www.deezer.com/track/{}",
                payload.id
            )));
        }

        Err(ProviderError::Other(format!(
            "deezer track link not found for ISRC {isrc}"
        )))
    }

    /// Invokes the Spotify ISRC lookup hook or returns an error indicating it needs to be configured.
    pub async fn lookup_spotify_isrc(&self, spotify_track_id: &str) -> Result<String> {
        if let Some(lookup) = &self.spotify_isrc_lookup {
            lookup(spotify_track_id).await
        } else {
            Err(ProviderError::Other(
                "Spotify ISRC lookup not available (backend/isrc_finder.go unported)".to_string(),
            ))
        }
    }

    /// Invokes the Tidal ISRC lookup hook or returns an error indicating it needs to be configured.
    pub async fn lookup_tidal_url_by_isrc(&self, isrc: &str) -> Result<String> {
        if let Some(lookup) = &self.tidal_isrc_lookup {
            lookup(isrc).await
        } else {
            Err(ProviderError::Other(
                "Tidal ISRC lookup not available (backend/tidal.go unported)".to_string(),
            ))
        }
    }

    /// Invokes the Qobuz availability check hook.
    pub async fn check_qobuz_availability(&self, isrc: &str) -> (bool, String) {
        if let Some(checker) = &self.qobuz_availability_checker {
            checker(isrc).await
        } else {
            (false, String::new())
        }
    }

    /// Fetches all streaming URLs from Spotify track ID using configured resolvers.
    pub async fn get_all_urls_from_spotify(
        &self,
        spotify_track_id: &str,
        region: &str,
    ) -> Result<SongLinkURLs> {
        let (links, err) = self
            .resolve_spotify_track_links(spotify_track_id, region)
            .await;
        if links.tidal_url.is_empty() && links.amazon_url.is_empty() {
            if let Some(e) = err {
                return Err(e);
            }
        }

        let mut urls = SongLinkURLs {
            tidal_url: links.tidal_url,
            amazon_url: normalize_amazon_music_url(&links.amazon_url),
            isrc: links.isrc,
        };

        if urls.tidal_url.is_empty() && !urls.isrc.is_empty() {
            if let Ok(tidal_url) = self.lookup_tidal_url_by_isrc(&urls.isrc).await {
                urls.tidal_url = tidal_url.clone();
                println!("Found Tidal URL via ISRC search: {tidal_url}");
            }
        }

        if urls.tidal_url.is_empty() && urls.amazon_url.is_empty() {
            if let Some(e) = err {
                return Err(e);
            }
            return Err(ProviderError::Other("no streaming URLs found".to_string()));
        }

        Ok(urls)
    }

    /// Checks availability across Tidal, Amazon, Qobuz, and Deezer for a Spotify track.
    pub async fn check_track_availability(
        &self,
        spotify_track_id: &str,
    ) -> Result<TrackAvailability> {
        let (links, mut err) = self.resolve_spotify_track_links(spotify_track_id, "").await;

        let mut availability = TrackAvailability {
            spotify_id: spotify_track_id.to_string(),
            ..TrackAvailability::default()
        };

        availability.tidal_url = links.tidal_url;
        availability.amazon_url = normalize_amazon_music_url(&links.amazon_url);
        availability.deezer_url = normalize_deezer_track_url(&links.deezer_url);
        availability.tidal = !availability.tidal_url.is_empty();
        availability.amazon = !availability.amazon_url.is_empty();
        availability.deezer = !availability.deezer_url.is_empty();

        if let Ok(id) = parse_qobuz_track_id(&links.qobuz_url) {
            availability.qobuz = true;
            availability.qobuz_url = format!("https://open.qobuz.com/track/{id}");
        } else if !links.qobuz_url.trim().is_empty() {
            availability.qobuz = true;
            availability.qobuz_url = links.qobuz_url;
        }

        let mut isrc = links.isrc.trim().to_string();

        if isrc.is_empty() && !availability.deezer_url.is_empty() {
            if let Ok(resolved_isrc) = self.get_deezer_isrc(&availability.deezer_url).await {
                isrc = resolved_isrc;
            }
        }

        if isrc.is_empty() {
            match self.lookup_spotify_isrc(spotify_track_id).await {
                Ok(fallback_isrc) => isrc = fallback_isrc,
                Err(fallback_err) => {
                    if err.is_none() {
                        err = Some(fallback_err);
                    }
                }
            }
        }

        if !isrc.is_empty() && !availability.qobuz {
            let (has_qobuz, qobuz_url) = self.check_qobuz_availability(&isrc).await;
            availability.qobuz = has_qobuz;
            availability.qobuz_url = qobuz_url;
        }

        if availability.tidal || availability.amazon || availability.deezer || availability.qobuz {
            return Ok(availability);
        }

        if let Some(err) = err {
            return Err(err);
        }

        Err(ProviderError::Other("no platforms found".to_string()))
    }

    /// Resolves Deezer track URL for a Spotify track.
    pub async fn get_deezer_url_from_spotify(&self, spotify_track_id: &str) -> Result<String> {
        let (links, mut err) = self.resolve_spotify_track_links(spotify_track_id, "").await;

        if !links.deezer_url.is_empty() {
            let deezer_url = normalize_deezer_track_url(&links.deezer_url);
            println!("Found Deezer URL: {deezer_url}");
            return Ok(deezer_url);
        }

        let mut isrc = links.isrc.trim().to_string();

        if isrc.is_empty() {
            match self.lookup_spotify_isrc(spotify_track_id).await {
                Ok(fallback_isrc) => isrc = fallback_isrc,
                Err(lookup_err) => {
                    if err.is_none() {
                        err = Some(lookup_err);
                    }
                }
            }
        }

        if !isrc.is_empty() {
            match self.lookup_deezer_track_url_by_isrc(&isrc).await {
                Ok(deezer_url) => {
                    println!("Found Deezer URL: {deezer_url}");
                    return Ok(deezer_url);
                }
                Err(deezer_err) => {
                    if err.is_none() {
                        err = Some(deezer_err);
                    }
                }
            }
        }

        if let Some(err) = err {
            return Err(err);
        }
        Err(ProviderError::Other("deezer link not found".to_string()))
    }

    /// Fetches ISRC from Spotify track ID using resolvers, Deezer fallback, or Spotify direct.
    pub async fn get_isrc(&self, spotify_id: &str) -> Result<String> {
        let (links, err) = self.resolve_spotify_track_links(spotify_id, "").await;

        if !links.isrc.is_empty() {
            return Ok(links.isrc);
        }

        if !links.deezer_url.is_empty() {
            if let Ok(isrc) = self.get_deezer_isrc(&links.deezer_url).await {
                return Ok(isrc);
            }
        }

        let spotify_res = self.lookup_spotify_isrc(spotify_id).await;
        if let Ok(ref isrc) = spotify_res {
            if !isrc.is_empty() {
                return Ok(isrc.clone());
            }
        }

        match (err, spotify_res) {
            (Some(e), Err(lookup_err)) => Err(ProviderError::Other(format!("{e} | {lookup_err}"))),
            (Some(e), _) => Err(e),
            (_, Err(lookup_err)) => Err(lookup_err),
            _ => Err(ProviderError::Other("ISRC not found".to_string())),
        }
    }

    /// Directly calls the Spotify ISRC lookup without resolvers.
    pub async fn get_isrc_direct(&self, spotify_id: &str) -> Result<String> {
        self.lookup_spotify_isrc(spotify_id).await
    }

    /// Resolves ISRC from a Deezer track URL using Deezer's public API.
    pub async fn get_deezer_isrc(&self, deezer_url: &str) -> Result<String> {
        get_deezer_isrc_with_client(
            &self.client,
            deezer_url,
            self.cancel_token.as_ref(),
            self.deezer_base_url.as_deref(),
        )
        .await
    }
}

/// Resolves ISRC from a Deezer track URL using the provided HTTP client.
pub async fn get_deezer_isrc_with_client(
    client: &reqwest::Client,
    deezer_url: &str,
    cancel_token: Option<&CancellationToken>,
    base_url: Option<&str>,
) -> Result<String> {
    let track_id = extract_deezer_track_id(deezer_url)?;
    let base = base_url.unwrap_or("https://api.deezer.com");
    let api_url = format!("{base}/track/{track_id}");

    let req = client.get(&api_url);
    let resp = send_request(req, cancel_token)
        .await
        .map_err(|e| map_http_error(e, "failed to call Deezer API"))?;

    if resp.status() != reqwest::StatusCode::OK {
        return Err(ProviderError::Other(format!(
            "Deezer API returned status {}",
            resp.status().as_u16()
        )));
    }

    #[derive(Deserialize)]
    struct DeezerTrackPayload {
        #[serde(default)]
        isrc: String,
        #[serde(default)]
        title: String,
    }

    let body = resp
        .bytes()
        .await
        .map_err(|e| ProviderError::Other(format!("failed to read Deezer API response: {e}")))?;

    let deezer_track: DeezerTrackPayload = serde_json::from_slice(&body)
        .map_err(|e| ProviderError::Other(format!("failed to decode Deezer API response: {e}")))?;

    if deezer_track.isrc.is_empty() {
        return Err(ProviderError::Other(format!(
            "ISRC not found in Deezer API response for track {track_id}"
        )));
    }

    println!(
        "Found ISRC from Deezer: {} (track: {})",
        deezer_track.isrc, deezer_track.title
    );
    Ok(deezer_track.isrc.trim().to_uppercase())
}

/// Extracts Deezer track ID from a Deezer URL.
pub fn extract_deezer_track_id(raw_url: &str) -> Result<String> {
    let clean = raw_url.trim();
    if clean.is_empty() {
        return Err(ProviderError::Other("empty Deezer URL".to_string()));
    }
    let parts: Vec<&str> = clean.split("/track/").collect();
    if parts.len() < 2 {
        return Err(ProviderError::Other(format!(
            "could not extract track ID from Deezer URL: {raw_url}"
        )));
    }
    let track_id = parts[1]
        .split('?')
        .next()
        .unwrap_or("")
        .trim_matches(|c| c == '/' || c == ' ');
    if track_id.is_empty() {
        return Err(ProviderError::Other(format!(
            "could not extract track ID from Deezer URL: {raw_url}"
        )));
    }
    Ok(track_id.to_string())
}

/// Extracts Spotify track ID from track ID, URI, or URL (port of Go's `extractSpotifyTrackID`).
pub fn extract_spotify_track_id(input: &str) -> Result<String> {
    let input = input.trim();
    if input.is_empty() {
        return Err(ProviderError::Other("empty Spotify track ID".to_string()));
    }

    if let Some(caps) = SPOTIFY_TRACK_URL_PATTERN.captures(input) {
        if let Some(m) = caps.get(1) {
            return Ok(m.as_str().to_string());
        }
    }

    if let Some(caps) = SPOTIFY_TRACK_URI_PATTERN.captures(input) {
        if let Some(m) = caps.get(1) {
            return Ok(m.as_str().to_string());
        }
    }

    if SPOTIFY_TRACK_ID_PATTERN.is_match(input) {
        return Ok(input.to_string());
    }

    Err(ProviderError::Other(format!(
        "invalid Spotify track ID: {input}"
    )))
}

/// Parses numeric track ID from a Qobuz track URL.
pub fn parse_qobuz_track_id(raw_url: &str) -> Result<i64> {
    let raw = raw_url.trim();
    if raw.is_empty() {
        return Err(ProviderError::Other("empty Qobuz URL".to_string()));
    }
    let caps = QOBUZ_TRACK_URL_PATTERN.captures(raw).ok_or_else(|| {
        ProviderError::Other(format!("could not extract track ID from Qobuz URL: {raw}"))
    })?;
    let id_str = &caps[1];
    let id: i64 = id_str
        .parse()
        .map_err(|_| ProviderError::Other(format!("invalid Qobuz track ID: {id_str}")))?;
    if id <= 0 {
        return Err(ProviderError::Other(format!(
            "invalid Qobuz track ID: {id_str}"
        )));
    }
    Ok(id)
}

/// Normalizes relative Qobuz URL into an absolute URL.
pub fn qobuz_normalize_relative_url(raw_url: &str) -> String {
    let raw_url = raw_url.trim();
    if raw_url.is_empty() {
        return String::new();
    }
    if raw_url.starts_with("http://") || raw_url.starts_with("https://") {
        return raw_url.to_string();
    }
    if raw_url.starts_with('/') {
        return format!("https://www.qobuz.com{raw_url}");
    }
    format!("https://www.qobuz.com/{raw_url}")
}

/// Slugifies a segment for Qobuz URL generation.
pub fn qobuz_slugify_segment(value: &str) -> String {
    let value = value.trim().to_lowercase();
    if value.is_empty() {
        return String::new();
    }
    let re_chars = Regex::new(r"[^a-z0-9]+").unwrap();
    let replaced = re_chars.replace_all(&value, "-");
    let trimmed = replaced.trim_matches('-');
    let re_dashes = Regex::new(r"-+").unwrap();
    re_dashes.replace_all(trimmed, "-").to_string()
}

/// Merges scraped streaming URLs into `links`.
pub fn merge_song_link_scrape(links: &mut ResolvedTrackLinks, data: &SongLinkScrapeResult) {
    if !data.tidal_url.is_empty() && links.tidal_url.is_empty() {
        links.tidal_url = data.tidal_url.clone();
        println!("Tidal URL found");
    }

    if !data.amazon_url.is_empty() && links.amazon_url.is_empty() {
        let normalized = normalize_amazon_music_url(&data.amazon_url);
        if !normalized.is_empty() {
            links.amazon_url = normalized;
            println!("Amazon URL found");
        }
    }

    if !data.deezer_url.is_empty() && links.deezer_url.is_empty() {
        links.deezer_url = normalize_deezer_track_url(&data.deezer_url);
        println!("Deezer URL found");
    }

    if !data.qobuz_url.is_empty() && links.qobuz_url.is_empty() {
        links.qobuz_url = data.qobuz_url.clone();
        println!("Qobuz URL found");
    }

    if links.isrc.is_empty() && !data.isrc.is_empty() {
        links.isrc = data.isrc.clone();
    }
}

/// Checks if any platform link is present on `links`.
pub fn has_any_song_link_data(links: &ResolvedTrackLinks) -> bool {
    !links.tidal_url.is_empty()
        || !links.amazon_url.is_empty()
        || !links.deezer_url.is_empty()
        || !links.qobuz_url.is_empty()
}

/// Extracts the first ISRC matching pattern from a response body.
pub fn first_isrc_match(body: &str) -> String {
    let upper = body.to_uppercase();
    if let Some(caps) = ISRC_PATTERN.captures(&upper) {
        if let Some(m) = caps.get(1) {
            return m.as_str().trim().to_string();
        }
    }
    String::new()
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};

    #[test]
    fn test_extract_spotify_track_id() {
        assert_eq!(
            extract_spotify_track_id("2EqlS6tkEnglzr7tkKAAYD").unwrap(),
            "2EqlS6tkEnglzr7tkKAAYD"
        );
        assert_eq!(
            extract_spotify_track_id("spotify:track:2EqlS6tkEnglzr7tkKAAYD").unwrap(),
            "2EqlS6tkEnglzr7tkKAAYD"
        );
        assert_eq!(
            extract_spotify_track_id(
                "https://open.spotify.com/intl-de/track/2EqlS6tkEnglzr7tkKAAYD?si=abc"
            )
            .unwrap(),
            "2EqlS6tkEnglzr7tkKAAYD"
        );
        assert!(extract_spotify_track_id("").is_err());
        assert!(extract_spotify_track_id("not-a-track").is_err());
    }

    #[test]
    fn test_extract_deezer_track_id() {
        assert_eq!(
            extract_deezer_track_id("https://www.deezer.com/track/12345678").unwrap(),
            "12345678"
        );
        assert_eq!(
            extract_deezer_track_id("https://www.deezer.com/us/track/12345678?utm_source=test")
                .unwrap(),
            "12345678"
        );
        assert!(extract_deezer_track_id("https://www.deezer.com/artist/123").is_err());
        assert!(extract_deezer_track_id("").is_err());
    }

    #[test]
    fn test_parse_qobuz_track_id() {
        assert_eq!(
            parse_qobuz_track_id("https://open.qobuz.com/track/123456").unwrap(),
            123456
        );
        assert_eq!(
            parse_qobuz_track_id("https://play.qobuz.com/tracks/987654").unwrap(),
            987654
        );
        assert!(parse_qobuz_track_id("https://open.qobuz.com/album/123").is_err());
        assert!(parse_qobuz_track_id("").is_err());
    }

    #[test]
    fn test_qobuz_slugify_segment() {
        assert_eq!(qobuz_slugify_segment("Hello World!"), "hello-world");
        assert_eq!(qobuz_slugify_segment("Song (Remix)"), "song-remix");
        assert_eq!(qobuz_slugify_segment("---A---B---"), "a-b");
    }

    #[test]
    fn test_qobuz_normalize_relative_url() {
        assert_eq!(
            qobuz_normalize_relative_url("/album/test/123"),
            "https://www.qobuz.com/album/test/123"
        );
        assert_eq!(
            qobuz_normalize_relative_url("album/test/123"),
            "https://www.qobuz.com/album/test/123"
        );
        assert_eq!(
            qobuz_normalize_relative_url("https://www.qobuz.com/album/123"),
            "https://www.qobuz.com/album/123"
        );
    }

    #[test]
    fn test_first_isrc_match() {
        let text = "Sample response with ISRC USRC17607839 embedded in it.";
        assert_eq!(first_isrc_match(text), "USRC17607839");

        let lowercase = "embedded isrc usrc17607839 here.";
        assert_eq!(first_isrc_match(lowercase), "USRC17607839");

        assert_eq!(first_isrc_match("no isrc here"), "");
    }

    #[test]
    fn test_has_any_song_link_data() {
        let mut links = ResolvedTrackLinks::default();
        assert!(!has_any_song_link_data(&links));

        links.tidal_url = "https://listen.tidal.com/track/1".to_string();
        assert!(has_any_song_link_data(&links));
    }

    #[tokio::test]
    async fn test_catalog_resolver_survives_download_stop() {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let server_url = format!("http://127.0.0.1:{}", addr.port());

        let server_task = tokio::spawn(async move {
            if let Ok((mut socket, _)) = listener.accept().await {
                let mut buf = [0u8; 1024];
                let _ = socket.read(&mut buf).await;
                let body = r#"<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"pageData":{"entityData":{"isrc":"USABC2600001"}}}}}</script>"#;
                let response = format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                    body.len()
                );
                let _ = socket.write_all(response.as_bytes()).await;
            }
        });

        let mut scope = auralis_core::download_cancel::begin_download_cancellation_scope();
        auralis_core::download_cancel::force_stop_active_downloads();
        scope.release();

        let client = SongLinkClient::new();
        let result = client
            .scrape_song_link_page(&server_url, "")
            .await
            .expect("catalog resolver should survive force stop");
        assert_eq!(result.isrc, "USABC2600001");

        let _ = server_task.await;
    }

    #[tokio::test]
    async fn test_download_resolver_honors_explicit_cancellation() {
        let token = CancellationToken::new();
        token.cancel();

        let client = SongLinkClient::with_context(token);
        let err = client
            .scrape_song_link_page("http://127.0.0.1:1", "")
            .await
            .expect_err("should return cancelled error");

        match err {
            ProviderError::Cancelled(_) => {}
            other => panic!("expected ProviderError::Cancelled, got: {other:?}"),
        }
    }

    #[tokio::test]
    async fn test_scrape_song_link_page_from_fixture() {
        let fixture_html = include_str!("../tests/fixtures/songlink/songlink_page.html");

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let server_url = format!("http://127.0.0.1:{}", addr.port());

        let fixture_body = fixture_html.to_string();
        let server_task = tokio::spawn(async move {
            if let Ok((mut socket, _)) = listener.accept().await {
                let mut buf = [0u8; 1024];
                let _ = socket.read(&mut buf).await;
                let response = format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{fixture_body}",
                    fixture_body.len()
                );
                let _ = socket.write_all(response.as_bytes()).await;
            }
        });

        let client = SongLinkClient::new();
        let result = client
            .scrape_song_link_page(&server_url, "US")
            .await
            .expect("should scrape fixture page");

        assert_eq!(result.isrc, "USRC17607839");
        assert_eq!(result.tidal_url, "https://listen.tidal.com/track/12345678");
        assert_eq!(
            result.amazon_url,
            "https://music.amazon.com/albums/B000EXAMPLE?trackAsin=B098765432"
        );
        assert_eq!(result.deezer_url, "https://www.deezer.com/track/11223344");
        assert_eq!(result.qobuz_url, "https://open.qobuz.com/track/55667788");

        let _ = server_task.await;
    }
}

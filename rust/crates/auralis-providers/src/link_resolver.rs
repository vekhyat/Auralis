//! Streaming link resolution across Zarz, Songstats, and SongLink (port of `backend/link_resolver.go`).

use std::time::Instant;

use crate::error::ProviderError;
use crate::songlink::{
    extract_deezer_track_id, extract_spotify_track_id, has_any_song_link_data,
    merge_song_link_scrape, SongLinkClient,
};
use crate::zarz::{ResolvedTrackLinks, ZarzResolver};

pub const LINK_RESOLVER_PROVIDER_SONGSTATS: &str =
    auralis_config::settings::LINK_RESOLVER_PROVIDER_SONGSTATS;
pub const LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK: &str =
    auralis_config::settings::LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK;

pub type RankedSourceIdsFn<'a> = &'a dyn Fn(&[&str], &str) -> Vec<String>;

/// Merges non-empty fields from `src` into `dst` where `dst` is empty.
pub fn merge_resolved_track_links(dst: &mut ResolvedTrackLinks, src: &ResolvedTrackLinks) {
    if dst.tidal_url.is_empty() {
        dst.tidal_url = src.tidal_url.clone();
    }
    if dst.amazon_url.is_empty() {
        dst.amazon_url = src.amazon_url.clone();
    }
    if dst.qobuz_url.is_empty() {
        dst.qobuz_url = src.qobuz_url.clone();
    }
    if dst.deezer_url.is_empty() {
        dst.deezer_url = src.deezer_url.clone();
    }
    if dst.isrc.is_empty() {
        dst.isrc = src.isrc.clone();
    }
}

/// Returns ordered link resolver list according to settings and optional ranking benchmark (port of Go's `orderedLinkResolvers`).
pub fn ordered_link_resolvers(
    settings: Option<&serde_json::Map<String, serde_json::Value>>,
    ranked_source_ids: Option<RankedSourceIdsFn<'_>>,
) -> Vec<&'static str> {
    let preferred = match settings {
        Some(s) => auralis_config::settings::get_link_resolver(s),
        None => LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK,
    };
    let allow_fallback = match settings {
        Some(s) => auralis_config::settings::get_link_resolver_allow_fallback(s),
        None => true,
    };

    if !allow_fallback {
        if preferred == LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK {
            return vec![LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK];
        }
        return vec![LINK_RESOLVER_PROVIDER_SONGSTATS];
    }

    let mut resources = vec!["resource-songstats", "resource-songlink"];
    if preferred == LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK {
        resources.swap(0, 1);
    }

    let ordered = if let Some(rank_fn) = ranked_source_ids {
        rank_fn(&resources, "resource")
    } else {
        resources.iter().map(|s| s.to_string()).collect()
    };

    let mut providers = Vec::with_capacity(ordered.len());
    for id in ordered {
        if id == "resource-songstats" {
            providers.push(LINK_RESOLVER_PROVIDER_SONGSTATS);
        } else {
            providers.push(LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK);
        }
    }
    providers
}

impl SongLinkClient {
    /// Resolves Spotify track streaming links using configured resolvers (port of Go's `resolveSpotifyTrackLinks`).
    pub async fn resolve_spotify_track_links(
        &self,
        spotify_track_id: &str,
        region: &str,
    ) -> (ResolvedTrackLinks, Option<ProviderError>) {
        self.resolve_spotify_track_links_with_settings(spotify_track_id, region, None)
            .await
    }

    /// Resolves Spotify track streaming links with optional settings override.
    pub async fn resolve_spotify_track_links_with_settings(
        &self,
        spotify_track_id: &str,
        region: &str,
        settings: Option<&serde_json::Map<String, serde_json::Value>>,
    ) -> (ResolvedTrackLinks, Option<ProviderError>) {
        let mut links = ResolvedTrackLinks::default();
        let mut attempts = Vec::new();

        match self.lookup_spotify_isrc(spotify_track_id).await {
            Ok(isrc) => {
                links.isrc = isrc;
            }
            Err(err) => {
                attempts.push(format!("spotify isrc: {err}"));
            }
        }

        let (added, zarz_err) = self
            .resolve_links_via_zarz(&mut links, spotify_track_id)
            .await;
        if let Some(zarz_err) = zarz_err {
            attempts.push(format!("zarz resolve: {zarz_err}"));
        } else if added {
            println!("Using Zarz resolve for Amazon/Qobuz links");
        }

        let ranker: Option<RankedSourceIdsFn<'_>> = self
            .ranked_source_ids
            .as_ref()
            .map(|f| &**f as &dyn Fn(&[&str], &str) -> Vec<String>);
        let resolvers = ordered_link_resolvers(settings, ranker);

        for resolver in resolvers {
            match resolver {
                LINK_RESOLVER_PROVIDER_SONGSTATS => {
                    let (added_data, songstats_err) =
                        self.resolve_links_via_songstats(&mut links).await;
                    if let Some(err) = songstats_err {
                        attempts.push(format!("songstats: {err}"));
                    } else if added_data {
                        println!("Using Songstats as configured link resolver");
                    }
                }
                LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK => {
                    let (added_data, song_link_err) = self
                        .resolve_links_via_deezer_song_link(&mut links, spotify_track_id, region)
                        .await;
                    if let Some(err) = song_link_err {
                        attempts.push(format!("songlink: {err}"));
                    } else if added_data {
                        println!("Using Songlink as configured link resolver");
                    }
                }
                _ => {}
            }

            if !links.tidal_url.is_empty() && !links.amazon_url.is_empty() {
                return (links, None);
            }
        }

        if has_any_song_link_data(&links) {
            return (links, None);
        }

        if attempts.is_empty() {
            attempts.push("no streaming URLs found".to_string());
        }

        let err = ProviderError::Other(attempts.join(" | "));
        (links, Some(err))
    }

    /// Resolves streaming links via Zarz (port of Go's `resolveLinksViaZarz`).
    pub async fn resolve_links_via_zarz(
        &self,
        links: &mut ResolvedTrackLinks,
        spotify_track_id: &str,
    ) -> (bool, Option<ProviderError>) {
        let before = links.clone();
        let track_id = match extract_spotify_track_id(spotify_track_id) {
            Ok(id) => id,
            Err(e) => return (false, Some(e)),
        };
        let res = if let Some(hook) = &self.zarz_resolve {
            hook(&track_id).await
        } else {
            ZarzResolver::resolve(&track_id).await
        };
        match res {
            Ok(mut resolved) => {
                // Tidal IDs from resolve can rematch a different recording. Leave Tidal
                // to song.link / the public US catalog search, matching tidal-web.
                resolved.tidal_url.clear();
                merge_resolved_track_links(links, &resolved);
                (*links != before, None)
            }
            Err(err) => (false, Some(err)),
        }
    }

    /// Resolves links via Songstats with benchmarking outcome recording (port of Go's `resolveLinksViaSongstats`).
    pub async fn resolve_links_via_songstats(
        &self,
        links: &mut ResolvedTrackLinks,
    ) -> (bool, Option<ProviderError>) {
        let started = Instant::now();
        let (matched, mut result_err) = self.resolve_links_via_songstats_inner(links).await;
        if result_err.is_none() && !matched {
            result_err = Some(ProviderError::Other(
                "resolver returned no matching links".to_string(),
            ));
        }
        let elapsed = started.elapsed();
        if let Some(recorder) = &self.record_source_outcome {
            let err_str = result_err.as_ref().map(|e| e.to_string());
            recorder(
                "resource-songstats",
                "",
                "resource",
                elapsed,
                matched && result_err.is_none(),
                err_str.as_deref(),
            );
        }
        (matched, result_err)
    }

    async fn resolve_links_via_songstats_inner(
        &self,
        links: &mut ResolvedTrackLinks,
    ) -> (bool, Option<ProviderError>) {
        if links.isrc.is_empty() {
            return (
                false,
                Some(ProviderError::Other(
                    "ISRC is required for Songstats resolver".to_string(),
                )),
            );
        }

        let before = links.clone();
        let isrc = links.isrc.clone();

        println!("Fetching Songstats links for ISRC {isrc}");
        if let Err(err) = self.populate_links_from_songstats(links, &isrc).await {
            return (false, Some(err));
        }

        (*links != before, None)
    }

    /// Resolves links via Deezer and song.link with benchmarking outcome recording (port of Go's `resolveLinksViaDeezerSongLink`).
    pub async fn resolve_links_via_deezer_song_link(
        &self,
        links: &mut ResolvedTrackLinks,
        spotify_track_id: &str,
        region: &str,
    ) -> (bool, Option<ProviderError>) {
        let started = Instant::now();
        let (matched, mut result_err) = self
            .resolve_links_via_deezer_song_link_inner(links, spotify_track_id, region)
            .await;
        if result_err.is_none() && !matched {
            result_err = Some(ProviderError::Other(
                "resolver returned no matching links".to_string(),
            ));
        }
        let elapsed = started.elapsed();
        if let Some(recorder) = &self.record_source_outcome {
            let err_str = result_err.as_ref().map(|e| e.to_string());
            recorder(
                "resource-songlink",
                "",
                "resource",
                elapsed,
                matched && result_err.is_none(),
                err_str.as_deref(),
            );
        }
        (matched, result_err)
    }

    async fn resolve_links_via_deezer_song_link_inner(
        &self,
        links: &mut ResolvedTrackLinks,
        spotify_track_id: &str,
        region: &str,
    ) -> (bool, Option<ProviderError>) {
        let before = links.clone();
        let mut attempts = Vec::new();

        match extract_spotify_track_id(spotify_track_id) {
            Ok(track_id) => {
                println!("Scraping song.link via Spotify track {track_id}");
                let page_url = format!("https://song.link/s/{track_id}");
                match self.scrape_song_link_page(&page_url, region).await {
                    Ok(data) => {
                        merge_song_link_scrape(links, &data);
                    }
                    Err(scrape_err) => {
                        attempts.push(format!("song.link spotify: {scrape_err}"));
                    }
                }
            }
            Err(err) => {
                attempts.push(format!("spotify track id: {err}"));
            }
        }

        if (links.tidal_url.is_empty() || links.amazon_url.is_empty()) && !links.isrc.is_empty() {
            if links.deezer_url.is_empty() {
                println!("Resolving Deezer track from ISRC {}", links.isrc);
                match self.lookup_deezer_track_url_by_isrc(&links.isrc).await {
                    Ok(deezer_url) => {
                        links.deezer_url = deezer_url;
                        println!("Found Deezer URL: {}", links.deezer_url);
                    }
                    Err(err) => {
                        attempts.push(format!("deezer isrc: {err}"));
                    }
                }
            }

            if !links.deezer_url.is_empty() {
                match extract_deezer_track_id(&links.deezer_url) {
                    Ok(deezer_track_id) => {
                        println!("Scraping song.link via Deezer track {deezer_track_id}");
                        let page_url = format!("https://song.link/d/{deezer_track_id}");
                        match self.scrape_song_link_page(&page_url, region).await {
                            Ok(data) => {
                                merge_song_link_scrape(links, &data);
                            }
                            Err(scrape_err) => {
                                attempts.push(format!("song.link deezer: {scrape_err}"));
                            }
                        }
                    }
                    Err(err) => {
                        attempts.push(format!("deezer track id: {err}"));
                    }
                }
            }
        }

        if *links != before {
            if attempts.is_empty() {
                return (true, None);
            }
            return (true, Some(ProviderError::Other(attempts.join(" | "))));
        }

        if attempts.is_empty() {
            attempts.push("no links found via song.link".to_string());
        }

        (false, Some(ProviderError::Other(attempts.join(" | "))))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::{Arc, Mutex};
    use tokio::io::{AsyncReadExt, AsyncWriteExt};

    async fn start_mock_server<F>(
        handler: F,
    ) -> (
        String,
        tokio::task::JoinHandle<()>,
        tokio::sync::oneshot::Sender<()>,
    )
    where
        F: Fn(&str) -> (u16, &'static str, String) + Send + Sync + 'static,
    {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let server_url = format!("http://127.0.0.1:{}", addr.port());
        let handler = Arc::new(handler);
        let (shutdown_tx, mut shutdown_rx) = tokio::sync::oneshot::channel::<()>();

        let handle = tokio::spawn(async move {
            loop {
                tokio::select! {
                    _ = &mut shutdown_rx => break,
                    accept_res = listener.accept() => {
                        let Ok((mut socket, _)) = accept_res else { break; };
                        let handler = Arc::clone(&handler);
                        tokio::spawn(async move {
                            let mut buf = [0u8; 4096];
                            if let Ok(n) = socket.read(&mut buf).await {
                                if n == 0 { return; }
                                let req = String::from_utf8_lossy(&buf[..n]);
                                let first_line = req.lines().next().unwrap_or("");
                                let path = first_line.split_whitespace().nth(1).unwrap_or("/");
                                let (status, content_type, body) = handler(path);
                                let status_text = if status == 200 { "OK" } else { "Not Found" };
                                let resp = format!(
                                    "HTTP/1.1 {} {}\r\nContent-Type: {}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                                    status, status_text, content_type, body.len(), body
                                );
                                let _ = socket.write_all(resp.as_bytes()).await;
                            }
                        });
                    }
                }
            }
        });

        (server_url, handle, shutdown_tx)
    }

    fn songlink_html(tidal: bool, amazon: bool, deezer: bool, isrc: &str) -> String {
        let mut links = Vec::new();
        if tidal {
            links.push(r#"{"platform":"tidal","url":"https://listen.tidal.com/track/12345678"}"#);
        }
        if amazon {
            links.push(r#"{"platform":"amazonMusic","url":"https://music.amazon.com/albums/B000EXAMPLE?trackAsin=B098765432"}"#);
        }
        if deezer {
            links.push(r#"{"platform":"deezer","url":"https://www.deezer.com/track/11223344"}"#);
        }
        let links_json = links.join(",");
        format!(
            r#"<!DOCTYPE html><html><head><script id="__NEXT_DATA__" type="application/json">{{"props":{{"pageProps":{{"pageData":{{"entityData":{{"isrc":"{isrc}"}},"sections":[{{"links":[{links_json}]}}]}}}}}}}}</script></head><body></body></html>"#
        )
    }

    /// (a) song.link /s/ succeeds with Tidal+Amazon -> no Deezer lookup, no /d/ request
    #[tokio::test]
    async fn test_deezer_songlink_stops_when_tidal_and_amazon_found() {
        let requested_paths = Arc::new(Mutex::new(Vec::new()));
        let paths_clone = Arc::clone(&requested_paths);

        let (server_url, handle, shutdown_tx) = start_mock_server(move |path| {
            paths_clone.lock().unwrap().push(path.to_string());
            if path.starts_with("/s/2EqlS6tkEnglzr7tkKAAYD") {
                let html = songlink_html(true, true, false, "USRC17607839");
                (200, "text/html", html)
            } else {
                (404, "text/plain", "Not Found".to_string())
            }
        })
        .await;

        let client = SongLinkClient::new()
            .with_songlink_base_url(&server_url)
            .with_deezer_base_url(&server_url)
            .with_spotify_isrc_lookup(|_| async { Ok("USRC17607839".to_string()) })
            .with_zarz_resolve(|_| async {
                Err(ProviderError::Other("zarz not configured".to_string()))
            });

        let mut settings = serde_json::Map::new();
        settings.insert(
            "linkResolver".to_string(),
            serde_json::Value::String(LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK.to_string()),
        );
        settings.insert(
            "allowResolverFallback".to_string(),
            serde_json::Value::Bool(false),
        );

        let (links, err) = client
            .resolve_spotify_track_links_with_settings(
                "2EqlS6tkEnglzr7tkKAAYD",
                "",
                Some(&settings),
            )
            .await;

        assert!(err.is_none(), "expected no error, got: {err:?}");
        assert_eq!(links.tidal_url, "https://listen.tidal.com/track/12345678");
        assert_eq!(
            links.amazon_url,
            "https://music.amazon.com/tracks/B098765432?musicTerritory=US"
        );

        let paths = requested_paths.lock().unwrap().clone();
        assert!(
            paths
                .iter()
                .any(|p| p.starts_with("/s/2EqlS6tkEnglzr7tkKAAYD")),
            "expected /s/ request"
        );
        assert!(
            !paths.iter().any(|p| p.contains("/track/isrc:")),
            "Deezer ISRC lookup must NOT be called when Tidal+Amazon are present"
        );
        assert!(
            !paths.iter().any(|p| p.starts_with("/d/")),
            "/d/ request must NOT be made when Tidal+Amazon are present"
        );

        let _ = shutdown_tx.send(());
        let _ = handle.await;
    }

    /// (b) /s/ lacks Amazon and ISRC known -> Deezer ISRC lookup then /d/<id> requested
    #[tokio::test]
    async fn test_deezer_songlink_falls_back_to_deezer_and_d_page() {
        let requested_paths = Arc::new(Mutex::new(Vec::new()));
        let paths_clone = Arc::clone(&requested_paths);

        let (server_url, handle, shutdown_tx) = start_mock_server(move |path| {
            paths_clone.lock().unwrap().push(path.to_string());
            if path.starts_with("/s/2EqlS6tkEnglzr7tkKAAYD") {
                // Returns Tidal only; Amazon missing, ISRC present
                let html = songlink_html(true, false, false, "USRC17607839");
                (200, "text/html", html)
            } else if path.starts_with("/track/isrc:USRC17607839") {
                // Deezer API returns track with Deezer URL
                let json = r#"{"id": 11223344, "link": "https://www.deezer.com/track/11223344"}"#;
                (200, "application/json", json.to_string())
            } else if path.starts_with("/d/11223344") {
                // song.link /d/ page returns Amazon
                let html = songlink_html(false, true, true, "USRC17607839");
                (200, "text/html", html)
            } else {
                (404, "text/plain", "Not Found".to_string())
            }
        })
        .await;

        let client = SongLinkClient::new()
            .with_songlink_base_url(&server_url)
            .with_deezer_base_url(&server_url)
            .with_spotify_isrc_lookup(|_| async { Ok("USRC17607839".to_string()) })
            .with_zarz_resolve(|_| async {
                Err(ProviderError::Other("zarz not configured".to_string()))
            });

        let mut settings = serde_json::Map::new();
        settings.insert(
            "linkResolver".to_string(),
            serde_json::Value::String(LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK.to_string()),
        );
        settings.insert(
            "allowResolverFallback".to_string(),
            serde_json::Value::Bool(false),
        );

        let (links, err) = client
            .resolve_spotify_track_links_with_settings(
                "2EqlS6tkEnglzr7tkKAAYD",
                "",
                Some(&settings),
            )
            .await;

        assert!(err.is_none(), "expected no error, got: {err:?}");
        assert_eq!(links.tidal_url, "https://listen.tidal.com/track/12345678");
        assert_eq!(
            links.amazon_url,
            "https://music.amazon.com/tracks/B098765432?musicTerritory=US"
        );
        assert_eq!(links.deezer_url, "https://www.deezer.com/track/11223344");

        let paths = requested_paths.lock().unwrap().clone();
        assert!(
            paths
                .iter()
                .any(|p| p.starts_with("/s/2EqlS6tkEnglzr7tkKAAYD")),
            "expected /s/ request"
        );
        assert!(
            paths
                .iter()
                .any(|p| p.starts_with("/track/isrc:USRC17607839")),
            "expected Deezer ISRC lookup"
        );
        assert!(
            paths.iter().any(|p| p.starts_with("/d/11223344")),
            "expected /d/ request with extracted Deezer track ID"
        );

        let _ = shutdown_tx.send(());
        let _ = handle.await;
    }

    /// (c) resolver loop stops after first resolver that yields Tidal+Amazon
    #[tokio::test]
    async fn test_resolve_loop_stops_after_first_resolver_with_tidal_amazon() {
        let requested_paths = Arc::new(Mutex::new(Vec::new()));
        let paths_clone = Arc::clone(&requested_paths);

        let (server_url, handle, shutdown_tx) = start_mock_server(move |path| {
            paths_clone.lock().unwrap().push(path.to_string());
            if path.starts_with("/USRC17607839") {
                // Songstats page returns both Tidal and Amazon
                let html = r#"
                    <!DOCTYPE html><html><head>
                    <script type="application/ld+json">
                    {
                        "@context": "https://schema.org",
                        "@type": "MusicRecording",
                        "sameAs": [
                            "https://listen.tidal.com/track/12345678",
                            "https://music.amazon.com/albums/B000EXAMPLE?trackAsin=B098765432"
                        ]
                    }
                    </script></head><body></body></html>
                "#;
                (200, "text/html", html.to_string())
            } else if path.starts_with("/s/") {
                panic!("Songlink should never be called when Songstats yields Tidal+Amazon");
            } else {
                (404, "text/plain", "Not Found".to_string())
            }
        })
        .await;

        let client = SongLinkClient::new()
            .with_songstats_base_url(&server_url)
            .with_songlink_base_url(&server_url)
            .with_spotify_isrc_lookup(|_| async { Ok("USRC17607839".to_string()) })
            .with_zarz_resolve(|_| async {
                Err(ProviderError::Other("zarz not configured".to_string()))
            });

        // Preferred: songstats, allow_fallback: true
        let mut settings = serde_json::Map::new();
        settings.insert(
            "linkResolver".to_string(),
            serde_json::Value::String(LINK_RESOLVER_PROVIDER_SONGSTATS.to_string()),
        );
        settings.insert(
            "allowResolverFallback".to_string(),
            serde_json::Value::Bool(true),
        );

        let (links, err) = client
            .resolve_spotify_track_links_with_settings(
                "2EqlS6tkEnglzr7tkKAAYD",
                "",
                Some(&settings),
            )
            .await;

        assert!(err.is_none());
        assert_eq!(links.tidal_url, "https://listen.tidal.com/track/12345678");
        assert_eq!(
            links.amazon_url,
            "https://music.amazon.com/tracks/B098765432?musicTerritory=US"
        );

        let paths = requested_paths.lock().unwrap().clone();
        assert!(
            paths.iter().any(|p| p.starts_with("/USRC17607839")),
            "Songstats must be called"
        );
        assert!(
            !paths.iter().any(|p| p.starts_with("/s/")),
            "Songlink must NOT be called when loop stops on Tidal+Amazon"
        );

        let _ = shutdown_tx.send(());
        let _ = handle.await;
    }

    /// (c-2) resolver loop stops after first resolver yielding Tidal+Amazon even if it reported an error
    #[tokio::test]
    async fn test_resolve_loop_stops_after_first_resolver_yielding_tidal_amazon_even_with_error() {
        let requested_paths = Arc::new(Mutex::new(Vec::new()));
        let paths_clone = Arc::clone(&requested_paths);

        let (server_url, handle, shutdown_tx) = start_mock_server(move |path| {
            paths_clone.lock().unwrap().push(path.to_string());
            if path.starts_with("/s/2EqlS6tkEnglzr7tkKAAYD") {
                // Returns Tidal and Amazon, but Deezer is missing
                let html = songlink_html(true, true, false, "USRC17607839");
                (200, "text/html", html)
            } else if path.starts_with("/USRC17607839") {
                panic!("Songstats must NOT be called when first resolver yields Tidal+Amazon");
            } else {
                (404, "text/plain", "Not Found".to_string())
            }
        })
        .await;

        let client = SongLinkClient::new()
            .with_songlink_base_url(&server_url)
            .with_songstats_base_url(&server_url)
            .with_spotify_isrc_lookup(|_| async { Ok("USRC17607839".to_string()) })
            .with_zarz_resolve(|_| async { Err(ProviderError::Other("zarz timeout".to_string())) });

        // Preferred: deezer-songlink, allow_fallback: true
        let mut settings = serde_json::Map::new();
        settings.insert(
            "linkResolver".to_string(),
            serde_json::Value::String(LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK.to_string()),
        );
        settings.insert(
            "allowResolverFallback".to_string(),
            serde_json::Value::Bool(true),
        );

        let (links, err) = client
            .resolve_spotify_track_links_with_settings(
                "2EqlS6tkEnglzr7tkKAAYD",
                "",
                Some(&settings),
            )
            .await;

        // Even though Zarz recorded an attempt error, Tidal+Amazon are present, so (links, None) is returned!
        assert!(
            err.is_none(),
            "expected None error because Tidal+Amazon found, got: {err:?}"
        );
        assert_eq!(links.tidal_url, "https://listen.tidal.com/track/12345678");
        assert_eq!(
            links.amazon_url,
            "https://music.amazon.com/tracks/B098765432?musicTerritory=US"
        );

        let paths = requested_paths.lock().unwrap().clone();
        assert!(
            paths.iter().any(|p| p.starts_with("/s/")),
            "DeezerSonglink must be called"
        );
        assert!(
            !paths.iter().any(|p| p.starts_with("/USRC17607839")),
            "Songstats must NOT be called"
        );

        let _ = shutdown_tx.send(());
        let _ = handle.await;
    }

    /// (d) nothing found -> exact joined error and partial links returned
    #[tokio::test]
    async fn test_resolve_track_links_returns_exact_error_and_partial_links() {
        let client = SongLinkClient::new()
            .with_spotify_isrc_lookup(|_| async {
                Err(ProviderError::Other("network timeout".to_string()))
            })
            .with_zarz_resolve(|_| async {
                Err(ProviderError::Other("server error 500".to_string()))
            })
            .with_songlink_base_url("http://127.0.0.1:1");

        let mut settings = serde_json::Map::new();
        settings.insert(
            "linkResolver".to_string(),
            serde_json::Value::String(LINK_RESOLVER_PROVIDER_SONGSTATS.to_string()),
        );
        settings.insert(
            "allowResolverFallback".to_string(),
            serde_json::Value::Bool(true),
        );

        let (links, err) = client
            .resolve_spotify_track_links_with_settings(
                "2EqlS6tkEnglzr7tkKAAYD",
                "",
                Some(&settings),
            )
            .await;

        assert_eq!(links, ResolvedTrackLinks::default());
        let err = err.expect("expected error when nothing resolved");
        assert!(
            err.to_string().starts_with("spotify isrc: network timeout | zarz resolve: server error 500 | songstats: ISRC is required for Songstats resolver | songlink:"),
            "Error string was: {err}"
        );
    }

    /// (d-2) nothing found -> exact joined attempt text, partial links (ISRC) still returned
    #[tokio::test]
    async fn test_resolve_track_links_returns_partial_links_when_no_streaming_urls_found() {
        let (server_url, handle, shutdown_tx) = start_mock_server(|path| {
            if path.starts_with("/USRC17607839") {
                (
                    200,
                    "text/html",
                    "<html><body>No links here</body></html>".to_string(),
                )
            } else if path.starts_with("/s/2EqlS6tkEnglzr7tkKAAYD") {
                let html = songlink_html(false, false, false, "USRC17607839");
                (200, "text/html", html)
            } else if path.starts_with("/track/isrc:USRC17607839") {
                (
                    404,
                    "application/json",
                    r#"{"error":{"message":"Data not found"}}"#.to_string(),
                )
            } else {
                (404, "text/plain", "Not Found".to_string())
            }
        })
        .await;

        let client = SongLinkClient::new()
            .with_spotify_isrc_lookup(|_| async { Ok("USRC17607839".to_string()) })
            .with_zarz_resolve(|_| async { Err(ProviderError::Other("zarz error".to_string())) })
            .with_songstats_base_url(&server_url)
            .with_songlink_base_url(&server_url)
            .with_deezer_base_url(&server_url);

        let mut settings = serde_json::Map::new();
        settings.insert(
            "linkResolver".to_string(),
            serde_json::Value::String(LINK_RESOLVER_PROVIDER_SONGSTATS.to_string()),
        );
        settings.insert(
            "allowResolverFallback".to_string(),
            serde_json::Value::Bool(true),
        );

        let (links, err) = client
            .resolve_spotify_track_links_with_settings(
                "2EqlS6tkEnglzr7tkKAAYD",
                "",
                Some(&settings),
            )
            .await;

        // Partial link: ISRC was resolved and must be returned!
        assert_eq!(links.isrc, "USRC17607839");
        assert_eq!(links.tidal_url, "");
        assert_eq!(links.amazon_url, "");
        assert_eq!(links.deezer_url, "");
        assert_eq!(links.qobuz_url, "");

        let err = err.expect("expected error when no streaming URLs found");
        assert_eq!(
            err.to_string(),
            "zarz resolve: zarz error | songstats: Songstats JSON-LD not found | songlink: deezer isrc: Deezer ISRC API returned status 404"
        );

        let _ = shutdown_tx.send(());
        let _ = handle.await;
    }

    /// (e) orderedLinkResolvers combinations incl. unknown ranked ID
    #[test]
    fn test_ordered_link_resolvers_combinations() {
        // 1. allow_fallback = false, preferred = deezer-songlink
        let mut s1 = serde_json::Map::new();
        s1.insert(
            "linkResolver".to_string(),
            serde_json::Value::String(LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK.to_string()),
        );
        s1.insert(
            "allowResolverFallback".to_string(),
            serde_json::Value::Bool(false),
        );
        assert_eq!(
            ordered_link_resolvers(Some(&s1), None),
            vec![LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK]
        );

        // 2. allow_fallback = false, preferred = songstats
        let mut s2 = serde_json::Map::new();
        s2.insert(
            "linkResolver".to_string(),
            serde_json::Value::String(LINK_RESOLVER_PROVIDER_SONGSTATS.to_string()),
        );
        s2.insert(
            "allowResolverFallback".to_string(),
            serde_json::Value::Bool(false),
        );
        assert_eq!(
            ordered_link_resolvers(Some(&s2), None),
            vec![LINK_RESOLVER_PROVIDER_SONGSTATS]
        );

        // 3. allow_fallback = true, preferred = deezer-songlink, ranker = None
        let mut s3 = serde_json::Map::new();
        s3.insert(
            "linkResolver".to_string(),
            serde_json::Value::String(LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK.to_string()),
        );
        s3.insert(
            "allowResolverFallback".to_string(),
            serde_json::Value::Bool(true),
        );
        assert_eq!(
            ordered_link_resolvers(Some(&s3), None),
            vec![
                LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK,
                LINK_RESOLVER_PROVIDER_SONGSTATS
            ]
        );

        // 4. allow_fallback = true, preferred = songstats, ranker = None
        let mut s4 = serde_json::Map::new();
        s4.insert(
            "linkResolver".to_string(),
            serde_json::Value::String(LINK_RESOLVER_PROVIDER_SONGSTATS.to_string()),
        );
        s4.insert(
            "allowResolverFallback".to_string(),
            serde_json::Value::Bool(true),
        );
        assert_eq!(
            ordered_link_resolvers(Some(&s4), None),
            vec![
                LINK_RESOLVER_PROVIDER_SONGSTATS,
                LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK
            ]
        );

        // 5. allow_fallback = true, preferred = deezer-songlink, ranker returns unknown ID followed by songstats
        // In Go: "resource-songstats" -> songstats, ANY other ID -> deezer-songlink.
        let ranker_fn = |_input: &[&str], kind: &str| -> Vec<String> {
            assert_eq!(kind, "resource");
            vec![
                "unknown-provider-id".to_string(),
                "resource-songstats".to_string(),
            ]
        };
        assert_eq!(
            ordered_link_resolvers(Some(&s3), Some(&ranker_fn)),
            vec![
                LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK,
                LINK_RESOLVER_PROVIDER_SONGSTATS
            ]
        );

        // 6. allow_fallback = true, preferred = songstats, ranker returns duplicate unknown IDs without deduplication
        let ranker_dup = |_input: &[&str], _kind: &str| -> Vec<String> {
            vec![
                "resource-songstats".to_string(),
                "unknown-provider-id".to_string(),
                "resource-songlink".to_string(),
            ]
        };
        assert_eq!(
            ordered_link_resolvers(Some(&s4), Some(&ranker_dup)),
            vec![
                LINK_RESOLVER_PROVIDER_SONGSTATS,
                LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK,
                LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK,
            ]
        );
    }

    /// (f) songstats with empty ISRC -> exact Go error and outcome recorded with "resource-songstats"/"resource"
    #[tokio::test]
    async fn test_songstats_empty_isrc_outcome() {
        #[derive(Clone, Debug)]
        struct Outcome {
            source_id: String,
            service: String,
            quality: String,
            success: bool,
            err: Option<String>,
        }

        let recorded = Arc::new(Mutex::new(Vec::new()));
        let rec_clone = Arc::clone(&recorded);

        let client = SongLinkClient::new().with_record_source_outcome(
            move |source_id, service, quality, _elapsed, success, err| {
                rec_clone.lock().unwrap().push(Outcome {
                    source_id: source_id.to_string(),
                    service: service.to_string(),
                    quality: quality.to_string(),
                    success,
                    err: err.map(|s| s.to_string()),
                });
            },
        );

        let mut links = ResolvedTrackLinks::default();
        let (matched, err) = client.resolve_links_via_songstats(&mut links).await;

        assert!(!matched);
        let err = err.expect("expected error on empty ISRC");
        assert_eq!(err.to_string(), "ISRC is required for Songstats resolver");

        let outcomes = recorded.lock().unwrap().clone();
        assert_eq!(outcomes.len(), 1);
        let out = &outcomes[0];
        assert_eq!(out.source_id, "resource-songstats");
        assert_eq!(out.service, "");
        assert_eq!(out.quality, "resource");
        assert!(!out.success);
        assert_eq!(
            out.err.as_deref(),
            Some("ISRC is required for Songstats resolver")
        );
    }

    /// (f-2) deezer-songlink records outcome with "resource-songlink"/"resource"
    #[tokio::test]
    async fn test_deezer_songlink_outcome_recording() {
        #[derive(Clone, Debug)]
        struct Outcome {
            source_id: String,
            service: String,
            quality: String,
            success: bool,
            err: Option<String>,
        }

        let recorded = Arc::new(Mutex::new(Vec::new()));
        let rec_clone = Arc::clone(&recorded);

        let client = SongLinkClient::new()
            .with_songlink_base_url("http://127.0.0.1:1")
            .with_record_source_outcome(
                move |source_id, service, quality, _elapsed, success, err| {
                    rec_clone.lock().unwrap().push(Outcome {
                        source_id: source_id.to_string(),
                        service: service.to_string(),
                        quality: quality.to_string(),
                        success,
                        err: err.map(|s| s.to_string()),
                    });
                },
            );

        let mut links = ResolvedTrackLinks::default();
        let (matched, err) = client
            .resolve_links_via_deezer_song_link(&mut links, "2EqlS6tkEnglzr7tkKAAYD", "")
            .await;

        assert!(!matched);
        assert!(err.is_some());

        let outcomes = recorded.lock().unwrap().clone();
        assert_eq!(outcomes.len(), 1);
        let out = &outcomes[0];
        assert_eq!(out.source_id, "resource-songlink");
        assert_eq!(out.service, "");
        assert_eq!(out.quality, "resource");
        assert!(!out.success);
        assert!(out.err.is_some());
    }
}

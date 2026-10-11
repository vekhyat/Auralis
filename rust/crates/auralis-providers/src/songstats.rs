//! Songstats metadata scraper and link population (port of `backend/songstats.go`).

use std::sync::LazyLock;

use regex::Regex;

use crate::bounded_body::read_bounded_response;
use crate::error::{ProviderError, Result};
use crate::jiosaavn::html_unescape;
use crate::songlink::{
    has_any_song_link_data, map_http_error, send_request, SongLinkClient, SONGLINK_USER_AGENT,
};
use crate::zarz::{normalize_amazon_music_url, normalize_deezer_track_url, ResolvedTrackLinks};

static SONGSTATS_SCRIPT_PATTERN: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r#"(?is)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>"#).unwrap()
});

impl SongLinkClient {
    /// Populates streaming links from Songstats for a given track ISRC.
    pub async fn populate_links_from_songstats(
        &self,
        links: &mut ResolvedTrackLinks,
        isrc: &str,
    ) -> Result<()> {
        let base = self
            .songstats_base_url
            .as_deref()
            .unwrap_or("https://songstats.com");
        let page_url = format!("{}/{}?ref=ISRCFinder", base, isrc.trim().to_uppercase());

        let req = self
            .client
            .get(&page_url)
            .header("User-Agent", SONGLINK_USER_AGENT);

        let resp = send_request(req, self.cancel_token.as_ref())
            .await
            .map_err(|e| map_http_error(e, "failed to fetch Songstats page"))?;

        let status = resp.status();
        if status != reqwest::StatusCode::OK {
            return Err(ProviderError::Other(format!(
                "Songstats returned status {}",
                status.as_u16()
            )));
        }

        let body = read_bounded_response(resp, 8 << 20).await.map_err(|err| {
            ProviderError::Other(format!("failed to read Songstats response: {err}"))
        })?;

        parse_and_populate_songstats_links(&body, links)
    }
}

/// Parses HTML containing Songstats JSON-LD scripts and populates platform links on `links`.
pub fn parse_and_populate_songstats_links(
    body: &[u8],
    links: &mut ResolvedTrackLinks,
) -> Result<()> {
    let body_str = String::from_utf8_lossy(body);
    let mut found = false;

    let mut matches_count = 0;
    for caps in SONGSTATS_SCRIPT_PATTERN.captures_iter(&body_str) {
        matches_count += 1;
        let script_raw = &caps[1];
        let script_body = html_unescape(script_raw).trim().to_string();
        if script_body.is_empty() {
            continue;
        }

        let Ok(payload) = serde_json::from_str::<serde_json::Value>(&script_body) else {
            continue;
        };

        let before = links.clone();
        collect_songstats_links(&payload, links);
        if *links != before {
            found = true;
        }
    }

    if matches_count == 0 {
        return Err(ProviderError::Other(
            "Songstats JSON-LD not found".to_string(),
        ));
    }

    if !found && !has_any_song_link_data(links) {
        return Err(ProviderError::Other(
            "no platform links found in Songstats".to_string(),
        ));
    }

    Ok(())
}

fn collect_songstats_links(value: &serde_json::Value, links: &mut ResolvedTrackLinks) {
    match value {
        serde_json::Value::Object(map) => {
            if let Some(same_as) = map.get("sameAs") {
                apply_songstats_same_as(same_as, links);
            }
            for nested in map.values() {
                collect_songstats_links(nested, links);
            }
        }
        serde_json::Value::Array(items) => {
            for nested in items {
                collect_songstats_links(nested, links);
            }
        }
        _ => {}
    }
}

fn apply_songstats_same_as(value: &serde_json::Value, links: &mut ResolvedTrackLinks) {
    match value {
        serde_json::Value::String(link) => {
            assign_songstats_link(link, links);
        }
        serde_json::Value::Array(items) => {
            for item in items {
                if let serde_json::Value::String(link) = item {
                    assign_songstats_link(link, links);
                }
            }
        }
        _ => {}
    }
}

fn assign_songstats_link(raw_link: &str, links: &mut ResolvedTrackLinks) {
    let link = raw_link.trim();
    if link.is_empty() {
        return;
    }

    if link.contains("listen.tidal.com/track") {
        if links.tidal_url.is_empty() {
            links.tidal_url = link.to_string();
            println!("Tidal URL found via Songstats");
        }
    } else if link.contains("music.amazon.com") {
        if links.amazon_url.is_empty() {
            let normalized = normalize_amazon_music_url(link);
            if !normalized.is_empty() {
                links.amazon_url = normalized;
                println!("Amazon URL found via Songstats");
            }
        }
    } else if link.contains("deezer.com") {
        if links.deezer_url.is_empty() {
            links.deezer_url = normalize_deezer_track_url(link);
            println!("Deezer URL found via Songstats");
        }
    } else if link.contains("qobuz.com") && links.qobuz_url.is_empty() {
        links.qobuz_url = link.to_string();
        println!("Qobuz URL found via Songstats");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_and_populate_songstats_links() {
        let html = r#"
            <!DOCTYPE html>
            <html>
            <head>
            <script type="application/ld+json">
            {
                "@context": "https://schema.org",
                "@type": "MusicRecording",
                "name": "Test Track",
                "sameAs": [
                    "https://listen.tidal.com/track/12345678",
                    "https://music.amazon.com/albums/B000EXAMPLE?trackAsin=B098765432",
                    "https://www.deezer.com/track/11223344",
                    "https://open.qobuz.com/track/55667788"
                ]
            }
            </script>
            </head>
            <body></body>
            </html>
        "#;

        let mut links = ResolvedTrackLinks::default();
        parse_and_populate_songstats_links(html.as_bytes(), &mut links).unwrap();

        assert_eq!(links.tidal_url, "https://listen.tidal.com/track/12345678");
        assert_eq!(
            links.amazon_url,
            "https://music.amazon.com/tracks/B098765432?musicTerritory=US"
        );
        assert_eq!(links.deezer_url, "https://www.deezer.com/track/11223344");
        assert_eq!(links.qobuz_url, "https://open.qobuz.com/track/55667788");
    }

    #[test]
    fn test_parse_and_populate_songstats_links_from_fixture() {
        let fixture_bytes = include_bytes!("../tests/fixtures/songstats/songstats_page.html");
        let mut links = ResolvedTrackLinks::default();
        parse_and_populate_songstats_links(fixture_bytes, &mut links).unwrap();

        assert_eq!(links.tidal_url, "https://listen.tidal.com/track/87654321");
        assert_eq!(
            links.amazon_url,
            "https://music.amazon.com/tracks/B012345678?musicTerritory=US"
        );
        assert_eq!(links.deezer_url, "https://www.deezer.com/track/44332211");
        assert_eq!(links.qobuz_url, "https://open.qobuz.com/track/88776655");
    }

    #[test]
    fn test_parse_songstats_missing_ld_json() {
        let html = "<html><body>No script here</body></html>";
        let mut links = ResolvedTrackLinks::default();
        let err = parse_and_populate_songstats_links(html.as_bytes(), &mut links)
            .expect_err("should fail when JSON-LD is missing");
        assert!(err.to_string().contains("Songstats JSON-LD not found"));
    }

    #[test]
    fn test_parse_songstats_no_links() {
        let html = r#"<script type="application/ld+json">{"@type": "MusicRecording"}</script>"#;
        let mut links = ResolvedTrackLinks::default();
        let err = parse_and_populate_songstats_links(html.as_bytes(), &mut links)
            .expect_err("should fail when no links found");
        assert!(err
            .to_string()
            .contains("no platform links found in Songstats"));
    }
}

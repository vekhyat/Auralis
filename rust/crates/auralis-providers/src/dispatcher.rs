//! Unified provider dispatcher and fallback resolver.

use crate::error::{ProviderError, Result};
use crate::models::{ResolvedStream, TrackMetadata};
use crate::zarz::{ResolvedTrackLinks, ZarzResolver};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PreferredService {
    Tidal,
    Qobuz,
    Amazon,
    Deezer,
    Apple,
    JioSaavn,
}

#[derive(Debug, Clone)]
pub struct ResolutionPlan {
    pub metadata: TrackMetadata,
    pub links: ResolvedTrackLinks,
    pub service_order: Vec<PreferredService>,
}

impl ResolutionPlan {
    pub fn new(
        metadata: TrackMetadata,
        links: ResolvedTrackLinks,
        preferred: Option<PreferredService>,
    ) -> Self {
        let mut service_order = Vec::with_capacity(6);
        if let Some(p) = preferred {
            service_order.push(p);
        }
        for svc in [
            PreferredService::Tidal,
            PreferredService::Qobuz,
            PreferredService::Deezer,
            PreferredService::Amazon,
            PreferredService::Apple,
            PreferredService::JioSaavn,
        ] {
            if !service_order.contains(&svc) {
                service_order.push(svc);
            }
        }
        Self {
            metadata,
            links,
            service_order,
        }
    }

    /// Selects the first available direct stream or URL according to service order.
    pub fn select_best_service_url(&self) -> Result<(PreferredService, String)> {
        for svc in &self.service_order {
            let url = match svc {
                PreferredService::Tidal if !self.links.tidal_url.is_empty() => {
                    Some(&self.links.tidal_url)
                }
                PreferredService::Qobuz if !self.links.qobuz_url.is_empty() => {
                    Some(&self.links.qobuz_url)
                }
                PreferredService::Deezer if !self.links.deezer_url.is_empty() => {
                    Some(&self.links.deezer_url)
                }
                PreferredService::Amazon if !self.links.amazon_url.is_empty() => {
                    Some(&self.links.amazon_url)
                }
                _ => None,
            };
            if let Some(u) = url {
                return Ok((*svc, u.clone()));
            }
        }

        Err(ProviderError::ResolutionFailed(
            "no matching stream link found across services".into(),
        ))
    }
}

pub struct FallbackDispatcher;

impl FallbackDispatcher {
    pub fn plan(
        metadata: TrackMetadata,
        zarz_body: &[u8],
        preferred: Option<PreferredService>,
    ) -> Result<ResolutionPlan> {
        let links = ZarzResolver::parse_resolve_body(zarz_body)?;
        Ok(ResolutionPlan::new(metadata, links, preferred))
    }

    pub fn synthesize_resolved_stream(
        stream_url: String,
        format: String,
        bitrate_kbps: u32,
        is_encrypted: bool,
        encryption_key: Option<Vec<u8>>,
    ) -> ResolvedStream {
        ResolvedStream {
            stream_url,
            format,
            bitrate_kbps,
            is_encrypted,
            encryption_key,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_resolution_plan_order_and_selection() {
        let meta = TrackMetadata {
            isrc: "USRC12345678".into(),
            title: "Test Track".into(),
            artists: vec!["Test Artist".into()],
            album: "Test Album".into(),
            duration_ms: 180000,
            cover_url: None,
            spotify_id: None,
        };

        let links = ResolvedTrackLinks {
            isrc: "USRC12345678".into(),
            tidal_url: "https://tidal.com/track/111".into(),
            amazon_url: "https://music.amazon.com/tracks/B09ABC".into(),
            qobuz_url: "".into(),
            deezer_url: "https://deezer.com/track/222".into(),
        };

        // Prefer Deezer
        let plan = ResolutionPlan::new(meta.clone(), links.clone(), Some(PreferredService::Deezer));
        assert_eq!(plan.service_order[0], PreferredService::Deezer);
        let (svc, url) = plan.select_best_service_url().unwrap();
        assert_eq!(svc, PreferredService::Deezer);
        assert_eq!(url, "https://deezer.com/track/222");

        // Prefer Qobuz (not present in links), fallback to next in order
        let plan2 = ResolutionPlan::new(meta, links, Some(PreferredService::Qobuz));
        let (svc2, url2) = plan2.select_best_service_url().unwrap();
        assert_eq!(svc2, PreferredService::Tidal);
        assert_eq!(url2, "https://tidal.com/track/111");
    }
}

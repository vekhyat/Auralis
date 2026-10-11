//! Port of `backend/isrc_helper.go`.

pub use crate::isrc_finder::{
    close_isrc_cache_db, get_cached_isrc, init_isrc_cache_db, init_isrc_cache_db_at,
    put_cached_isrc, with_isrc_cache_db,
};
use crate::songlink::SongLinkClient;

/// Port of Go's `ResolveTrackISRC`.
///
/// Resolves a track's ISRC: checks the local ISRC cache first; on cache miss,
/// queries SongLink/Spotify via [`SongLinkClient::get_isrc_direct`]. Returns the
/// uppercase ISRC on success, or an empty string if resolution fails.
pub async fn resolve_track_isrc(spotify_track_id: &str) -> String {
    let id = spotify_track_id.trim();
    if id.is_empty() {
        return String::new();
    }

    if let Ok(cached) = get_cached_isrc(id) {
        if !cached.is_empty() {
            return cached.trim().to_uppercase();
        }
    }

    let client = SongLinkClient::new().with_spotify_isrc_lookup(|id| {
        let id_owned = id.to_string();
        async move { crate::isrc_finder::lookup_spotify_isrc(&id_owned).await }
    });

    match client.get_isrc_direct(id).await {
        Ok(isrc) => isrc.trim().to_uppercase(),
        Err(_) => String::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_resolve_track_isrc_empty() {
        assert_eq!(resolve_track_isrc("").await, "");
        assert_eq!(resolve_track_isrc("   ").await, "");
    }

    #[test]
    fn test_resolve_track_isrc_cache_hit() {
        let _guard = crate::isrc_finder::ISRC_STORE_TEST_LOCK
            .lock()
            .unwrap_or_else(|e| e.into_inner());
        close_isrc_cache_db();
        let temp = tempfile::tempdir().unwrap();
        let db_path = temp.path().join("test_isrc_helper_cache.redb");

        init_isrc_cache_db_at(&db_path).unwrap();
        put_cached_isrc("cached-track-123", "usrc12345678").unwrap();

        let runtime = tokio::runtime::Runtime::new().unwrap();
        let resolved = runtime.block_on(resolve_track_isrc("cached-track-123"));
        assert_eq!(resolved, "USRC12345678");

        close_isrc_cache_db();
    }
}

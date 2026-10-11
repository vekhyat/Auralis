//! Configuration getters, normalizers, and legacy cleanup routines.

use crate::settings_store::SettingsStore;
use serde_json::{Map, Value};
use std::path::Path;

pub const LEGACY_TIDAL_API_CACHE_FILE: &str = "tidal-api-urls.json";
pub const LINK_RESOLVER_PROVIDER_SONGSTATS: &str = "songstats";
pub const LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK: &str = "deezer-songlink";
pub const LINK_RESOLVER_SONGSTATS: &str = LINK_RESOLVER_PROVIDER_SONGSTATS;
pub const LINK_RESOLVER_DEEZER_SONGLINK: &str = LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK;
pub const DEFAULT_EXISTING_FILE_CHECK_MODE: &str = "filename";

/// Deletes the legacy `tidal-api-urls.json` cache file inside the application directory.
/// Ignores NotFound errors.
pub fn cleanup_legacy_tidal_public_api_state() -> std::io::Result<()> {
    let app_dir = crate::paths::ensure_app_dir()?;
    cleanup_legacy_tidal_public_api_state_in(&app_dir)
}

/// Deletes the legacy `tidal-api-urls.json` cache file inside the given directory.
/// Ignores NotFound errors.
pub fn cleanup_legacy_tidal_public_api_state_in(app_dir: &Path) -> std::io::Result<()> {
    let cache_path = app_dir.join(LEGACY_TIDAL_API_CACHE_FILE);
    match std::fs::remove_file(&cache_path) {
        Ok(()) => Ok(()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(e) => Err(e),
    }
}

/// Normalizes the existing file check mode string.
/// - "isrc", "upc" -> "isrc"
/// - "hybrid" -> "hybrid"
/// - everything else / empty -> "filename"
pub fn normalize_existing_file_check_mode(value: &str) -> &'static str {
    match value.trim().to_lowercase().as_str() {
        "isrc" | "upc" => "isrc",
        "hybrid" => "hybrid",
        _ => DEFAULT_EXISTING_FILE_CHECK_MODE,
    }
}

/// Normalizes the link resolver provider string.
/// - "songstats" -> "songstats"
/// - "songlink", "deezer-songlink", "" -> "deezer-songlink"
/// - fallback / unknown -> "deezer-songlink"
pub fn normalize_link_resolver(value: &str) -> &'static str {
    match value.trim().to_lowercase().as_str() {
        "songstats" => LINK_RESOLVER_PROVIDER_SONGSTATS,
        "songlink" | "deezer-songlink" | "" => LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK,
        _ => LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK,
    }
}

/// Returns the boolean setting for `redownloadWithSuffix` from a settings map. Default false.
pub fn get_redownload_with_suffix(settings: &Map<String, Value>) -> bool {
    settings
        .get("redownloadWithSuffix")
        .and_then(|v| v.as_bool())
        .unwrap_or(false)
}

/// Returns the boolean setting for `redownloadWithSuffix` from the settings store. Default false.
pub fn get_redownload_with_suffix_setting(store: &SettingsStore) -> bool {
    store
        .load()
        .map(|s| get_redownload_with_suffix(&s))
        .unwrap_or(false)
}

/// Returns normalized `existingFileCheckMode` from a settings map. Default "filename".
pub fn get_existing_file_check_mode(settings: &Map<String, Value>) -> &'static str {
    match settings
        .get("existingFileCheckMode")
        .and_then(|v| v.as_str())
    {
        Some(raw) => normalize_existing_file_check_mode(raw),
        None => DEFAULT_EXISTING_FILE_CHECK_MODE,
    }
}

/// Returns normalized `existingFileCheckMode` from the settings store. Default "filename".
pub fn get_existing_file_check_mode_setting(store: &SettingsStore) -> &'static str {
    store
        .load()
        .map(|s| get_existing_file_check_mode(&s))
        .unwrap_or(DEFAULT_EXISTING_FILE_CHECK_MODE)
}

/// Returns normalized `linkResolver` from a settings map. Default "deezer-songlink".
pub fn get_link_resolver(settings: &Map<String, Value>) -> &'static str {
    match settings.get("linkResolver").and_then(|v| v.as_str()) {
        Some(raw) => normalize_link_resolver(raw),
        None => LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK,
    }
}

/// Returns normalized `linkResolver` from the settings store. Default "deezer-songlink".
pub fn get_link_resolver_setting(store: &SettingsStore) -> &'static str {
    store
        .load()
        .map(|s| get_link_resolver(&s))
        .unwrap_or(LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK)
}

/// Returns boolean setting for `allowResolverFallback` from a settings map. Default true.
pub fn get_link_resolver_allow_fallback(settings: &Map<String, Value>) -> bool {
    settings
        .get("allowResolverFallback")
        .and_then(|v| v.as_bool())
        .unwrap_or(true)
}

/// Returns boolean setting for `allowResolverFallback` from the settings store. Default true.
pub fn get_link_resolver_allow_fallback_setting(store: &SettingsStore) -> bool {
    store
        .load()
        .map(|s| get_link_resolver_allow_fallback(&s))
        .unwrap_or(true)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_cleanup_legacy_tidal_public_api_state() {
        let temp_dir = tempfile::tempdir().unwrap();
        let app_dir = temp_dir.path();
        let cache_file = app_dir.join(LEGACY_TIDAL_API_CACHE_FILE);

        // 1. Succeeds if file does not exist (idempotent)
        assert!(!cache_file.exists());
        assert!(cleanup_legacy_tidal_public_api_state_in(app_dir).is_ok());

        // 2. Removes file if it exists
        std::fs::write(&cache_file, b"{\"cached\": true}").unwrap();
        assert!(cache_file.exists());
        assert!(cleanup_legacy_tidal_public_api_state_in(app_dir).is_ok());
        assert!(!cache_file.exists());

        // 3. Second call also succeeds
        assert!(cleanup_legacy_tidal_public_api_state_in(app_dir).is_ok());
    }

    #[test]
    fn test_normalize_existing_file_check_mode() {
        // Accepted values
        assert_eq!(normalize_existing_file_check_mode("isrc"), "isrc");
        assert_eq!(normalize_existing_file_check_mode("ISRC"), "isrc");
        assert_eq!(normalize_existing_file_check_mode(" upc "), "isrc");
        assert_eq!(normalize_existing_file_check_mode("UPC"), "isrc");
        assert_eq!(normalize_existing_file_check_mode("hybrid"), "hybrid");
        assert_eq!(normalize_existing_file_check_mode("  HYBRID  "), "hybrid");

        // Defaults and invalid inputs
        assert_eq!(normalize_existing_file_check_mode("filename"), "filename");
        assert_eq!(normalize_existing_file_check_mode(""), "filename");
        assert_eq!(normalize_existing_file_check_mode("   "), "filename");
        assert_eq!(
            normalize_existing_file_check_mode("unknown_mode"),
            "filename"
        );
        assert_eq!(normalize_existing_file_check_mode("1234"), "filename");
    }

    #[test]
    fn test_normalize_link_resolver() {
        // Songstats
        assert_eq!(
            normalize_link_resolver("songstats"),
            LINK_RESOLVER_PROVIDER_SONGSTATS
        );
        assert_eq!(
            normalize_link_resolver(" SongStats "),
            LINK_RESOLVER_PROVIDER_SONGSTATS
        );

        // Deezer songlink and aliases
        assert_eq!(
            normalize_link_resolver("songlink"),
            LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK
        );
        assert_eq!(
            normalize_link_resolver("deezer-songlink"),
            LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK
        );
        assert_eq!(
            normalize_link_resolver(" Deezer-SongLink "),
            LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK
        );

        // Defaults and invalid inputs
        assert_eq!(
            normalize_link_resolver(""),
            LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK
        );
        assert_eq!(
            normalize_link_resolver("   "),
            LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK
        );
        assert_eq!(
            normalize_link_resolver("spotify"),
            LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK
        );
        assert_eq!(
            normalize_link_resolver("random-junk"),
            LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK
        );
    }

    #[test]
    fn test_getters_from_map() {
        let mut map = Map::new();

        // Defaults when empty
        assert!(!get_redownload_with_suffix(&map));
        assert_eq!(get_existing_file_check_mode(&map), "filename");
        assert_eq!(get_link_resolver(&map), "deezer-songlink");
        assert!(get_link_resolver_allow_fallback(&map));

        // When values are set
        map.insert("redownloadWithSuffix".into(), json!(true));
        map.insert("existingFileCheckMode".into(), json!("UPC"));
        map.insert("linkResolver".into(), json!("songstats"));
        map.insert("allowResolverFallback".into(), json!(false));

        assert!(get_redownload_with_suffix(&map));
        assert_eq!(get_existing_file_check_mode(&map), "isrc");
        assert_eq!(get_link_resolver(&map), "songstats");
        assert!(!get_link_resolver_allow_fallback(&map));

        // Invalid types fallback safely
        map.insert("redownloadWithSuffix".into(), json!("invalid_bool"));
        map.insert("existingFileCheckMode".into(), json!(42));
        map.insert("linkResolver".into(), json!(true));
        map.insert("allowResolverFallback".into(), json!("not_a_bool"));

        assert!(!get_redownload_with_suffix(&map));
        assert_eq!(get_existing_file_check_mode(&map), "filename");
        assert_eq!(get_link_resolver(&map), "deezer-songlink");
        assert!(get_link_resolver_allow_fallback(&map));
    }

    #[test]
    fn test_getters_from_store() {
        let dir = tempfile::tempdir().unwrap();
        let config_path = dir.path().join("config.json");
        let store = SettingsStore::new(Some(config_path));

        // Missing file returns defaults
        assert!(!get_redownload_with_suffix_setting(&store));
        assert_eq!(get_existing_file_check_mode_setting(&store), "filename");
        assert_eq!(get_link_resolver_setting(&store), "deezer-songlink");
        assert!(get_link_resolver_allow_fallback_setting(&store));

        // Convenience methods on SettingsStore
        assert!(!store.get_redownload_with_suffix());
        assert_eq!(store.get_existing_file_check_mode(), "filename");
        assert_eq!(store.get_link_resolver(), "deezer-songlink");
        assert!(store.get_link_resolver_allow_fallback());

        // Save settings and verify getters read them correctly
        let mut to_save = Map::new();
        to_save.insert("redownloadWithSuffix".into(), json!(true));
        to_save.insert("existingFileCheckMode".into(), json!("hybrid"));
        to_save.insert("linkResolver".into(), json!("songstats"));
        to_save.insert("allowResolverFallback".into(), json!(false));
        store.save(&to_save).unwrap();

        assert!(store.get_redownload_with_suffix());
        assert_eq!(store.get_existing_file_check_mode(), "hybrid");
        assert_eq!(store.get_link_resolver(), "songstats");
        assert!(!store.get_link_resolver_allow_fallback());
    }
}

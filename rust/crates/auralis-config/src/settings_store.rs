//! Thread-safe settings repository for loading, saving, and migrating config.json.

use crate::atomic_file::write_file_atomic;
use crate::file_acl::restrict_private_file;
use crate::layout::{
    default_settings, flatten_config_settings, marshal_config_settings,
};
use crate::paths::get_config_path;
use crate::sanitizer::sanitize_settings_map;
use auralis_core::error::{AuralisError, Result};
use serde_json::{Map, Value};
use std::path::PathBuf;
use std::sync::Mutex;

#[derive(Debug)]
pub struct SettingsStore {
    config_path: Option<PathBuf>,
    lock: Mutex<()>,
}

impl Default for SettingsStore {
    fn default() -> Self {
        Self::new(None)
    }
}

impl SettingsStore {
    pub fn new(custom_path: Option<PathBuf>) -> Self {
        Self {
            config_path: custom_path,
            lock: Mutex::new(()),
        }
    }

    fn path(&self) -> PathBuf {
        self.config_path
            .clone()
            .unwrap_or_else(get_config_path)
    }

    /// Loads settings from disk.
    /// - If the file does not exist, returns defaults without writing.
    /// - If the file contains corrupt/invalid JSON, returns defaults without overwriting disk.
    /// - Flattens and sanitizes settings in memory.
    pub fn load(&self) -> Result<Map<String, Value>> {
        let _guard = self.lock.lock().unwrap();
        let path = self.path();

        let mut current = default_settings();

        if !path.exists() {
            return Ok(current);
        }

        let data = match std::fs::read(&path) {
            Ok(bytes) => bytes,
            Err(e) => return Err(AuralisError::Io(e)),
        };

        let parsed: Value = match serde_json::from_slice(&data) {
            Ok(val) => val,
            Err(_) => {
                // Return defaults without wiping the corrupt file
                return Ok(current);
            }
        };

        if let Some(obj) = parsed.as_object() {
            let flat = flatten_config_settings(obj);
            for (k, v) in flat {
                current.insert(k, v);
            }
        }

        sanitize_settings_map(&mut current);
        Ok(current)
    }

    /// Saves settings map to disk atomically, applying layout v7 structure and ACL hardening.
    pub fn save(&self, settings: &Map<String, Value>) -> Result<()> {
        let _guard = self.lock.lock().unwrap();
        let path = self.path();

        let mut merged = default_settings();
        for (k, v) in settings {
            merged.insert(k.clone(), v.clone());
        }
        sanitize_settings_map(&mut merged);

        let payload = marshal_config_settings(&merged)?;
        write_file_atomic(&path, &payload)?;
        let _ = restrict_private_file(&path);

        Ok(())
    }

    /// Startup migration: canonicalizes layout if file exists and canonical representation differs.
    pub fn migrate(&self) -> Result<bool> {
        let _guard = self.lock.lock().unwrap();
        let path = self.path();

        if !path.exists() {
            return Ok(false);
        }

        let data = std::fs::read(&path)?;
        let parsed: Value = match serde_json::from_slice(&data) {
            Ok(v) => v,
            Err(_) => return Ok(false), // don't wipe corrupt file during migration
        };

        let Some(obj) = parsed.as_object() else {
            return Ok(false);
        };

        let mut merged = default_settings();
        let flat = flatten_config_settings(obj);
        for (k, v) in flat {
            merged.insert(k, v);
        }
        sanitize_settings_map(&mut merged);

        let canonical = marshal_config_settings(&merged)?;

        let original_trimmed = data.trim_ascii();
        let canonical_trimmed = canonical.trim_ascii();

        if original_trimmed == canonical_trimmed {
            return Ok(false);
        }

        write_file_atomic(&path, &canonical)?;
        let _ = restrict_private_file(&path);
        Ok(true)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_settings_store_lifecycle() {
        let dir = tempfile::tempdir().unwrap();
        let config_path = dir.path().join("config.json");
        let store = SettingsStore::new(Some(config_path.clone()));

        // 1. Missing file returns defaults
        let loaded = store.load().unwrap();
        assert_eq!(loaded["language"], "en");
        assert_eq!(loaded["downloader"], "auto");
        assert!(!config_path.exists());

        // 2. Corrupt JSON file returns defaults and doesn't wipe
        std::fs::write(&config_path, b"invalid json content {").unwrap();
        let fallback = store.load().unwrap();
        assert_eq!(fallback["downloader"], "auto");
        assert_eq!(
            std::fs::read_to_string(&config_path).unwrap(),
            "invalid json content {"
        );

        // 3. Save writes atomically with v7 layout
        let mut new_settings = loaded;
        new_settings.insert("downloader".into(), "tidal".into());
        new_settings.insert("customTidalApi".into(), "https://tidal.example.com/".into());
        store.save(&new_settings).unwrap();

        let disk_content = std::fs::read_to_string(&config_path).unwrap();
        assert!(disk_content.contains("\"configVersion\": 7"));
        assert!(disk_content.contains("\"settingsPage\""));
        assert!(disk_content.contains("\"https://tidal.example.com\""));

        // 4. Load saved content
        let reloaded = store.load().unwrap();
        assert_eq!(reloaded["downloader"], "tidal");
        assert_eq!(reloaded["customTidalApi"], "https://tidal.example.com");

        // 5. Migrate is a no-op when already canonical
        let migrated = store.migrate().unwrap();
        assert!(!migrated);

        // 6. Migrate rewrites legacy flat JSON to layout v7
        let legacy = r#"{"downloader":"qobuz","language":"es"}"#;
        std::fs::write(&config_path, legacy).unwrap();
        let migrated_res = store.migrate().unwrap();
        assert!(migrated_res);

        let canonical_str = std::fs::read_to_string(&config_path).unwrap();
        assert!(canonical_str.contains("\"configVersion\": 7"));
        let parsed_after_migrate = store.load().unwrap();
        assert_eq!(parsed_after_migrate["downloader"], "qobuz");
        assert_eq!(parsed_after_migrate["language"], "es");
    }
}

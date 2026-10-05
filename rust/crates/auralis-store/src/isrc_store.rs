//! Redb-backed cache for Spotify Track ID to ISRC mappings.

use crate::tables::TABLE_SPOTIFY_ISRC;
use auralis_core::error::store::StoreError;
use auralis_core::error::Result;
use auralis_core::models::IsrcCacheEntry;
use redb::{Database, ReadableDatabase};
use std::collections::HashMap;
use std::path::Path;
use std::sync::Arc;

pub struct IsrcStore {
    db: Arc<Database>,
}

impl IsrcStore {
    pub fn open(path: &Path) -> Result<Self> {
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        let db = Database::create(path).map_err(|e| StoreError::Redb(e.to_string()))?;

        let write_tx = db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        {
            let _ = write_tx
                .open_table(TABLE_SPOTIFY_ISRC)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
        }
        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        Ok(Self { db: Arc::new(db) })
    }

    pub fn normalize_track_id(raw: &str) -> String {
        let trimmed = raw.trim();
        if let Some(stripped) = trimmed.strip_prefix("spotify:track:") {
            stripped.trim().to_string()
        } else {
            trimmed.to_string()
        }
    }

    pub fn normalize_isrc(raw: &str) -> String {
        raw.trim().to_uppercase()
    }

    pub fn get(&self, track_id: &str) -> Result<Option<String>> {
        let norm_id = Self::normalize_track_id(track_id);
        if norm_id.is_empty() {
            return Ok(None);
        }

        let read_tx = self
            .db
            .begin_read()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        let table = read_tx
            .open_table(TABLE_SPOTIFY_ISRC)
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        let Some(val) = table
            .get(norm_id.as_str())
            .map_err(|e| StoreError::Redb(e.to_string()))?
        else {
            return Ok(None);
        };

        if let Ok(entry) = serde_json::from_slice::<IsrcCacheEntry>(val.value()) {
            return Ok(Some(entry.isrc));
        }

        if let Ok(raw_str) = std::str::from_utf8(val.value()) {
            return Ok(Some(raw_str.to_string()));
        }

        Ok(None)
    }

    pub fn put(&self, track_id: &str, isrc: &str) -> Result<()> {
        let norm_id = Self::normalize_track_id(track_id);
        let norm_isrc = Self::normalize_isrc(isrc);
        if norm_id.is_empty() || norm_isrc.is_empty() {
            return Ok(());
        }

        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs() as i64)
            .unwrap_or(0);

        let entry = IsrcCacheEntry {
            track_id: norm_id.clone(),
            isrc: norm_isrc,
            updated_at: now,
        };
        let bytes = serde_json::to_vec(&entry)?;

        let write_tx = self
            .db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        {
            let mut table = write_tx
                .open_table(TABLE_SPOTIFY_ISRC)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
            table
                .insert(norm_id.as_str(), bytes.as_slice())
                .map_err(|e| StoreError::Redb(e.to_string()))?;
        }
        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        Ok(())
    }

    pub fn batch_get(&self, track_ids: &[String]) -> Result<HashMap<String, String>> {
        let mut result = HashMap::new();
        let read_tx = self
            .db
            .begin_read()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        let table = read_tx
            .open_table(TABLE_SPOTIFY_ISRC)
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        for id in track_ids {
            let norm_id = Self::normalize_track_id(id);
            if norm_id.is_empty() {
                continue;
            }
            if let Some(val) = table
                .get(norm_id.as_str())
                .map_err(|e| StoreError::Redb(e.to_string()))?
            {
                if let Ok(entry) = serde_json::from_slice::<IsrcCacheEntry>(val.value()) {
                    result.insert(norm_id, entry.isrc);
                } else if let Ok(raw_str) = std::str::from_utf8(val.value()) {
                    result.insert(norm_id, raw_str.to_string());
                }
            }
        }

        Ok(result)
    }

    pub fn batch_put(&self, entries: &[(String, String)]) -> Result<()> {
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs() as i64)
            .unwrap_or(0);

        let write_tx = self
            .db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        {
            let mut table = write_tx
                .open_table(TABLE_SPOTIFY_ISRC)
                .map_err(|e| StoreError::Redb(e.to_string()))?;

            for (track_id, isrc) in entries {
                let norm_id = Self::normalize_track_id(track_id);
                let norm_isrc = Self::normalize_isrc(isrc);
                if norm_id.is_empty() || norm_isrc.is_empty() {
                    continue;
                }
                let entry = IsrcCacheEntry {
                    track_id: norm_id.clone(),
                    isrc: norm_isrc,
                    updated_at: now,
                };
                if let Ok(bytes) = serde_json::to_vec(&entry) {
                    let _ = table.insert(norm_id.as_str(), bytes.as_slice());
                }
            }
        }
        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_isrc_store_cache() {
        let temp = tempfile::tempdir().unwrap();
        let db_path = temp.path().join("isrc_cache.redb");
        let store = IsrcStore::open(&db_path).unwrap();

        assert_eq!(store.get("spotify:track:12345").unwrap(), None);

        store.put("spotify:track:12345", "us-rc1-76-00001").unwrap();
        assert_eq!(
            store.get("12345").unwrap(),
            Some("US-RC1-76-00001".to_string())
        );

        let batch = store
            .batch_get(&["12345".into(), "nonexistent".into()])
            .unwrap();
        assert_eq!(batch.len(), 1);
        assert_eq!(batch.get("12345"), Some(&"US-RC1-76-00001".to_string()));
    }
}

//! Redb-backed history store for download history and fetch history.

use crate::tables::{TABLE_DOWNLOAD_HISTORY, TABLE_FETCH_HISTORY};
use auralis_core::error::store::StoreError;
use auralis_core::error::Result;
use auralis_core::models::{FetchHistoryItem, HistoryItem};
use redb::{Database, ReadableDatabase, ReadableTable, ReadableTableMetadata};
use std::path::Path;
use std::sync::Arc;

pub const MAX_HISTORY_ITEMS: usize = 10000;

pub struct HistoryStore {
    db: Arc<Database>,
}

impl HistoryStore {
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
                .open_table(TABLE_DOWNLOAD_HISTORY)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
            let _ = write_tx
                .open_table(TABLE_FETCH_HISTORY)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
        }
        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        Ok(Self { db: Arc::new(db) })
    }

    /// Loads download history ordered latest first.
    pub fn load_history(&self) -> Result<Vec<HistoryItem>> {
        let read_tx = self
            .db
            .begin_read()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        let table = read_tx
            .open_table(TABLE_DOWNLOAD_HISTORY)
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        let mut items = Vec::new();
        for item in table
            .iter()
            .map_err(|e| StoreError::Redb(e.to_string()))?
        {
            let (_k, v) = item.map_err(|e| StoreError::Redb(e.to_string()))?;
            if let Ok(parsed) = serde_json::from_slice::<HistoryItem>(v.value()) {
                items.push(parsed);
            }
        }

        // Sort descending by timestamp
        items.sort_by_key(|a| std::cmp::Reverse(a.timestamp));
        Ok(items)
    }

    /// Adds a download history item, trimming to MAX_HISTORY_ITEMS if exceeded.
    pub fn add_history_item(&self, item: &HistoryItem) -> Result<()> {
        let write_tx = self
            .db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        {
            let mut table = write_tx
                .open_table(TABLE_DOWNLOAD_HISTORY)
                .map_err(|e| StoreError::Redb(e.to_string()))?;

            let bytes = serde_json::to_vec(item)?;
            table
                .insert(item.id.as_str(), bytes.as_slice())
                .map_err(|e| StoreError::Redb(e.to_string()))?;

            // Prune if over capacity
            let count = table
                .len()
                .map_err(|e| StoreError::Redb(e.to_string()))? as usize;
            if count > MAX_HISTORY_ITEMS {
                let mut all: Vec<(String, i64)> = Vec::with_capacity(count);
                for entry in table
                    .iter()
                    .map_err(|e| StoreError::Redb(e.to_string()))?
                {
                    let (k, v) = entry.map_err(|e| StoreError::Redb(e.to_string()))?;
                    if let Ok(parsed) = serde_json::from_slice::<HistoryItem>(v.value()) {
                        all.push((k.value().to_string(), parsed.timestamp));
                    }
                }
                all.sort_by_key(|(_, ts)| *ts);
                let to_remove = all.len() - MAX_HISTORY_ITEMS;
                for (id, _) in all.into_iter().take(to_remove) {
                    let _ = table.remove(id.as_str());
                }
            }
        }

        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        Ok(())
    }

    /// Clears all download history.
    pub fn clear_history(&self) -> Result<()> {
        let write_tx = self
            .db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        {
            let mut table = write_tx
                .open_table(TABLE_DOWNLOAD_HISTORY)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
            let keys: Vec<String> = table
                .iter()
                .map_err(|e| StoreError::Redb(e.to_string()))?
                .filter_map(|r| r.ok().map(|(k, _)| k.value().to_string()))
                .collect();
            for k in keys {
                let _ = table.remove(k.as_str());
            }
        }

        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        Ok(())
    }

    /// Loads fetch history ordered latest first.
    pub fn load_fetch_history(&self) -> Result<Vec<FetchHistoryItem>> {
        let read_tx = self
            .db
            .begin_read()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        let table = read_tx
            .open_table(TABLE_FETCH_HISTORY)
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        let mut items = Vec::new();
        for item in table
            .iter()
            .map_err(|e| StoreError::Redb(e.to_string()))?
        {
            let (_k, v) = item.map_err(|e| StoreError::Redb(e.to_string()))?;
            if let Ok(parsed) = serde_json::from_slice::<FetchHistoryItem>(v.value()) {
                items.push(parsed);
            }
        }

        items.sort_by_key(|a| std::cmp::Reverse(a.timestamp));
        Ok(items)
    }

    /// Adds a fetch history item, trimming to MAX_HISTORY_ITEMS if exceeded.
    pub fn add_fetch_history_item(&self, item: &FetchHistoryItem) -> Result<()> {
        let write_tx = self
            .db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        {
            let mut table = write_tx
                .open_table(TABLE_FETCH_HISTORY)
                .map_err(|e| StoreError::Redb(e.to_string()))?;

            let bytes = serde_json::to_vec(item)?;
            table
                .insert(item.id.as_str(), bytes.as_slice())
                .map_err(|e| StoreError::Redb(e.to_string()))?;

            let count = table
                .len()
                .map_err(|e| StoreError::Redb(e.to_string()))? as usize;
            if count > MAX_HISTORY_ITEMS {
                let mut all: Vec<(String, i64)> = Vec::with_capacity(count);
                for entry in table
                    .iter()
                    .map_err(|e| StoreError::Redb(e.to_string()))?
                {
                    let (k, v) = entry.map_err(|e| StoreError::Redb(e.to_string()))?;
                    if let Ok(parsed) = serde_json::from_slice::<FetchHistoryItem>(v.value()) {
                        all.push((k.value().to_string(), parsed.timestamp));
                    }
                }
                all.sort_by_key(|(_, ts)| *ts);
                let to_remove = all.len() - MAX_HISTORY_ITEMS;
                for (id, _) in all.into_iter().take(to_remove) {
                    let _ = table.remove(id.as_str());
                }
            }
        }

        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        Ok(())
    }

    /// Clears all fetch history.
    pub fn clear_fetch_history(&self) -> Result<()> {
        let write_tx = self
            .db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        {
            let mut table = write_tx
                .open_table(TABLE_FETCH_HISTORY)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
            let keys: Vec<String> = table
                .iter()
                .map_err(|e| StoreError::Redb(e.to_string()))?
                .filter_map(|r| r.ok().map(|(k, _)| k.value().to_string()))
                .collect();
            for k in keys {
                let _ = table.remove(k.as_str());
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
    fn test_history_store_crud() {
        let temp = tempfile::tempdir().unwrap();
        let db_path = temp.path().join("history.redb");
        let store = HistoryStore::open(&db_path).unwrap();

        let item1 = HistoryItem {
            id: "1".into(),
            spotify_id: "s1".into(),
            title: "Song 1".into(),
            artists: "Artist 1".into(),
            album: "Album 1".into(),
            duration_str: "3:00".into(),
            cover_url: "".into(),
            quality: "FLAC".into(),
            format: "flac".into(),
            path: "/path/1".into(),
            source: "tidal".into(),
            timestamp: 100,
        };

        let item2 = HistoryItem {
            id: "2".into(),
            spotify_id: "s2".into(),
            title: "Song 2".into(),
            artists: "Artist 2".into(),
            album: "Album 2".into(),
            duration_str: "3:30".into(),
            cover_url: "".into(),
            quality: "FLAC".into(),
            format: "flac".into(),
            path: "/path/2".into(),
            source: "tidal".into(),
            timestamp: 200,
        };

        store.add_history_item(&item1).unwrap();
        store.add_history_item(&item2).unwrap();

        let list = store.load_history().unwrap();
        assert_eq!(list.len(), 2);
        // Latest first
        assert_eq!(list[0].id, "2");
        assert_eq!(list[1].id, "1");

        store.clear_history().unwrap();
        assert_eq!(store.load_history().unwrap().len(), 0);
    }
}

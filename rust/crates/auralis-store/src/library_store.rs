//! Redb-backed local music library index store.

use crate::tables::{TABLE_LIBRARY_ENTRIES, TABLE_LIBRARY_ROOTS};
use auralis_core::error::store::StoreError;
use auralis_core::error::Result;
use auralis_core::models::{
    LibraryIndexEntry, LibraryIndexLookupRequest, LibraryIndexRootState,
};
use redb::{Database, ReadableDatabase, ReadableTable};
use std::collections::HashSet;
use std::path::Path;
use std::sync::Arc;

pub struct LibraryIndexStore {
    db: Arc<Database>,
}

impl LibraryIndexStore {
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
                .open_table(TABLE_LIBRARY_ENTRIES)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
            let _ = write_tx
                .open_table(TABLE_LIBRARY_ROOTS)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
        }
        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        Ok(Self { db: Arc::new(db) })
    }

    pub fn normalize_key(path: &str) -> String {
        #[cfg(target_os = "windows")]
        return path.trim().to_lowercase();
        #[cfg(not(target_os = "windows"))]
        return path.trim().to_string();
    }

    pub fn get_entry(&self, path_key: &str) -> Result<Option<LibraryIndexEntry>> {
        let key = Self::normalize_key(path_key);
        let read_tx = self
            .db
            .begin_read()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        let table = read_tx
            .open_table(TABLE_LIBRARY_ENTRIES)
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        if let Some(val) = table
            .get(key.as_str())
            .map_err(|e| StoreError::Redb(e.to_string()))?
        {
            if let Ok(entry) = serde_json::from_slice::<LibraryIndexEntry>(val.value()) {
                return Ok(Some(entry));
            }
        }
        Ok(None)
    }

    pub fn put_entries(&self, entries: &[LibraryIndexEntry]) -> Result<()> {
        let write_tx = self
            .db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        {
            let mut table = write_tx
                .open_table(TABLE_LIBRARY_ENTRIES)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
            for entry in entries {
                let key = Self::normalize_key(&entry.path_key);
                let bytes = serde_json::to_vec(entry)?;
                table
                    .insert(key.as_str(), bytes.as_slice())
                    .map_err(|e| StoreError::Redb(e.to_string()))?;
            }
        }
        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        Ok(())
    }

    pub fn remove_entries(&self, path_keys: &[String]) -> Result<()> {
        let write_tx = self
            .db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        {
            let mut table = write_tx
                .open_table(TABLE_LIBRARY_ENTRIES)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
            for key in path_keys {
                let norm = Self::normalize_key(key);
                let _ = table.remove(norm.as_str());
            }
        }
        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        Ok(())
    }

    pub fn get_root_state(&self, root_key: &str) -> Result<Option<LibraryIndexRootState>> {
        let key = Self::normalize_key(root_key);
        let read_tx = self
            .db
            .begin_read()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        let table = read_tx
            .open_table(TABLE_LIBRARY_ROOTS)
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        if let Some(val) = table
            .get(key.as_str())
            .map_err(|e| StoreError::Redb(e.to_string()))?
        {
            if let Ok(state) = serde_json::from_slice::<LibraryIndexRootState>(val.value()) {
                return Ok(Some(state));
            }
        }
        Ok(None)
    }

    pub fn put_root_state(&self, state: &LibraryIndexRootState) -> Result<()> {
        let key = Self::normalize_key(&state.root_key);
        let bytes = serde_json::to_vec(state)?;
        let write_tx = self
            .db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        {
            let mut table = write_tx
                .open_table(TABLE_LIBRARY_ROOTS)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
            table
                .insert(key.as_str(), bytes.as_slice())
                .map_err(|e| StoreError::Redb(e.to_string()))?;
        }
        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        Ok(())
    }

    pub fn lookup(&self, req: &LibraryIndexLookupRequest) -> Result<Vec<LibraryIndexEntry>> {
        let read_tx = self
            .db
            .begin_read()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        let table = read_tx
            .open_table(TABLE_LIBRARY_ENTRIES)
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        let isrc_target = req.isrc.trim().to_uppercase();
        let spotify_target = req.spotify_id.trim();

        let filename_set: HashSet<String> = req
            .filenames
            .iter()
            .map(|f| {
                let base = Path::new(f)
                    .file_name()
                    .and_then(|s| s.to_str())
                    .unwrap_or(f);
                #[cfg(target_os = "windows")]
                return base.trim().to_lowercase();
                #[cfg(not(target_os = "windows"))]
                return base.trim().to_string();
            })
            .collect();

        let mut matches = Vec::new();
        for item in table
            .iter()
            .map_err(|e| StoreError::Redb(e.to_string()))?
        {
            let (_k, v) = item.map_err(|e| StoreError::Redb(e.to_string()))?;
            let Ok(entry) = serde_json::from_slice::<LibraryIndexEntry>(v.value()) else {
                continue;
            };

            let mut matched = false;

            // Spotify ID match
            if !spotify_target.is_empty() && entry.spotify_id == spotify_target {
                matched = true;
            }

            // Mode-specific match
            if !matched {
                match req.mode.to_lowercase().as_str() {
                    "isrc" => {
                        if !isrc_target.is_empty()
                            && entry.isrc.trim().eq_ignore_ascii_case(&isrc_target)
                        {
                            matched = true;
                        }
                    }
                    "hybrid" => {
                        if !isrc_target.is_empty()
                            && entry.isrc.trim().eq_ignore_ascii_case(&isrc_target)
                        {
                            matched = true;
                        } else {
                            let entry_fn = {
                                #[cfg(target_os = "windows")]
                                {
                                    entry.filename.trim().to_lowercase()
                                }
                                #[cfg(not(target_os = "windows"))]
                                {
                                    entry.filename.trim().to_string()
                                }
                            };
                            if filename_set.contains(&entry_fn) {
                                matched = true;
                            }
                        }
                    }
                    _ => {
                        // Filename mode
                        let entry_fn = {
                            #[cfg(target_os = "windows")]
                            {
                                entry.filename.trim().to_lowercase()
                            }
                            #[cfg(not(target_os = "windows"))]
                            {
                                entry.filename.trim().to_string()
                            }
                        };
                        if filename_set.contains(&entry_fn) {
                            matched = true;
                        }
                    }
                }
            }

            if matched {
                matches.push(entry);
            }
        }

        Ok(matches)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_library_index_store_crud_and_lookup() {
        let temp = tempfile::tempdir().unwrap();
        let db_path = temp.path().join("library_index.redb");
        let store = LibraryIndexStore::open(&db_path).unwrap();

        let entry1 = LibraryIndexEntry {
            root_key: "c:/music".into(),
            path: "c:/music/song1.flac".into(),
            path_key: "c:/music/song1.flac".into(),
            filename: "song1.flac".into(),
            spotify_id: "spot1".into(),
            isrc: "USRC111".into(),
            size: 102400,
            modified_unix_nano: 1000,
            metadata_indexed: true,
        };

        let entry2 = LibraryIndexEntry {
            root_key: "c:/music".into(),
            path: "c:/music/song2.flac".into(),
            path_key: "c:/music/song2.flac".into(),
            filename: "song2.flac".into(),
            spotify_id: "spot2".into(),
            isrc: "USRC222".into(),
            size: 204800,
            modified_unix_nano: 2000,
            metadata_indexed: true,
        };

        store.put_entries(&[entry1.clone(), entry2]).unwrap();

        let fetched = store.get_entry("c:/music/song1.flac").unwrap();
        assert!(fetched.is_some());
        assert_eq!(fetched.unwrap().isrc, "USRC111");

        // Lookup by ISRC
        let lookup_isrc = store
            .lookup(&LibraryIndexLookupRequest {
                mode: "isrc".into(),
                isrc: "usrc111".into(),
                ..Default::default()
            })
            .unwrap();
        assert_eq!(lookup_isrc.len(), 1);
        assert_eq!(lookup_isrc[0].filename, "song1.flac");

        // Lookup by filename
        let lookup_fn = store
            .lookup(&LibraryIndexLookupRequest {
                mode: "filename".into(),
                filenames: vec!["song1.flac".into()],
                ..Default::default()
            })
            .unwrap();
        assert_eq!(lookup_fn.len(), 1);
        assert_eq!(lookup_fn[0].spotify_id, "spot1");
    }
}

//! Legacy bbolt database migration into redb.
//!
//! Migrates legacy .db files into modern .redb files and renames
//! the original file to .db.migrated.bak without deleting it.

use crate::bbolt::BoltReader;
use crate::history_store::HistoryStore;
use crate::isrc_store::IsrcStore;
use crate::library_store::LibraryIndexStore;
use crate::queue_store::QueueStore;
use auralis_core::constants::*;
use auralis_core::error::Result;
use auralis_core::models::{HistoryItem, LibraryIndexEntry, LibraryIndexRootState};
use serde_json::Value;
use std::path::Path;

pub struct MigrationResult {
    pub file_name: String,
    pub migrated: bool,
    pub items_migrated: usize,
}

pub fn migrate_legacy_stores(dir: &Path) -> Result<Vec<MigrationResult>> {
    let mut results = Vec::new();

    // 1. Queue migration
    let queue_db = dir.join(LEGACY_QUEUE_DB_FILE);
    let queue_redb = dir.join(QUEUE_REDB_FILE);
    if queue_db.exists() && !queue_redb.exists() {
        let res = migrate_queue(&queue_db, &queue_redb)?;
        results.push(res);
    }

    // 2. History migration
    let history_db = dir.join(LEGACY_HISTORY_DB_FILE);
    let history_redb = dir.join(HISTORY_REDB_FILE);
    if history_db.exists() && !history_redb.exists() {
        let res = migrate_history(&history_db, &history_redb)?;
        results.push(res);
    }

    // 3. ISRC Cache migration
    let isrc_db = dir.join(LEGACY_ISRC_CACHE_DB_FILE);
    let isrc_redb = dir.join(ISRC_CACHE_REDB_FILE);
    if isrc_db.exists() && !isrc_redb.exists() {
        let res = migrate_isrc(&isrc_db, &isrc_redb)?;
        results.push(res);
    }

    // 4. Library Index migration
    let lib_db = dir.join(LEGACY_LIBRARY_INDEX_DB_FILE);
    let lib_redb = dir.join(LIBRARY_INDEX_REDB_FILE);
    if lib_db.exists() && !lib_redb.exists() {
        let res = migrate_library(&lib_db, &lib_redb)?;
        results.push(res);
    }

    Ok(results)
}

fn finalize_migration(legacy_path: &Path) {
    let bak_path = legacy_path.with_extension("db.migrated.bak");
    let _ = std::fs::rename(legacy_path, bak_path);
}

fn migrate_queue(legacy_path: &Path, target_path: &Path) -> Result<MigrationResult> {
    let data = std::fs::read(legacy_path)?;
    let reader = BoltReader::new(&data).map_err(auralis_core::error::AuralisError::Store)?;

    let items_bucket = reader
        .read_bucket(LEGACY_QUEUE_ITEMS_BUCKET.as_bytes())
        .unwrap_or_default();
    let meta_bucket = reader
        .read_bucket(LEGACY_QUEUE_META_BUCKET.as_bytes())
        .unwrap_or_default();

    let mut items = Vec::new();
    for (_, val) in items_bucket {
        if let Ok(item) = serde_json::from_slice::<Value>(&val) {
            items.push(item);
        }
    }

    let order: Vec<String> = if let Some(order_bytes) = meta_bucket.get(b"order".as_slice()) {
        serde_json::from_slice(order_bytes).unwrap_or_default()
    } else {
        Vec::new()
    };

    let items_count = items.len();
    let store = QueueStore::open(target_path)?;
    store.save(&items, &order)?;

    finalize_migration(legacy_path);

    Ok(MigrationResult {
        file_name: LEGACY_QUEUE_DB_FILE.into(),
        migrated: true,
        items_migrated: items_count,
    })
}

fn migrate_history(legacy_path: &Path, target_path: &Path) -> Result<MigrationResult> {
    let data = std::fs::read(legacy_path)?;
    let reader = BoltReader::new(&data).map_err(auralis_core::error::AuralisError::Store)?;

    let history_bucket = reader
        .read_bucket(LEGACY_DOWNLOAD_HISTORY_BUCKET.as_bytes())
        .unwrap_or_default();

    let store = HistoryStore::open(target_path)?;
    let mut count = 0;
    for (_, val) in history_bucket {
        if let Ok(item) = serde_json::from_slice::<HistoryItem>(&val) {
            if store.add_history_item(&item).is_ok() {
                count += 1;
            }
        }
    }

    finalize_migration(legacy_path);

    Ok(MigrationResult {
        file_name: LEGACY_HISTORY_DB_FILE.into(),
        migrated: true,
        items_migrated: count,
    })
}

fn migrate_isrc(legacy_path: &Path, target_path: &Path) -> Result<MigrationResult> {
    let data = std::fs::read(legacy_path)?;
    let reader = BoltReader::new(&data).map_err(auralis_core::error::AuralisError::Store)?;

    let isrc_bucket = reader
        .read_bucket(LEGACY_ISRC_CACHE_BUCKET.as_bytes())
        .unwrap_or_default();

    let store = IsrcStore::open(target_path)?;
    let mut count = 0;
    for (k, val) in isrc_bucket {
        let track_id = String::from_utf8_lossy(&k).to_string();
        let isrc = if let Ok(entry) = serde_json::from_slice::<Value>(&val) {
            entry
                .get("isrc")
                .and_then(|v| v.as_str())
                .unwrap_or("")
                .to_string()
        } else {
            String::from_utf8_lossy(&val).to_string()
        };

        if !track_id.is_empty() && !isrc.is_empty() && store.put(&track_id, &isrc).is_ok() {
            count += 1;
        }
    }

    finalize_migration(legacy_path);

    Ok(MigrationResult {
        file_name: LEGACY_ISRC_CACHE_DB_FILE.into(),
        migrated: true,
        items_migrated: count,
    })
}

fn migrate_library(legacy_path: &Path, target_path: &Path) -> Result<MigrationResult> {
    let data = std::fs::read(legacy_path)?;
    let reader = BoltReader::new(&data).map_err(auralis_core::error::AuralisError::Store)?;

    let entries_bucket = reader
        .read_bucket(LEGACY_LIBRARY_INDEX_ENTRIES_BUCKET.as_bytes())
        .unwrap_or_default();
    let roots_bucket = reader
        .read_bucket(LEGACY_LIBRARY_INDEX_ROOTS_BUCKET.as_bytes())
        .unwrap_or_default();

    let store = LibraryIndexStore::open(target_path)?;

    let mut entries = Vec::new();
    for (_, val) in entries_bucket {
        if let Ok(entry) = serde_json::from_slice::<LibraryIndexEntry>(&val) {
            entries.push(entry);
        }
    }
    let entries_count = entries.len();
    store.put_entries(&entries)?;

    for (_, val) in roots_bucket {
        if let Ok(root) = serde_json::from_slice::<LibraryIndexRootState>(&val) {
            let _ = store.put_root_state(&root);
        }
    }

    finalize_migration(legacy_path);

    Ok(MigrationResult {
        file_name: LEGACY_LIBRARY_INDEX_DB_FILE.into(),
        migrated: true,
        items_migrated: entries_count,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::bbolt::build_test_bbolt_db;

    #[test]
    fn test_migrate_legacy_stores_empty() {
        let temp = tempfile::tempdir().unwrap();
        let res = migrate_legacy_stores(temp.path()).unwrap();
        assert!(res.is_empty());
    }

    #[test]
    fn test_finalize_migration_preserves_backup() {
        let temp = tempfile::tempdir().unwrap();
        let legacy_file = temp.path().join("history.db");
        std::fs::write(&legacy_file, b"test bolt data").unwrap();

        finalize_migration(&legacy_file);

        assert!(!legacy_file.exists());
        let backup = temp.path().join("history.db.migrated.bak");
        assert!(backup.exists());
        assert_eq!(std::fs::read_to_string(&backup).unwrap(), "test bolt data");
    }

    #[test]
    fn test_migrate_legacy_stores_with_real_fixtures() {
        let temp = tempfile::tempdir().unwrap();
        let dir = temp.path();

        // 1. Build queue.db with items and order
        let queue_item = serde_json::json!({
            "id": "item-123",
            "title": "Stairway to Heaven",
            "artist": "Led Zeppelin"
        });
        let queue_item_bytes = serde_json::to_vec(&queue_item).unwrap();
        let queue_order_bytes = serde_json::to_vec(&vec!["item-123".to_string()]).unwrap();
        let queue_db_bytes = build_test_bbolt_db(&[
            (LEGACY_QUEUE_ITEMS_BUCKET.as_bytes(), &[(b"item-123", &queue_item_bytes)]),
            (LEGACY_QUEUE_META_BUCKET.as_bytes(), &[(b"order", &queue_order_bytes)]),
        ]);
        std::fs::write(dir.join(LEGACY_QUEUE_DB_FILE), &queue_db_bytes).unwrap();

        // 2. Build history.db
        let history_item = HistoryItem {
            id: "hist-001".into(),
            spotify_id: "spotify-123".into(),
            title: "Bohemian Rhapsody".into(),
            artists: "Queen".into(),
            album: "A Night at the Opera".into(),
            duration_str: "5:55".into(),
            cover_url: "".into(),
            quality: "FLAC".into(),
            format: "flac".into(),
            path: "/music/queen.flac".into(),
            source: "tidal".into(),
            timestamp: 1700000000,
        };
        let hist_bytes = serde_json::to_vec(&history_item).unwrap();
        let history_db_bytes = build_test_bbolt_db(&[
            (LEGACY_DOWNLOAD_HISTORY_BUCKET.as_bytes(), &[(b"hist-001", &hist_bytes)]),
        ]);
        std::fs::write(dir.join(LEGACY_HISTORY_DB_FILE), &history_db_bytes).unwrap();

        // 3. Build isrc_cache.db
        let isrc_db_bytes = build_test_bbolt_db(&[
            (LEGACY_ISRC_CACHE_BUCKET.as_bytes(), &[(b"spotify:track:abc1", b"USPR37300012")]),
        ]);
        std::fs::write(dir.join(LEGACY_ISRC_CACHE_DB_FILE), &isrc_db_bytes).unwrap();

        // 4. Build library_index.db
        let lib_entry = LibraryIndexEntry {
            root_key: "/music".into(),
            path: "/music/eagles.flac".into(),
            path_key: "eagles.flac".into(),
            filename: "eagles.flac".into(),
            spotify_id: "spotify-eagles".into(),
            isrc: "USPR37300012".into(),
            size: 40000000,
            modified_unix_nano: 1700000000000000000,
            metadata_indexed: true,
        };
        let lib_bytes = serde_json::to_vec(&lib_entry).unwrap();
        let root_state = LibraryIndexRootState {
            root: "/music".into(),
            root_key: "/music".into(),
            level: 1,
            scanned_at: 1700000002,
        };
        let root_bytes = serde_json::to_vec(&root_state).unwrap();
        let lib_db_bytes = build_test_bbolt_db(&[
            (LEGACY_LIBRARY_INDEX_ENTRIES_BUCKET.as_bytes(), &[(b"eagles.flac", &lib_bytes)]),
            (LEGACY_LIBRARY_INDEX_ROOTS_BUCKET.as_bytes(), &[(b"/music", &root_bytes)]),
        ]);
        std::fs::write(dir.join(LEGACY_LIBRARY_INDEX_DB_FILE), &lib_db_bytes).unwrap();

        // Run migration
        let results = migrate_legacy_stores(dir).expect("migration should succeed");
        assert_eq!(results.len(), 4);

        // Verify QueueStore round-trip
        let q_store = QueueStore::open(&dir.join(QUEUE_REDB_FILE)).unwrap();
        let (items, order) = q_store.load().unwrap();
        assert_eq!(items.len(), 1);
        assert_eq!(order, vec!["item-123"]);

        // Verify HistoryStore round-trip
        let h_store = HistoryStore::open(&dir.join(HISTORY_REDB_FILE)).unwrap();
        let history = h_store.load_history().unwrap();
        assert_eq!(history.len(), 1);
        assert_eq!(history[0].id, "hist-001");
        assert_eq!(history[0].title, "Bohemian Rhapsody");

        // Verify IsrcStore round-trip
        let i_store = IsrcStore::open(&dir.join(ISRC_CACHE_REDB_FILE)).unwrap();
        assert_eq!(
            i_store.get("spotify:track:abc1").unwrap(),
            Some("USPR37300012".into())
        );

        // Verify LibraryIndexStore round-trip
        let l_store = LibraryIndexStore::open(&dir.join(LIBRARY_INDEX_REDB_FILE)).unwrap();
        let entry = l_store.get_entry("eagles.flac").unwrap().expect("entry exists");
        assert_eq!(entry.filename, "eagles.flac");
        assert_eq!(entry.spotify_id, "spotify-eagles");
        assert_eq!(l_store.get_root_state("/music").unwrap().unwrap().scanned_at, 1700000002);

        // Verify original .db files are renamed to .db.migrated.bak and NEVER deleted
        assert!(!dir.join(LEGACY_QUEUE_DB_FILE).exists());
        assert!(!dir.join(LEGACY_HISTORY_DB_FILE).exists());
        assert!(!dir.join(LEGACY_ISRC_CACHE_DB_FILE).exists());
        assert!(!dir.join(LEGACY_LIBRARY_INDEX_DB_FILE).exists());

        let q_bak = dir.join("queue.db.migrated.bak");
        let h_bak = dir.join("history.db.migrated.bak");
        let i_bak = dir.join("isrc_cache.db.migrated.bak");
        let l_bak = dir.join("library_index.db.migrated.bak");

        assert!(q_bak.exists());
        assert!(h_bak.exists());
        assert!(i_bak.exists());
        assert!(l_bak.exists());

        // Verify backup bytes are identical to original bbolt data
        assert_eq!(std::fs::read(&q_bak).unwrap(), queue_db_bytes);
        assert_eq!(std::fs::read(&h_bak).unwrap(), history_db_bytes);
        assert_eq!(std::fs::read(&i_bak).unwrap(), isrc_db_bytes);
        assert_eq!(std::fs::read(&l_bak).unwrap(), lib_db_bytes);
    }

    #[test]
    fn test_corrupt_payload_surfaces_error_and_preserves_bytes() {
        let temp = tempfile::tempdir().unwrap();
        let dir = temp.path();

        let corrupt_data = b"RANDOM_CORRUPT_NOT_BOLT_DATABASE_BYTES_12345678";
        let queue_file = dir.join(LEGACY_QUEUE_DB_FILE);
        std::fs::write(&queue_file, corrupt_data).unwrap();

        // Migration must return Err
        let res = migrate_legacy_stores(dir);
        assert!(res.is_err(), "corrupt legacy database must surface an error");

        // Original file must still exist with the exact same bytes (NOT deleted)
        assert!(queue_file.exists());
        assert_eq!(std::fs::read(&queue_file).unwrap(), corrupt_data);

        // Target redb file must NOT have been created
        assert!(!dir.join(QUEUE_REDB_FILE).exists());

        // Backup file must NOT exist (migration was aborted)
        assert!(!dir.join("queue.db.migrated.bak").exists());
    }
}

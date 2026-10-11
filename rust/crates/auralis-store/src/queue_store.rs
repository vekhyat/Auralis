//! Redb-backed download queue store.

use crate::tables::{TABLE_QUEUE_ITEMS, TABLE_QUEUE_META};
use auralis_core::error::store::StoreError;
use auralis_core::error::Result;
use auralis_core::models::QueueApplyRequest;
use redb::{Database, ReadableDatabase, ReadableTable};
use serde_json::Value;
use std::collections::HashSet;
use std::path::Path;
use std::sync::Arc;

pub struct QueueStore {
    db: Arc<Database>,
}

impl QueueStore {
    pub fn open(path: &Path) -> Result<Self> {
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        let db = Database::create(path).map_err(|e| StoreError::Redb(e.to_string()))?;

        // Ensure tables exist
        let write_tx = db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        {
            let _ = write_tx
                .open_table(TABLE_QUEUE_ITEMS)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
            let _ = write_tx
                .open_table(TABLE_QUEUE_META)
                .map_err(|e| StoreError::Redb(e.to_string()))?;
        }
        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        Ok(Self { db: Arc::new(db) })
    }

    /// Loads the queue items and their order.
    pub fn load(&self) -> Result<(Vec<Value>, Vec<String>)> {
        let read_tx = self
            .db
            .begin_read()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        let meta_table = read_tx
            .open_table(TABLE_QUEUE_META)
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        let order: Vec<String> = if let Some(order_bytes) = meta_table
            .get("order")
            .map_err(|e| StoreError::Redb(e.to_string()))?
        {
            serde_json::from_slice(order_bytes.value()).unwrap_or_default()
        } else {
            Vec::new()
        };

        let items_table = read_tx
            .open_table(TABLE_QUEUE_ITEMS)
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        let mut items_map = std::collections::HashMap::new();
        for item in items_table
            .iter()
            .map_err(|e| StoreError::Redb(e.to_string()))?
        {
            let (key, val) = item.map_err(|e| StoreError::Redb(e.to_string()))?;
            if let Ok(v) = serde_json::from_slice::<Value>(val.value()) {
                items_map.insert(key.value().to_string(), v);
            }
        }

        let mut ordered_items = Vec::new();
        let mut seen = HashSet::new();

        for id in &order {
            if let Some(item) = items_map.get(id) {
                ordered_items.push(item.clone());
                seen.insert(id.clone());
            }
        }

        // Add remaining items that were not present in order
        for (id, item) in &items_map {
            if !seen.contains(id) {
                ordered_items.push(item.clone());
            }
        }

        Ok((ordered_items, order))
    }

    /// Completely replaces queue items and order.
    pub fn save(&self, items: &[Value], order: &[String]) -> Result<()> {
        let write_tx = self
            .db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        {
            let mut items_table = write_tx
                .open_table(TABLE_QUEUE_ITEMS)
                .map_err(|e| StoreError::Redb(e.to_string()))?;

            // Clear existing items
            let existing_keys: Vec<String> = items_table
                .iter()
                .map_err(|e| StoreError::Redb(e.to_string()))?
                .filter_map(|r| r.ok().map(|(k, _)| k.value().to_string()))
                .collect();

            for k in existing_keys {
                let _ = items_table.remove(k.as_str());
            }

            for item in items {
                if let Some(id) = item.get("id").and_then(|v| v.as_str()) {
                    let bytes = serde_json::to_vec(item)?;
                    items_table
                        .insert(id, bytes.as_slice())
                        .map_err(|e| StoreError::Redb(e.to_string()))?;
                }
            }

            let mut meta_table = write_tx
                .open_table(TABLE_QUEUE_META)
                .map_err(|e| StoreError::Redb(e.to_string()))?;

            let order_bytes = serde_json::to_vec(order)?;
            meta_table
                .insert("order", order_bytes.as_slice())
                .map_err(|e| StoreError::Redb(e.to_string()))?;
        }

        write_tx
            .commit()
            .map_err(|e| StoreError::Redb(e.to_string()))?;
        Ok(())
    }

    /// Atomically applies upserts, removals, and order updates.
    pub fn apply(&self, request: &QueueApplyRequest) -> Result<()> {
        let write_tx = self
            .db
            .begin_write()
            .map_err(|e| StoreError::Redb(e.to_string()))?;

        {
            let mut items_table = write_tx
                .open_table(TABLE_QUEUE_ITEMS)
                .map_err(|e| StoreError::Redb(e.to_string()))?;

            for rem in &request.remove {
                let _ = items_table.remove(rem.as_str());
            }

            for item in &request.upsert {
                if let Some(id) = item.get("id").and_then(|v| v.as_str()) {
                    let bytes = serde_json::to_vec(item)?;
                    items_table
                        .insert(id, bytes.as_slice())
                        .map_err(|e| StoreError::Redb(e.to_string()))?;
                }
            }

            if !request.order.is_empty() {
                let mut meta_table = write_tx
                    .open_table(TABLE_QUEUE_META)
                    .map_err(|e| StoreError::Redb(e.to_string()))?;
                let order_bytes = serde_json::to_vec(&request.order)?;
                meta_table
                    .insert("order", order_bytes.as_slice())
                    .map_err(|e| StoreError::Redb(e.to_string()))?;
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
    use serde_json::json;

    #[test]
    fn test_queue_store_crud_and_apply() {
        let temp = tempfile::tempdir().unwrap();
        let db_path = temp.path().join("queue.redb");
        let store = QueueStore::open(&db_path).unwrap();

        let item1 = json!({"id": "track-1", "title": "Song 1"});
        let item2 = json!({"id": "track-2", "title": "Song 2"});

        store
            .save(
                &[item1.clone(), item2.clone()],
                &["track-2".into(), "track-1".into()],
            )
            .unwrap();

        let (loaded, order) = store.load().unwrap();
        assert_eq!(order, vec!["track-2", "track-1"]);
        assert_eq!(loaded.len(), 2);
        assert_eq!(loaded[0]["id"], "track-2");
        assert_eq!(loaded[1]["id"], "track-1");

        // Apply upsert and remove
        let item3 = json!({"id": "track-3", "title": "Song 3"});
        let apply_req = QueueApplyRequest {
            upsert: vec![item3],
            remove: vec!["track-1".into()],
            order: vec!["track-3".into(), "track-2".into()],
        };
        store.apply(&apply_req).unwrap();

        let (loaded_after, order_after) = store.load().unwrap();
        assert_eq!(order_after, vec!["track-3", "track-2"]);
        assert_eq!(loaded_after.len(), 2);
        assert_eq!(loaded_after[0]["id"], "track-3");
        assert_eq!(loaded_after[1]["id"], "track-2");
    }
}

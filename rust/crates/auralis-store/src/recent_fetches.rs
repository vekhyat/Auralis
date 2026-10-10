//! Thread-safe atomic JSON store for recent fetches.

use auralis_config::write_file_atomic;
use auralis_core::error::store::StoreError;
use auralis_core::error::Result;
use auralis_core::models::RecentFetchItem;
use std::path::PathBuf;
use std::sync::Mutex;

const MAX_RECENT_FETCHES: usize = 200;

pub struct RecentFetchesStore {
    path: PathBuf,
    lock: Mutex<()>,
}

impl RecentFetchesStore {
    pub fn new(path: PathBuf) -> Self {
        Self {
            path,
            lock: Mutex::new(()),
        }
    }

    pub fn load(&self) -> Result<Vec<RecentFetchItem>> {
        let _guard = self.lock.lock().unwrap();
        if !self.path.exists() {
            return Ok(Vec::new());
        }

        let data = std::fs::read(&self.path)?;
        let trimmed = std::str::from_utf8(&data)
            .map_err(|e| auralis_core::error::AuralisError::Store(StoreError::LegacyParse(e.to_string())))?
            .trim();
        if trimmed.is_empty() {
            return Ok(Vec::new());
        }

        let items: Vec<RecentFetchItem> = serde_json::from_str(trimmed)?;
        Ok(items)
    }

    pub fn save(&self, items: &[RecentFetchItem]) -> Result<()> {
        let _guard = self.lock.lock().unwrap();
        let payload = serde_json::to_vec_pretty(items)?;
        write_file_atomic(&self.path, &payload)?;
        Ok(())
    }

    pub fn add(&self, item: RecentFetchItem) -> Result<()> {
        let _guard = self.lock.lock().unwrap();
        let mut items = if self.path.exists() {
            let data = std::fs::read(&self.path)?;
            let trimmed = std::str::from_utf8(&data)
                .map_err(|e| auralis_core::error::AuralisError::Store(StoreError::LegacyParse(e.to_string())))?
                .trim();
            if !trimmed.is_empty() {
                serde_json::from_str::<Vec<RecentFetchItem>>(trimmed)?
            } else {
                Vec::new()
            }
        } else {
            Vec::new()
        };

        // Remove existing duplicate by URL or ID
        items.retain(|existing| {
            (!existing.url.is_empty() && existing.url != item.url)
                && (!existing.id.is_empty() && existing.id != item.id)
        });

        // Insert at beginning
        items.insert(0, item);

        // Cap at MAX_RECENT_FETCHES
        if items.len() > MAX_RECENT_FETCHES {
            items.truncate(MAX_RECENT_FETCHES);
        }

        let payload = serde_json::to_vec_pretty(&items)?;
        write_file_atomic(&self.path, &payload)?;
        Ok(())
    }

    pub fn clear(&self) -> Result<()> {
        let _guard = self.lock.lock().unwrap();
        let empty: [RecentFetchItem; 0] = [];
        let payload = serde_json::to_vec_pretty(&empty)?;
        write_file_atomic(&self.path, &payload)?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_recent_fetches_crud() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("recent_fetches.json");
        let store = RecentFetchesStore::new(path);

        assert_eq!(store.load().unwrap().len(), 0);

        let item1 = RecentFetchItem {
            id: "track1".into(),
            url: "https://open.spotify.com/track/track1".into(),
            item_type: "track".into(),
            name: "Song 1".into(),
            artist: "Artist 1".into(),
            image: "".into(),
            is_explicit: false,
            timestamp: 1000,
        };
        store.add(item1).unwrap();
        assert_eq!(store.load().unwrap().len(), 1);

        let item2 = RecentFetchItem {
            id: "track2".into(),
            url: "https://open.spotify.com/track/track2".into(),
            item_type: "track".into(),
            name: "Song 2".into(),
            artist: "Artist 2".into(),
            image: "".into(),
            is_explicit: false,
            timestamp: 2000,
        };
        store.add(item2).unwrap();
        assert_eq!(store.load().unwrap().len(), 2);

        store.clear().unwrap();
        assert_eq!(store.load().unwrap().len(), 0);
    }

    #[test]
    fn test_corrupt_payload_never_wipes_store() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("recent_fetches.json");
        let corrupt_data = b"<!DOCTYPE html><html>corrupted json data</html>";
        std::fs::write(&path, corrupt_data).unwrap();

        let store = RecentFetchesStore::new(path.clone());

        // load must fail and surface error
        assert!(store.load().is_err());

        // add must fail and surface error without wiping
        let item = RecentFetchItem {
            id: "item1".into(),
            url: "http://example.com".into(),
            item_type: "track".into(),
            name: "T".into(),
            artist: "A".into(),
            image: "".into(),
            is_explicit: false,
            timestamp: 100,
        };
        assert!(store.add(item).is_err());

        // verify original bytes are completely intact
        assert_eq!(std::fs::read(&path).unwrap(), corrupt_data);
    }
}

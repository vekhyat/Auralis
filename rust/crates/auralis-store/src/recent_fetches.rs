//! Thread-safe atomic JSON store for recent fetches (port of `backend/recent_fetches.go`).

use auralis_config::write_file_atomic;
use auralis_core::error::store::StoreError;
use auralis_core::error::Result;
use auralis_core::models::RecentFetchItem;
use std::path::PathBuf;
use std::sync::Mutex;

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

    /// Mirrors `LoadRecentFetches`: a missing, blank or `null` file is an empty list.
    pub fn load(&self) -> Result<Vec<RecentFetchItem>> {
        let _guard = self.lock.lock().unwrap_or_else(|e| e.into_inner());
        let data = match std::fs::read(&self.path) {
            Ok(data) => data,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
            Err(e) => return Err(e.into()),
        };
        let text = std::str::from_utf8(&data).map_err(|e| {
            auralis_core::error::AuralisError::Store(StoreError::LegacyParse(e.to_string()))
        })?;
        if text.trim().is_empty() {
            return Ok(Vec::new());
        }
        let items: Option<Vec<RecentFetchItem>> = serde_json::from_str(text)?;
        Ok(items.unwrap_or_default())
    }

    /// Mirrors `SaveRecentFetches`: writes the list as 2-space indented JSON, atomically.
    pub fn save(&self, items: &[RecentFetchItem]) -> Result<()> {
        let _guard = self.lock.lock().unwrap_or_else(|e| e.into_inner());
        let payload = serde_json::to_vec_pretty(items)?;
        write_file_atomic(&self.path, &payload)?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn item(id: &str, name: &str, timestamp: i64) -> RecentFetchItem {
        RecentFetchItem {
            id: id.into(),
            url: format!("https://open.spotify.com/track/{id}"),
            item_type: "track".into(),
            name: name.into(),
            artist: "Artist".into(),
            image: "".into(),
            is_explicit: false,
            timestamp,
        }
    }

    // Port of TestSaveRecentFetchesIsAtomic.
    #[test]
    fn save_recent_fetches_is_atomic() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("recent_fetches.json");
        let store = RecentFetchesStore::new(path.clone());

        let items = vec![item("1", "Song", 10)];
        store.save(&items).unwrap();
        let loaded = store.load().unwrap();
        assert_eq!(loaded, items);

        store.save(&[]).unwrap();
        assert!(store.load().unwrap().is_empty());
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "[]");

        let leftovers: Vec<_> = std::fs::read_dir(temp.path())
            .unwrap()
            .filter_map(|e| e.ok())
            .filter(|e| e.file_name().to_string_lossy().contains(".tmp"))
            .collect();
        assert!(leftovers.is_empty(), "temp files left behind");
        assert!(path.exists());
    }

    #[test]
    fn missing_blank_and_null_files_load_as_empty() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("recent_fetches.json");
        let store = RecentFetchesStore::new(path.clone());
        assert!(store.load().unwrap().is_empty());

        std::fs::write(&path, "  \n").unwrap();
        assert!(store.load().unwrap().is_empty());

        std::fs::write(&path, "null").unwrap();
        assert!(store.load().unwrap().is_empty());
    }

    #[test]
    fn saves_every_item_without_dedupe_or_cap() {
        let temp = tempfile::tempdir().unwrap();
        let store = RecentFetchesStore::new(temp.path().join("recent_fetches.json"));
        let mut items: Vec<_> = (0..250).map(|i| item("", &format!("S{i}"), i)).collect();
        items[0].is_explicit = true;
        store.save(&items).unwrap();
        let loaded = store.load().unwrap();
        assert_eq!(loaded.len(), 250);
        assert_eq!(loaded, items);
    }

    #[test]
    fn writes_go_json_shape() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("recent_fetches.json");
        let store = RecentFetchesStore::new(path.clone());
        let mut explicit = item("2", "B", 2);
        explicit.is_explicit = true;
        store.save(&[item("1", "A", 1), explicit]).unwrap();
        let json: serde_json::Value =
            serde_json::from_str(&std::fs::read_to_string(&path).unwrap()).unwrap();
        assert_eq!(json[0]["type"], "track");
        assert!(json[0].get("is_explicit").is_none());
        assert_eq!(json[1]["is_explicit"], true);
        assert!(std::fs::read_to_string(&path)
            .unwrap()
            .starts_with("[\n  {\n    \"id\": \"1\""));
    }

    #[test]
    fn missing_fields_decode_to_zero_values() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("recent_fetches.json");
        std::fs::write(&path, r#"[{"id":"x","url":"u"}]"#).unwrap();
        let loaded = RecentFetchesStore::new(path).load().unwrap();
        assert_eq!(loaded.len(), 1);
        assert_eq!(loaded[0].id, "x");
        assert_eq!(loaded[0].name, "");
        assert_eq!(loaded[0].timestamp, 0);
        assert!(!loaded[0].is_explicit);
    }

    #[test]
    fn corrupt_payload_surfaces_error_and_keeps_bytes() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("recent_fetches.json");
        let corrupt = b"<!DOCTYPE html><html>corrupted json data</html>";
        std::fs::write(&path, corrupt).unwrap();

        let store = RecentFetchesStore::new(path.clone());
        assert!(store.load().is_err());
        assert_eq!(std::fs::read(&path).unwrap(), corrupt);
    }
}

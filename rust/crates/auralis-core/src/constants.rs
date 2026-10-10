//! Application-wide constants for Auralis.

pub const APP_NAME: &str = "Auralis";
pub const APP_SCHEME: &str = "auralis";

pub const CONFIG_LAYOUT_VERSION: u32 = 7;
pub const CONFIG_FILE_NAME: &str = "config.json";
pub const RECENT_FETCHES_FILE_NAME: &str = "recent_fetches.json";
pub const LEGACY_TIDAL_API_CACHE_FILE: &str = "tidal-api-urls.json";

// Database filenames
pub const LEGACY_QUEUE_DB_FILE: &str = "queue.db";
pub const QUEUE_REDB_FILE: &str = "queue.redb";
pub const QUEUE_ITEMS_TABLE: &str = "download_queue_items";
pub const QUEUE_META_TABLE: &str = "download_queue_meta";
pub const QUEUE_ORDER_KEY: &str = "order";

pub const LEGACY_HISTORY_DB_FILE: &str = "history.db";
pub const HISTORY_REDB_FILE: &str = "history.redb";
pub const DOWNLOAD_HISTORY_TABLE: &str = "download_history";
pub const FETCH_HISTORY_TABLE: &str = "fetch_history";
pub const MAX_HISTORY_ITEMS: usize = 10_000;

pub const LEGACY_ISRC_CACHE_DB_FILE: &str = "isrc_cache.db";
pub const ISRC_CACHE_REDB_FILE: &str = "isrc_cache.redb";
pub const SPOTIFY_TRACK_ISRC_TABLE: &str = "spotify_track_isrc";

pub const LEGACY_LIBRARY_INDEX_DB_FILE: &str = "library_index.db";
pub const LIBRARY_INDEX_REDB_FILE: &str = "library_index.redb";
pub const LIBRARY_INDEX_ENTRIES_TABLE: &str = "library_index_entries";
pub const LIBRARY_INDEX_ROOTS_TABLE: &str = "library_index_roots";
pub const LIBRARY_INDEX_LEVEL_FILENAME: i32 = 1;
pub const LIBRARY_INDEX_LEVEL_METADATA: i32 = 2;
pub const LIBRARY_INDEX_MIN_AUDIO_SIZE: u64 = 100 * 1024; // 100 KB

// Legacy bbolt bucket names
pub const LEGACY_QUEUE_ITEMS_BUCKET: &str = "DownloadQueueItems";
pub const LEGACY_QUEUE_META_BUCKET: &str = "DownloadQueueMeta";
pub const LEGACY_DOWNLOAD_HISTORY_BUCKET: &str = "DownloadHistory";
pub const LEGACY_FETCH_HISTORY_BUCKET: &str = "FetchHistory";
pub const LEGACY_ISRC_CACHE_BUCKET: &str = "SpotifyTrackISRC";
pub const LEGACY_LIBRARY_INDEX_ENTRIES_BUCKET: &str = "LibraryIndexEntries";
pub const LEGACY_LIBRARY_INDEX_ROOTS_BUCKET: &str = "LibraryIndexRoots";

// Supported audio extensions for library indexing
pub const SUPPORTED_AUDIO_EXTENSIONS: &[&str] = &[
    "flac", "mp3", "m4a", "aac", "alac", "wav", "ogg", "opus", "aiff",
];

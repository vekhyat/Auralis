//! Redb table definitions for Auralis storage.

use redb::TableDefinition;

// Queue tables
pub const TABLE_QUEUE_ITEMS: TableDefinition<&str, &[u8]> =
    TableDefinition::new("download_queue_items");
pub const TABLE_QUEUE_META: TableDefinition<&str, &[u8]> =
    TableDefinition::new("download_queue_meta");

// History tables
pub const TABLE_DOWNLOAD_HISTORY: TableDefinition<&str, &[u8]> =
    TableDefinition::new("download_history");
pub const TABLE_FETCH_HISTORY: TableDefinition<&str, &[u8]> =
    TableDefinition::new("fetch_history");

// ISRC Cache tables
pub const TABLE_SPOTIFY_ISRC: TableDefinition<&str, &[u8]> =
    TableDefinition::new("spotify_track_isrc");

// Library Index tables
pub const TABLE_LIBRARY_ENTRIES: TableDefinition<&str, &[u8]> =
    TableDefinition::new("library_index_entries");
pub const TABLE_LIBRARY_ROOTS: TableDefinition<&str, &[u8]> =
    TableDefinition::new("library_index_roots");

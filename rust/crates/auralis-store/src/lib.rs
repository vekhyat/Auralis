//! Persistence and storage engine for Auralis backed by redb and atomic JSON.

pub mod bbolt;
pub mod history_store;
pub mod isrc_store;
pub mod library_store;
pub mod migration;
pub mod queue_store;
pub mod recent_fetches;
pub mod tables;

pub use bbolt::BoltReader;
pub use history_store::{HistoryStore, MAX_HISTORY_ITEMS};
pub use isrc_store::IsrcStore;
pub use library_store::LibraryIndexStore;
pub use migration::{migrate_legacy_stores, MigrationResult};
pub use queue_store::QueueStore;
pub use recent_fetches::RecentFetchesStore;

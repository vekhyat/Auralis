//! Library Doctor: library scans, rule validation, fix planning, journaled apply & undo,
//! playback target profiles, and M3U8 playlist export (complete port of `backend/library`).
//!
//! # Architecture and Injected Dependencies
//!
//! - **Library Index Sync**:
//!   In the original Go codebase, `backend.MoveLibraryIndexFile(oldPath, newPath)` was invoked
//!   whenever an audio file or sidecar was moved. Since the database index / app layer is not yet
//!   ported in Phase 1, `auralis-library` abstracts this via the [`LibraryIndexSync`] trait
//!   and supports closures `Fn(&str, &str) + Send + Sync`. Callers (e.g. the app layer) can supply
//!   a custom sync handler or use [`NoOpIndexSync`] when operating in CLI or headless mode.
//!
//! - **Tagging and Codec Engine**:
//!   Tag reading, writing, and cover extraction are handled through [`lofty`]. Low-level tag mapping
//!   and normalization are located in `tag_io`. If advanced tag writing features (like ID3v2 frame-level
//!   custom text or replaygain synth) need consolidation with `auralis-tagger`, see `NEEDS.md` / crate notes.

pub mod journal;
pub mod playlists;
pub mod profiles;
pub mod rules;
pub mod scan;
pub mod tag_io;
pub mod types;

// Re-exports of core types
pub use journal::{
    append_journal, apply, last_batch, read_journal, undo_last, within_root, LibraryIndexSync,
    NoOpIndexSync,
};
pub use playlists::{playlist_line, tracks_from_paths, write_playlist};
pub use profiles::{
    builtin_profiles, primary_artist, profile_by_id, profiles, ArtistMode, Profile, TrackFields,
    PROFILE_MEDIASERVER, PROFILE_MEDIASTORE, PROFILE_POWERAMP, PROFILE_ROCKBOX,
};
pub use rules::{
    build_plan, calculate_health_score, count_albums, detect_all, rule_by_id, rules, Rule, RULES,
};
pub use scan::scan_library;
pub use tag_io::{first_tag, read_tags};
pub use types::{
    ApplyResult, Issue, JournalEntry, LibraryReport, Operation, Plan, PlaylistMode, PlaylistResult,
    PlaylistSkip, PlaylistTrack, Scan, ScanProgress, Severity, Track,
};

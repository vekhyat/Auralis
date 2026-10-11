//! Audio tagging, cover art extraction/embedding, lyrics, and ReplayGain for Auralis.

pub mod cover;
pub mod error;
pub mod lyrics;
pub mod metadata;
pub mod replaygain;
pub mod tagger;

pub use cover::{
    build_cover_filename, create_lofty_picture, create_lofty_picture_from_bytes,
    download_cover_art, upgrade_spotify_cover_url,
};
pub use error::{Result, TaggerError};
pub use lyrics::{
    fetch_lrclib_lyrics, format_lrc, parse_lrc, save_lrc_file, LyricsLine, LyricsResponse,
};
pub use metadata::{AudioMetadata, MetadataTagSelection};
pub use replaygain::{
    analyze_track_replaygain, calculate_album_replaygain, format_gain_db, format_peak,
    AlbumReplayGain, TrackReplayGain, REPLAYGAIN_REFERENCE_LUFS,
};
pub use tagger::{apply_replaygain_tags, read_audio_tags, write_audio_tags, TagWriteMode};

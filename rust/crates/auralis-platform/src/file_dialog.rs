//! File dialog options and result post-processing.
//!
//! Port of `backend/file_dialog.go` and `backend/folder.go`.
//!
//! # App-Layer Hook Note
//! In Go/Wails, dialogs were displayed directly via `wails/v2/pkg/runtime.OpenFileDialog`,
//! `OpenMultipleFilesDialog`, and `OpenDirectoryDialog`.
//! In Tauri v2, native dialogs are displayed by the app layer via `tauri-plugin-dialog`:
//! ```text
//! let files = app.dialog().file().add_filter(...).pick_files();
//! ```
//! This module provides the dialog configuration (titles, display names, glob patterns,
//! file extension lists, default directories) and result post-processing as plain, pure-Rust
//! functions and structures. Tauri command handlers call these functions to retrieve dialog
//! options and format results without coupling `auralis-platform` to the Tauri runtime.

use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

/// A file filter descriptor for native file picker dialogs.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct DialogFilter {
    /// Human-readable label (e.g. "Audio Files (*.flac, *.mp3, ...)")
    pub display_name: String,
    /// Semicolon-delimited pattern string (e.g. "*.flac;*.mp3")
    pub pattern: String,
    /// List of file extensions without leading dot (e.g. `["flac", "mp3"]`)
    pub extensions: Vec<String>,
}

/// Options for opening a file selection dialog.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct OpenFileDialogOptions {
    /// Dialog title.
    pub title: String,
    /// Optional default initial directory.
    pub default_directory: Option<PathBuf>,
    /// File type filters.
    pub filters: Vec<DialogFilter>,
    /// Whether multiple files can be selected.
    pub multiple: bool,
}

/// Options for opening a directory selection dialog.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct OpenDirectoryDialogOptions {
    /// Dialog title.
    pub title: String,
    /// Optional default initial directory.
    pub default_directory: Option<PathBuf>,
}

/// Returns dialog options for selecting multiple audio files.
///
/// Port of `backend.SelectMultipleFiles(ctx)`.
pub fn select_multiple_files_dialog_options() -> OpenFileDialogOptions {
    OpenFileDialogOptions {
        title: "Select Audio Files".to_string(),
        default_directory: None,
        filters: vec![
            DialogFilter {
                display_name: "Audio Files (*.flac, *.mp3, *.m4a, *.wav, *.ogg, *.opus, *.aiff, *.ape, *.wv, *.mpc)".to_string(),
                pattern: "*.flac;*.mp3;*.m4a;*.mp4;*.m4b;*.aac;*.wav;*.aiff;*.aif;*.ogg;*.opus;*.ape;*.wv;*.mpc".to_string(),
                extensions: vec![
                    "flac".into(), "mp3".into(), "m4a".into(), "mp4".into(), "m4b".into(),
                    "aac".into(), "wav".into(), "aiff".into(), "aif".into(), "ogg".into(),
                    "opus".into(), "ape".into(), "wv".into(), "mpc".into(),
                ],
            },
            DialogFilter {
                display_name: "MP3 Files (*.mp3)".to_string(),
                pattern: "*.mp3".to_string(),
                extensions: vec!["mp3".into()],
            },
            DialogFilter {
                display_name: "M4A Files (*.m4a)".to_string(),
                pattern: "*.m4a".to_string(),
                extensions: vec!["m4a".into()],
            },
            DialogFilter {
                display_name: "FLAC Files (*.flac)".to_string(),
                pattern: "*.flac".to_string(),
                extensions: vec!["flac".into()],
            },
            DialogFilter {
                display_name: "AAC Files (*.aac)".to_string(),
                pattern: "*.aac".to_string(),
                extensions: vec!["aac".into()],
            },
            DialogFilter {
                display_name: "WAV Files (*.wav)".to_string(),
                pattern: "*.wav".to_string(),
                extensions: vec!["wav".into()],
            },
            DialogFilter {
                display_name: "OGG Files (*.ogg)".to_string(),
                pattern: "*.ogg".to_string(),
                extensions: vec!["ogg".into()],
            },
            DialogFilter {
                display_name: "All Files (*.*)".to_string(),
                pattern: "*.*".to_string(),
                extensions: vec!["*".into()],
            },
        ],
        multiple: true,
    }
}

/// Returns dialog options for selecting an output directory.
///
/// Port of `backend.SelectOutputDirectory(ctx)`.
pub fn select_output_directory_dialog_options() -> OpenDirectoryDialogOptions {
    OpenDirectoryDialogOptions {
        title: "Select Output Directory".to_string(),
        default_directory: None,
    }
}

/// Returns dialog options for selecting a download folder with fallback default path.
///
/// Port of `backend.SelectFolderDialog(ctx, defaultPath)`.
pub fn select_folder_dialog_options(default_path: Option<&Path>) -> OpenDirectoryDialogOptions {
    let resolved_default = default_path
        .map(|p| p.to_path_buf())
        .or_else(|| Some(crate::app_paths::get_default_music_path()));

    OpenDirectoryDialogOptions {
        title: "Select Download Folder".to_string(),
        default_directory: resolved_default,
    }
}

/// Returns dialog options for selecting a single audio file for analysis.
///
/// Port of `backend.SelectFileDialog(ctx)`.
pub fn select_file_dialog_options() -> OpenFileDialogOptions {
    OpenFileDialogOptions {
        title: "Select Audio File for Analysis".to_string(),
        default_directory: None,
        filters: vec![
            DialogFilter {
                display_name: "Audio Files (*.flac;*.mp3;*.m4a;*.aac)".to_string(),
                pattern: "*.flac;*.mp3;*.m4a;*.aac".to_string(),
                extensions: vec!["flac".into(), "mp3".into(), "m4a".into(), "aac".into()],
            },
            DialogFilter {
                display_name: "FLAC Audio Files (*.flac)".to_string(),
                pattern: "*.flac".to_string(),
                extensions: vec!["flac".into()],
            },
            DialogFilter {
                display_name: "MP3 Audio Files (*.mp3)".to_string(),
                pattern: "*.mp3".to_string(),
                extensions: vec!["mp3".into()],
            },
            DialogFilter {
                display_name: "M4A Audio Files (*.m4a)".to_string(),
                pattern: "*.m4a".to_string(),
                extensions: vec!["m4a".into()],
            },
            DialogFilter {
                display_name: "AAC Audio Files (*.aac)".to_string(),
                pattern: "*.aac".to_string(),
                extensions: vec!["aac".into()],
            },
            DialogFilter {
                display_name: "All Files (*.*)".to_string(),
                pattern: "*.*".to_string(),
                extensions: vec!["*".into()],
            },
        ],
        multiple: false,
    }
}

/// Returns dialog options for selecting image or video files.
///
/// Port of `backend.SelectImageVideoDialog(ctx)`.
pub fn select_image_video_dialog_options() -> OpenFileDialogOptions {
    OpenFileDialogOptions {
        title: "Select Image or Video".to_string(),
        default_directory: None,
        filters: vec![
            DialogFilter {
                display_name: "Supported Files (*.jpg, *.png, *.mp4, *.mov, ...)".to_string(),
                pattern: "*.jpg;*.jpeg;*.png;*.gif;*.webp;*.mp4;*.mkv;*.webm;*.mov".to_string(),
                extensions: vec![
                    "jpg".into(),
                    "jpeg".into(),
                    "png".into(),
                    "gif".into(),
                    "webp".into(),
                    "mp4".into(),
                    "mkv".into(),
                    "webm".into(),
                    "mov".into(),
                ],
            },
            DialogFilter {
                display_name: "All Files (*.*)".to_string(),
                pattern: "*.*".to_string(),
                extensions: vec!["*".into()],
            },
        ],
        multiple: true,
    }
}

/// Post-processes dialog file paths to string representations.
pub fn process_selected_files(files: Vec<PathBuf>) -> Vec<String> {
    files
        .into_iter()
        .map(|p| p.to_string_lossy().to_string())
        .collect()
}

/// Post-processes dialog directory path to optional string.
pub fn process_selected_directory(dir: Option<PathBuf>) -> Option<String> {
    dir.map(|p| p.to_string_lossy().to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_select_multiple_files_dialog_options() {
        let opts = select_multiple_files_dialog_options();
        assert_eq!(opts.title, "Select Audio Files");
        assert!(opts.multiple);
        assert_eq!(opts.filters.len(), 8);
        assert_eq!(
            opts.filters[0].display_name,
            "Audio Files (*.flac, *.mp3, *.m4a, *.wav, *.ogg, *.opus, *.aiff, *.ape, *.wv, *.mpc)"
        );
        assert!(opts.filters[0].extensions.contains(&"flac".to_string()));
        assert!(opts.filters[0].extensions.contains(&"mp3".to_string()));
        assert_eq!(opts.filters[7].display_name, "All Files (*.*)");
    }

    #[test]
    fn test_select_output_directory_dialog_options() {
        let opts = select_output_directory_dialog_options();
        assert_eq!(opts.title, "Select Output Directory");
        assert_eq!(opts.default_directory, None);
    }

    #[test]
    fn test_process_selected_paths() {
        let paths = vec![
            PathBuf::from("/music/song1.flac"),
            PathBuf::from("/music/song2.mp3"),
        ];
        let processed = process_selected_files(paths);
        assert_eq!(processed.len(), 2);
        assert!(processed[0].ends_with("song1.flac"));
        assert!(processed[1].ends_with("song2.mp3"));

        let dir = PathBuf::from("music").join("album");
        assert_eq!(
            process_selected_directory(Some(dir.clone())),
            Some(dir.to_string_lossy().to_string())
        );
        assert_eq!(process_selected_directory(None), None);
    }
}

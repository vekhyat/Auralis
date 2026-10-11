//! `auralis-platform`
//!
//! OS-level integration primitives for Auralis.
//!
//! # Platform Integration & Tauri Hook
//!
//! This crate implements the platform-specific capabilities previously located in
//! `backend/app_paths.go`, `backend/filemanager.go`, `backend/file_dialog.go`,
//! `backend/fileicon_darwin.go`, `backend/fileicon_stub.go`, `backend/protocol_windows.go`,
//! and `backend/protocol_other.go`.
//!
//! ## App-Layer File Dialogs
//! In the Go/Wails architecture, native system dialogs were shown using Wails runtime APIs
//! (`runtime.OpenFileDialog`, etc.). In Tauri v2, native dialogs are opened from Tauri
//! command handlers via `tauri-plugin-dialog`. This crate deliberately remains decoupled from
//! the Tauri runtime: it provides the dialog options (titles, file filter lists, default directories)
//! and result post-processing functions in [`file_dialog`], which Tauri commands pass directly
//! into `app.dialog().file()` or `app.dialog().file().pick_folder()`.
//!
//! ## Modules
//! - [`app_paths`]: Application directory resolution, creation, and webview profile isolation.
//! - [`filemanager`]: Directory listing, audio scanning, metadata reading, and file renaming.
//! - [`file_dialog`]: Pure-data configurations and handlers for native file and folder dialogs.
//! - [`fileicon`]: Setting custom macOS file icons using AppKit (with no-op stub on other platforms).
//! - [`protocol`]: Registering `auralis://` and `spotiflac://` custom URL protocol handlers.
//! - [`error`]: Typed errors for platform operations.

pub mod app_paths;
pub mod error;
pub mod file_dialog;
pub mod fileicon;
pub mod filemanager;
pub mod protocol;

// Top-level re-exports for convenience
pub use app_paths::{
    ensure_app_data_dir, ensure_app_dir, get_app_dir, isolated_webview_profile_path,
    resolve_app_data_dir, APP_DATA_DIR_ENV,
};
pub use error::{AppPathsError, FileIconError, FileManagerError, ProtocolError};
pub use file_dialog::{
    process_selected_directory, process_selected_files, select_file_dialog_options,
    select_folder_dialog_options, select_image_video_dialog_options,
    select_multiple_files_dialog_options, select_output_directory_dialog_options, DialogFilter,
    OpenDirectoryDialogOptions, OpenFileDialogOptions,
};
pub use fileicon::{resize_image_for_icon, set_macos_file_icon_from_image};
pub use filemanager::{
    assign_preferred_upc, classify_upc_description, first_preferred_ffprobe_upc_value,
    first_preferred_normalized_upc_value, generate_filename, get_file_sizes, list_audio_files,
    list_directory, preview_rename, read_audio_metadata, read_flac_metadata, read_m4a_metadata,
    read_metadata_with_ffprobe, read_mp3_metadata, rename_files, sanitize_filename_for_rename,
    AudioMetadata, FileInfo, RenamePreview, RenameResult, AUDIO_EXTENSIONS, FFPROBE_UPC_TAG_KEYS,
    NORMALIZED_UPC_TAG_KEYS, PREFERRED_UPC_TAG_KEY,
};
pub use protocol::register_auralis_protocol;
#[cfg(windows)]
pub use protocol::{register_url_protocol, register_url_protocol_with_root};

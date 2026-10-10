//! Configuration, path resolution, atomic file operations, and settings layout for Auralis.

pub mod atomic_file;
pub mod file_acl;
pub mod layout;
pub mod paths;
pub mod sanitizer;
pub mod settings_store;

pub use atomic_file::{move_file_replace, write_file_atomic};
pub use file_acl::restrict_private_file;
pub use layout::{
    categorize_config_settings, default_settings, flatten_config_settings,
    get_known_config_keys, marshal_config_settings, CONFIG_SECTIONS,
};
pub use paths::{
    ensure_app_dir, get_config_path, get_db_path, get_default_music_path,
    get_recent_fetches_path, get_webview_profile_path, resolve_app_dir,
};
pub use sanitizer::{
    normalize_custom_tidal_api, normalize_existing_file_check_mode,
    normalize_link_resolver, sanitize_auto_order, sanitize_downloader,
    sanitize_settings_map,
};
pub use settings_store::SettingsStore;

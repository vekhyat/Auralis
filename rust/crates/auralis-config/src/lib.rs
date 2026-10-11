//! Configuration, path resolution, atomic file operations, and settings layout for Auralis.

pub mod atomic_file;
pub mod credentials;
pub mod file_acl;
pub mod layout;
pub mod paths;
pub mod sanitizer;
pub mod settings;
pub mod settings_store;

pub use atomic_file::{move_file_replace, write_file_atomic};
pub use file_acl::restrict_private_file;
pub use layout::{
    categorize_config_settings, config_path_orders, default_settings, flatten_config_settings,
    get_known_config_keys, has_known_config_settings, make_ordered_config_value,
    marshal_config_settings, ConfigSectionDef, OrderedConfigEntry, OrderedConfigObject,
    OrderedConfigValue, CONFIG_SECTIONS,
};
pub use paths::{
    ensure_app_dir, get_config_path, get_db_path, get_default_music_path, get_recent_fetches_path,
    get_webview_profile_path, resolve_app_dir,
};
pub use sanitizer::{
    normalize_custom_tidal_api, normalize_existing_file_check_mode, normalize_link_resolver,
    sanitize_auto_order, sanitize_downloader, sanitize_settings_map,
};
pub use settings::{
    cleanup_legacy_tidal_public_api_state, cleanup_legacy_tidal_public_api_state_in,
    get_existing_file_check_mode, get_existing_file_check_mode_setting, get_link_resolver,
    get_link_resolver_allow_fallback, get_link_resolver_allow_fallback_setting,
    get_link_resolver_setting, get_redownload_with_suffix, get_redownload_with_suffix_setting,
    DEFAULT_EXISTING_FILE_CHECK_MODE, LEGACY_TIDAL_API_CACHE_FILE, LINK_RESOLVER_DEEZER_SONGLINK,
    LINK_RESOLVER_PROVIDER_DEEZER_SONGLINK, LINK_RESOLVER_PROVIDER_SONGSTATS,
    LINK_RESOLVER_SONGSTATS,
};
pub use settings_store::SettingsStore;

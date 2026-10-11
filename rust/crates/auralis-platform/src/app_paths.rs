//! Application path resolution and creation.
//!
//! Port of `backend/app_paths.go`. Functionality already in `auralis-config::paths`
//! (`resolve_app_dir`, `get_config_path`, `get_recent_fetches_path`, `get_default_music_path`,
//! `get_db_path`) is re-exported here to avoid duplication.

use crate::error::AppPathsError;
use std::path::{Path, PathBuf};

// Re-exports from auralis-config
pub use auralis_config::paths::{
    get_config_path, get_db_path, get_default_music_path, get_recent_fetches_path, resolve_app_dir,
};

/// Environment variable overriding the application data directory.
pub const APP_DATA_DIR_ENV: &str = "AURALIS_APP_DIR";

/// Returns the root application directory (`~/.auralis` or `$AURALIS_APP_DIR`).
///
/// Port of Go `backend.GetAppDir()`. If `$AURALIS_APP_DIR` is set and non-empty,
/// it must be an absolute path; otherwise returns `AppPathsError::RelativeAppDir`.
pub fn get_app_dir() -> Result<PathBuf, AppPathsError> {
    if let Ok(dir) = std::env::var(APP_DATA_DIR_ENV) {
        let trimmed = dir.trim();
        if !trimmed.is_empty() {
            let p = Path::new(trimmed);
            if !p.is_absolute() {
                return Err(AppPathsError::RelativeAppDir);
            }
            return Ok(p.to_path_buf());
        }
    }

    let home = dirs::home_dir()
        .ok_or_else(|| AppPathsError::HomeDirNotFound("home directory not found".to_string()))?;
    Ok(home.join(".auralis"))
}

/// Resolves the application data directory.
///
/// Port of Go `backend.ResolveAppDataDir()`. Alias for [`get_app_dir`].
pub fn resolve_app_data_dir() -> Result<PathBuf, AppPathsError> {
    get_app_dir()
}

/// Returns the path to the isolated webview profile directory if an alternate
/// app directory is configured via `$AURALIS_APP_DIR`, or `None` if unconfigured.
///
/// Port of Go `backend.IsolatedWebviewProfilePath()`.
pub fn isolated_webview_profile_path() -> Result<Option<PathBuf>, AppPathsError> {
    let custom = std::env::var(APP_DATA_DIR_ENV).unwrap_or_default();
    let trimmed = custom.trim();
    if trimmed.is_empty() {
        return Ok(None);
    }

    let dir = get_app_dir()?;
    Ok(Some(dir.join("webview")))
}

/// Ensures the application directory exists, creating it if needed.
///
/// Port of Go `backend.EnsureAppDir()`. Alias for [`ensure_app_data_dir`].
pub fn ensure_app_dir() -> Result<PathBuf, AppPathsError> {
    ensure_app_data_dir()
}

/// Ensures the application data directory exists with permissions, creating it if needed.
///
/// Port of Go `backend.EnsureAppDataDir()`.
pub fn ensure_app_data_dir() -> Result<PathBuf, AppPathsError> {
    let dir = resolve_app_data_dir()?;
    std::fs::create_dir_all(&dir).map_err(AppPathsError::CreateAppDir)?;
    Ok(dir)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Mutex;

    // Use a lock to serialize tests that touch the environment variable
    static ENV_LOCK: Mutex<()> = Mutex::new(());

    #[test]
    fn test_alternate_app_directory_also_isolates_webview_profile() {
        let _guard = ENV_LOCK.lock().unwrap();

        // 1. Unset/empty AURALIS_APP_DIR -> profile is None (Go: empty string)
        unsafe {
            std::env::set_var(APP_DATA_DIR_ENV, "");
        }
        let profile = isolated_webview_profile_path().expect("default profile query");
        assert_eq!(profile, None, "default profile must be None when unset");

        // 2. Absolute tempdir -> isolated profile at dir / "webview"
        let temp = tempfile::tempdir().expect("create temp dir");
        let dir = temp.path().to_path_buf();
        unsafe {
            std::env::set_var(APP_DATA_DIR_ENV, &dir);
        }

        let profile = isolated_webview_profile_path().expect("isolated profile query");
        assert_eq!(
            profile,
            Some(dir.join("webview")),
            "isolated profile must match dir/webview"
        );

        let app_dir = get_app_dir().expect("get app dir");
        assert_eq!(app_dir, dir, "app data dir must match temp dir");

        let app_data_dir = resolve_app_data_dir().expect("resolve app data dir");
        assert_eq!(app_data_dir, dir);

        // Test ensure_app_data_dir
        let sub_dir = dir.join("nested").join("test_app");
        unsafe {
            std::env::set_var(APP_DATA_DIR_ENV, &sub_dir);
        }
        let ensured = ensure_app_dir().expect("ensure app dir");
        assert_eq!(ensured, sub_dir);
        assert!(sub_dir.is_dir());

        // 3. Relative path override must be rejected
        unsafe {
            std::env::set_var(APP_DATA_DIR_ENV, "relative-directory");
        }
        let err = isolated_webview_profile_path();
        assert!(err.is_err(), "relative profile override must be rejected");

        let app_err = get_app_dir();
        assert!(
            app_err.is_err(),
            "relative app dir override must be rejected"
        );

        // Cleanup
        unsafe {
            std::env::remove_var(APP_DATA_DIR_ENV);
        }
    }
}

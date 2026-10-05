//! Path resolution for Auralis application directories and files.

use auralis_core::constants::{CONFIG_FILE_NAME, RECENT_FETCHES_FILE_NAME};
use std::path::PathBuf;

/// Resolves the root application directory.
/// Priority:
/// 1. $AURALIS_APP_DIR environment variable (if non-empty)
/// 2. User config directory (%APPDATA%\Auralis on Windows, ~/.config/auralis on Linux)
/// 3. User home directory fallback (~/.auralis)
/// 4. Current directory fallback (./.auralis)
pub fn resolve_app_dir() -> PathBuf {
    if let Ok(custom) = std::env::var("AURALIS_APP_DIR") {
        let trimmed = custom.trim();
        if !trimmed.is_empty() {
            return PathBuf::from(trimmed);
        }
    }

    if let Some(config_dir) = dirs::config_dir() {
        #[cfg(target_os = "windows")]
        return config_dir.join("Auralis");

        #[cfg(not(target_os = "windows"))]
        return config_dir.join("auralis");
    }

    if let Some(home_dir) = dirs::home_dir() {
        return home_dir.join(".auralis");
    }

    PathBuf::from(".auralis")
}

/// Ensures the application directory exists, creating it if necessary.
pub fn ensure_app_dir() -> std::io::Result<PathBuf> {
    let dir = resolve_app_dir();
    std::fs::create_dir_all(&dir)?;
    Ok(dir)
}

/// Returns the path to the configuration file (`config.json`).
pub fn get_config_path() -> PathBuf {
    resolve_app_dir().join(CONFIG_FILE_NAME)
}

/// Returns the path to the recent fetches file (`recent_fetches.json`).
pub fn get_recent_fetches_path() -> PathBuf {
    resolve_app_dir().join(RECENT_FETCHES_FILE_NAME)
}

/// Returns the path to the isolated webview profile directory.
pub fn get_webview_profile_path() -> PathBuf {
    resolve_app_dir().join("webview_profile")
}

/// Returns the path to a database file with given name inside the app dir.
pub fn get_db_path(filename: &str) -> PathBuf {
    resolve_app_dir().join(filename)
}

/// Returns the default music directory for the current user.
pub fn get_default_music_path() -> PathBuf {
    if let Some(audio_dir) = dirs::audio_dir() {
        if audio_dir.exists() {
            return audio_dir;
        }
    }

    if let Some(home_dir) = dirs::home_dir() {
        let music = home_dir.join("Music");
        if music.exists() {
            return music;
        }
        return home_dir;
    }

    PathBuf::from(".")
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::Path;

    #[test]
    fn test_app_dir_override() {
        let temp = tempfile::tempdir().unwrap();
        let temp_path = temp.path().to_string_lossy().to_string();

        unsafe {
            std::env::set_var("AURALIS_APP_DIR", &temp_path);
        }
        let resolved = resolve_app_dir();
        assert_eq!(resolved, Path::new(&temp_path));

        let config_path = get_config_path();
        assert_eq!(config_path, Path::new(&temp_path).join("config.json"));

        let fetches_path = get_recent_fetches_path();
        assert_eq!(fetches_path, Path::new(&temp_path).join("recent_fetches.json"));

        unsafe {
            std::env::remove_var("AURALIS_APP_DIR");
        }
    }
}

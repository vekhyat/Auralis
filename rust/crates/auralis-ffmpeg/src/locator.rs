//! FFmpeg and FFprobe binary location and verification.

use crate::error::{FfmpegError, Result};
use crate::process::configure_command;
use auralis_config::paths::resolve_app_dir;
use std::path::{Path, PathBuf};
use std::process::Command;

pub fn executable_name(base: &str) -> String {
    if cfg!(windows) {
        format!("{}.exe", base)
    } else {
        base.to_string()
    }
}

pub fn validate_executable(path: &Path) -> Result<()> {
    if !path.is_absolute() {
        return Err(FfmpegError::ValidationFailed(
            path.display().to_string(),
            "Path must be absolute".into(),
        ));
    }

    let meta = std::fs::metadata(path).map_err(|e| {
        FfmpegError::ValidationFailed(
            path.display().to_string(),
            format!("Cannot stat file: {}", e),
        )
    })?;

    if !meta.is_file() {
        return Err(FfmpegError::ValidationFailed(
            path.display().to_string(),
            "Path is not a regular file".into(),
        ));
    }

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        if meta.permissions().mode() & 0o111 == 0 {
            return Err(FfmpegError::ValidationFailed(
                path.display().to_string(),
                "File is not marked executable".into(),
            ));
        }
    }

    let Some(file_name) = path.file_name().and_then(|s| s.to_str()) else {
        return Err(FfmpegError::InvalidName("Invalid UTF-8 filename".into()));
    };

    let valid_names = ["ffmpeg", "ffmpeg.exe", "ffprobe", "ffprobe.exe"];
    if !valid_names.contains(&file_name.to_lowercase().as_str()) {
        return Err(FfmpegError::InvalidName(file_name.to_string()));
    }

    Ok(())
}

pub fn run_version_check(path: &Path) -> Result<()> {
    let mut cmd = Command::new(path);
    cmd.arg("-version");
    configure_command(&mut cmd);

    let output = cmd.output().map_err(|e| {
        FfmpegError::ExecutionFailed(format!("Failed to run version check: {}", e))
    })?;

    if !output.status.success() {
        return Err(FfmpegError::ExecutionFailed(format!(
            "Version check failed with status {}",
            output.status
        )));
    }

    Ok(())
}

pub fn resolve_executable(name: &str) -> Result<PathBuf> {
    let binary_name = executable_name(name);

    // 1. Check local app directory
    let app_dir = resolve_app_dir();
    let local_path = app_dir.join(&binary_name);
    if local_path.is_file()
        && validate_executable(&local_path).is_ok()
        && run_version_check(&local_path).is_ok()
    {
        return Ok(local_path);
    }

    // Check migration from .spotiflac-next if available
    if let Some(parent) = app_dir.parent() {
        let next_dir = parent.join(".spotiflac-next");
        let next_path = next_dir.join(&binary_name);
        if next_path.is_file() {
            let _ = std::fs::copy(&next_path, &local_path);
            if validate_executable(&local_path).is_ok()
                && run_version_check(&local_path).is_ok()
            {
                return Ok(local_path);
            }
        }
    }

    // 2. Check macOS brew locations
    #[cfg(target_os = "macos")]
    {
        for prefix in &["/opt/homebrew/bin", "/usr/local/bin"] {
            let brew_candidate = PathBuf::from(prefix).join(&binary_name);
            if brew_candidate.is_file()
                && validate_executable(&brew_candidate).is_ok()
                && run_version_check(&brew_candidate).is_ok()
            {
                return Ok(brew_candidate);
            }
        }
    }

    // 3. Check system PATH
    if let Ok(system_path) = which::which(&binary_name) {
        if validate_executable(&system_path).is_ok() && run_version_check(&system_path).is_ok() {
            return Ok(system_path);
        }
    }

    Err(FfmpegError::NotFound(binary_name))
}

pub fn get_ffmpeg_path() -> Result<PathBuf> {
    resolve_executable("ffmpeg")
}

pub fn get_ffprobe_path() -> Result<PathBuf> {
    resolve_executable("ffprobe")
}

pub fn is_ffmpeg_installed() -> bool {
    get_ffmpeg_path().is_ok()
}

pub fn is_ffprobe_installed() -> bool {
    get_ffprobe_path().is_ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_executable_name() {
        if cfg!(windows) {
            assert_eq!(executable_name("ffmpeg"), "ffmpeg.exe");
        } else {
            assert_eq!(executable_name("ffmpeg"), "ffmpeg");
        }
    }

    #[test]
    fn test_validate_executable_invalid_name() {
        let temp = tempfile::tempdir().unwrap();
        let invalid_file = temp.path().join("malicious.exe");
        std::fs::write(&invalid_file, b"test").unwrap();
        assert!(validate_executable(&invalid_file).is_err());
    }
}

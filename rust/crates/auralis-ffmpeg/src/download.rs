//! Bundled FFmpeg & FFprobe download, SHA-256 verification, and extraction.

use crate::error::{FfmpegError, Result};
use crate::locator::executable_name;
use auralis_config::paths::resolve_app_dir;
use serde::Deserialize;
use sha2::{Digest, Sha256};
use std::io::{Read, Seek};
use std::path::{Path, PathBuf};

const RELEASES_API_URL: &str = "https://api.github.com/repos/spotbye/Dependencies/releases";
const RELEASE_DOWNLOAD_URL: &str =
    "https://github.com/spotbye/Dependencies/releases/download";
const RELEASE_TAG_PREFIX: &str = "FFmpeg-";

#[derive(Debug, Deserialize)]
pub struct GithubReleaseAsset {
    pub name: String,
    #[serde(default)]
    pub digest: String,
}

#[derive(Debug, Deserialize)]
pub struct GithubRelease {
    pub tag_name: String,
    pub draft: bool,
    pub prerelease: bool,
    pub assets: Vec<GithubReleaseAsset>,
}

pub fn get_platform_asset_names() -> Result<(&'static str, &'static str)> {
    #[cfg(target_os = "windows")]
    return Ok(("ffmpeg-windows.zip", "ffprobe-windows.zip"));

    #[cfg(target_os = "linux")]
    {
        #[cfg(target_arch = "x86_64")]
        return Ok(("ffmpeg-linux-amd64.zip", "ffprobe-linux-amd64.zip"));

        #[cfg(target_arch = "aarch64")]
        return Ok(("ffmpeg-linux-arm64v8.zip", "ffprobe-linux-arm64v8.zip"));

        #[cfg(not(any(target_arch = "x86_64", target_arch = "aarch64")))]
        return Err(FfmpegError::ValidationFailed(
            "unsupported linux arch".into(),
            std::env::consts::ARCH.into(),
        ));
    }

    #[cfg(target_os = "macos")]
    {
        #[cfg(target_arch = "x86_64")]
        return Ok(("ffmpeg-macos-amd64.zip", "ffprobe-macos-amd64.zip"));

        #[cfg(target_arch = "aarch64")]
        return Ok(("ffmpeg-macos-arm64.zip", "ffprobe-macos-arm64.zip"));

        #[cfg(not(any(target_arch = "x86_64", target_arch = "aarch64")))]
        return Err(FfmpegError::ValidationFailed(
            "unsupported macos arch".into(),
            std::env::consts::ARCH.into(),
        ));
    }
}

pub fn verify_archive_sha256(data: &[u8], expected_hex: &str) -> Result<()> {
    let mut hasher = Sha256::new();
    hasher.update(data);
    let calculated = hex::encode(hasher.finalize());

    let normalized_expected = expected_hex
        .trim()
        .strip_prefix("sha256:")
        .unwrap_or(expected_hex)
        .trim()
        .to_lowercase();

    if calculated != normalized_expected {
        return Err(FfmpegError::ChecksumMismatch(
            "archive".into(),
            normalized_expected,
            calculated,
        ));
    }

    Ok(())
}

pub fn extract_executable_from_zip<R: Read + Seek>(
    reader: R,
    binary_name: &str,
    dest_path: &Path,
) -> Result<()> {
    let mut archive = zip::ZipArchive::new(reader)
        .map_err(|e| FfmpegError::ExtractionFailed(format!("Failed to open zip: {}", e)))?;

    let lower_target = binary_name.to_lowercase();
    let mut found_index = None;

    for i in 0..archive.len() {
        let file = archive
            .by_index(i)
            .map_err(|e| FfmpegError::ExtractionFailed(format!("Failed to read entry: {}", e)))?;
        let entry_name = file
            .name()
            .split('/')
            .next_back()
            .unwrap_or("")
            .split('\\')
            .next_back()
            .unwrap_or("")
            .to_lowercase();

        if entry_name == lower_target {
            found_index = Some(i);
            break;
        }
    }

    let Some(idx) = found_index else {
        return Err(FfmpegError::ExtractionFailed(format!(
            "Binary {} not found in zip archive",
            binary_name
        )));
    };

    let mut entry = archive
        .by_index(idx)
        .map_err(|e| FfmpegError::ExtractionFailed(format!("Failed to read binary: {}", e)))?;

    let temp_file = tempfile::Builder::new()
        .prefix(".auralis-exec-")
        .tempfile_in(
            dest_path
                .parent()
                .unwrap_or_else(|| Path::new(".")),
        )?;

    let mut file = temp_file.as_file();
    std::io::copy(&mut entry, &mut file)?;
    file.sync_all()?;

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(temp_file.path(), std::fs::Permissions::from_mode(0o755))?;
    }

    temp_file
        .persist(dest_path)
        .map_err(|e| FfmpegError::ExtractionFailed(format!("Atomic persist failed: {}", e)))?;

    Ok(())
}

pub async fn download_and_install_binaries<F>(progress: F) -> Result<(PathBuf, PathBuf)>
where
    F: Fn(u32, &str) + Send + Sync + 'static,
{
    let app_dir = resolve_app_dir();
    let (ffmpeg_asset, ffprobe_asset) = get_platform_asset_names()?;

    progress(5, "Checking for latest FFmpeg release...");

    let client = reqwest::Client::builder()
        .user_agent("Auralis")
        .build()?;

    let releases: Vec<GithubRelease> = client
        .get(RELEASES_API_URL)
        .header("Accept", "application/vnd.github+json")
        .send()
        .await?
        .json()
        .await?;

    let release = releases
        .into_iter()
        .find(|r| !r.draft && !r.prerelease && r.tag_name.starts_with(RELEASE_TAG_PREFIX))
        .ok_or_else(|| {
            FfmpegError::AssetNotFound(format!("No release matching {}", RELEASE_TAG_PREFIX))
        })?;

    // Download FFmpeg
    let ffmpeg_digest = release
        .assets
        .iter()
        .find(|a| a.name == ffmpeg_asset)
        .map(|a| a.digest.clone())
        .unwrap_or_default();

    let ffmpeg_url = format!(
        "{}/{}/{}",
        RELEASE_DOWNLOAD_URL, release.tag_name, ffmpeg_asset
    );
    progress(20, "Downloading FFmpeg...");

    let ffmpeg_bytes = client.get(&ffmpeg_url).send().await?.bytes().await?;
    if !ffmpeg_digest.is_empty() {
        verify_archive_sha256(&ffmpeg_bytes, &ffmpeg_digest)?;
    }

    progress(45, "Extracting FFmpeg...");
    let ffmpeg_dest = app_dir.join(executable_name("ffmpeg"));
    extract_executable_from_zip(
        std::io::Cursor::new(&ffmpeg_bytes),
        &executable_name("ffmpeg"),
        &ffmpeg_dest,
    )?;

    // Download FFprobe
    let ffprobe_digest = release
        .assets
        .iter()
        .find(|a| a.name == ffprobe_asset)
        .map(|a| a.digest.clone())
        .unwrap_or_default();

    let ffprobe_url = format!(
        "{}/{}/{}",
        RELEASE_DOWNLOAD_URL, release.tag_name, ffprobe_asset
    );
    progress(60, "Downloading FFprobe...");

    let ffprobe_bytes = client.get(&ffprobe_url).send().await?.bytes().await?;
    if !ffprobe_digest.is_empty() {
        verify_archive_sha256(&ffprobe_bytes, &ffprobe_digest)?;
    }

    progress(85, "Extracting FFprobe...");
    let ffprobe_dest = app_dir.join(executable_name("ffprobe"));
    extract_executable_from_zip(
        std::io::Cursor::new(&ffprobe_bytes),
        &executable_name("ffprobe"),
        &ffprobe_dest,
    )?;

    progress(100, "FFmpeg and FFprobe installed successfully");
    Ok((ffmpeg_dest, ffprobe_dest))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_verify_archive_sha256() {
        let sample = b"hello world";
        // sha256 of "hello world"
        let expected = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9";
        assert!(verify_archive_sha256(sample, expected).is_ok());
        assert!(verify_archive_sha256(sample, &format!("sha256:{}", expected)).is_ok());
        assert!(verify_archive_sha256(sample, "0000000000000000000000000000000000000000000000000000000000000000").is_err());
    }

    #[test]
    fn test_platform_assets() {
        let res = get_platform_asset_names();
        assert!(res.is_ok());
    }
}

//! macOS custom file icon integration.
//!
//! Port of `backend/fileicon_darwin.go` and `backend/fileicon_stub.go`.

use crate::error::FileIconError;
use std::path::{Path, PathBuf};

/// Sets the custom macOS file icon from an image file.
///
/// Port of `backend.SetMacOSFileIconFromImage`.
/// On macOS, resizes the image and invokes `osascript` to apply the icon via AppKit's `NSWorkspace`.
/// On other platforms, this is a no-op that returns `Ok(())`.
#[cfg(target_os = "macos")]
pub fn set_macos_file_icon_from_image(
    file_path: impl AsRef<Path>,
    image_path: impl AsRef<Path>,
    icon_size: u32,
) -> Result<(), FileIconError> {
    let file = file_path.as_ref();
    let image = image_path.as_ref();

    if file.as_os_str().is_empty() {
        return Err(FileIconError::InvalidPath("file path is required".into()));
    }
    if image.as_os_str().is_empty() {
        return Err(FileIconError::InvalidPath("image path is required".into()));
    }

    let resized_path = resize_image_for_icon(image, icon_size)?;

    let script = r#"
use framework "AppKit"
on run argv
    set imagePath to item 1 of argv
    set targetPath to item 2 of argv
    set iconImage to current application's NSImage's alloc()'s initWithContentsOfFile:imagePath
    if iconImage is missing value then error "Failed to load icon image"
    set didSet to (current application's NSWorkspace's sharedWorkspace()'s setIcon:iconImage forFile:targetPath options:0) as boolean
    if didSet is false then error "Failed to set custom file icon"
end run
"#;

    let mut cmd = std::process::Command::new("osascript");
    cmd.args([
        "-",
        &resized_path.to_string_lossy(),
        &file.to_string_lossy(),
    ]);
    cmd.stdin(std::process::Stdio::piped());
    cmd.stdout(std::process::Stdio::piped());
    cmd.stderr(std::process::Stdio::piped());

    let mut child = cmd.spawn().map_err(FileIconError::ProcessSpawn)?;
    if let Some(mut stdin) = child.stdin.take() {
        use std::io::Write;
        let _ = stdin.write_all(script.as_bytes());
    }

    let output = child
        .wait_with_output()
        .map_err(FileIconError::ProcessSpawn)?;
    let _ = std::fs::remove_file(&resized_path);

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        let stdout = String::from_utf8_lossy(&output.stdout);
        let msg = format!("{} {}", stderr.trim(), stdout.trim())
            .trim()
            .to_string();
        return Err(FileIconError::ScriptFailed(format!(
            "failed to apply macOS file icon: status {:?} ({})",
            output.status.code(),
            msg
        )));
    }

    Ok(())
}

/// Sets the custom macOS file icon from an image file (non-macOS stub).
///
/// Port of `backend/fileicon_stub.go`.
#[cfg(not(target_os = "macos"))]
pub fn set_macos_file_icon_from_image(
    file_path: impl AsRef<Path>,
    image_path: impl AsRef<Path>,
    _icon_size: u32,
) -> Result<(), FileIconError> {
    let file = file_path.as_ref();
    let image = image_path.as_ref();

    if file.as_os_str().is_empty() {
        return Err(FileIconError::InvalidPath("file path is required".into()));
    }
    if image.as_os_str().is_empty() {
        return Err(FileIconError::InvalidPath("image path is required".into()));
    }

    Ok(())
}

/// Resizes a source image to a square icon (defaults to 256x256) and saves it as a temporary PNG.
///
/// Port of `backend.ResizeImageForIcon` in `backend/cover.go`.
pub fn resize_image_for_icon(
    source_path: impl AsRef<Path>,
    icon_size: u32,
) -> Result<PathBuf, FileIconError> {
    let source = source_path.as_ref();
    if source.as_os_str().is_empty() {
        return Err(FileIconError::InvalidPath(
            "source image path is required".into(),
        ));
    }
    let size = if icon_size == 0 { 256 } else { icon_size };

    let img = image::open(source)
        .map_err(|e| FileIconError::ImageOpen(format!("failed to open source image: {}", e)))?;

    let dst = img.resize_exact(size, size, image::imageops::FilterType::CatmullRom);

    let temp = tempfile::Builder::new()
        .prefix("auralis-resized-icon-")
        .suffix(".png")
        .tempfile()
        .map_err(FileIconError::Io)?;

    let (file, path) = temp.keep().map_err(|e| FileIconError::Io(e.error))?;
    let mut writer = std::io::BufWriter::new(file);
    dst.write_to(&mut writer, image::ImageFormat::Png)
        .map_err(|e| {
            FileIconError::ImageEncode(format!("failed to encode resized icon image: {}", e))
        })?;

    Ok(path)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_set_macos_file_icon_stub_validation() {
        // Empty paths must return validation error
        assert!(set_macos_file_icon_from_image("", "some_image.png", 256).is_err());
        assert!(set_macos_file_icon_from_image("file.flac", "", 256).is_err());

        // Valid non-empty paths on non-macOS return Ok(())
        #[cfg(not(target_os = "macos"))]
        {
            let res = set_macos_file_icon_from_image("file.flac", "some_image.png", 256);
            assert!(res.is_ok());
        }
    }

    #[test]
    fn test_resize_image_for_icon() {
        let temp = tempfile::tempdir().unwrap();
        let src_path = temp.path().join("source.png");

        // Create a 100x100 test image
        let img = image::RgbaImage::new(100, 100);
        img.save(&src_path).unwrap();

        let resized_path = resize_image_for_icon(&src_path, 128).unwrap();
        assert!(resized_path.exists());

        let loaded = image::open(&resized_path).unwrap();
        assert_eq!(loaded.width(), 128);
        assert_eq!(loaded.height(), 128);

        let _ = std::fs::remove_file(resized_path);
    }
}

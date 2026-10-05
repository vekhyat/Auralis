//! Atomic file writing and replacing with crash safety.

use std::fs::File;
use std::io::{self, Write};
use std::path::Path;
use std::sync::atomic::{AtomicU64, Ordering};

static COUNTER: AtomicU64 = AtomicU64::new(1);

/// Atomically writes data to `target_path`.
/// Writes to a temporary file in the same directory first, syncs to disk,
/// and renames/replaces atomically.
pub fn write_file_atomic(target_path: &Path, data: &[u8]) -> io::Result<()> {
    let parent = target_path.parent().unwrap_or_else(|| Path::new("."));
    std::fs::create_dir_all(parent)? ;

    let temp_path = {
        let file_stem = target_path
            .file_name()
            .and_then(|s| s.to_str())
            .unwrap_or("file");
        let pid = std::process::id();
        let timestamp = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_nanos())
            .unwrap_or(0);
        let count = COUNTER.fetch_add(1, Ordering::Relaxed);
        parent.join(format!("{}.tmp.{}.{}.{}", file_stem, pid, timestamp, count))
    };

    {
        let mut file = File::create(&temp_path)?;
        file.write_all(data)?;
        file.flush()?;
        file.sync_all()?;
    }

    if let Err(e) = move_file_replace(&temp_path, target_path) {
        let _ = std::fs::remove_file(&temp_path);
        return Err(e);
    }

    Ok(())
}

/// Moves and atomically replaces `target` with `source`.
#[cfg(target_os = "windows")]
pub fn move_file_replace(source: &Path, target: &Path) -> io::Result<()> {
    use std::os::windows::ffi::OsStrExt;
    use windows_sys::Win32::Storage::FileSystem::{
        MoveFileExW, MOVEFILE_REPLACE_EXISTING, MOVEFILE_WRITE_THROUGH,
    };

    let src_wide: Vec<u16> = source
        .as_os_str()
        .encode_wide()
        .chain(std::iter::once(0))
        .collect();
    let dst_wide: Vec<u16> = target
        .as_os_str()
        .encode_wide()
        .chain(std::iter::once(0))
        .collect();

    let flags = MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH;
    let res = unsafe { MoveFileExW(src_wide.as_ptr(), dst_wide.as_ptr(), flags) };

    if res == 0 {
        Err(io::Error::last_os_error())
    } else {
        Ok(())
    }
}

#[cfg(not(target_os = "windows"))]
pub fn move_file_replace(source: &Path, target: &Path) -> io::Result<()> {
    std::fs::rename(source, target)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_atomic_write_and_replace() {
        let dir = tempfile::tempdir().unwrap();
        let target = dir.path().join("config.json");

        write_file_atomic(&target, b"initial content").unwrap();
        assert_eq!(std::fs::read_to_string(&target).unwrap(), "initial content");

        write_file_atomic(&target, b"updated content").unwrap();
        assert_eq!(std::fs::read_to_string(&target).unwrap(), "updated content");

        // Verify no leftover temp files
        let entries: Vec<_> = std::fs::read_dir(dir.path())
            .unwrap()
            .map(|e| e.unwrap().file_name().to_string_lossy().to_string())
            .collect();
        assert_eq!(entries, vec!["config.json"]);
    }

    #[test]
    fn test_interrupted_write_preserves_old_file() {
        let dir = tempfile::tempdir().unwrap();
        let target = dir.path().join("config.json");

        // Write initial valid file
        write_file_atomic(&target, b"intact original content").unwrap();
        assert_eq!(std::fs::read_to_string(&target).unwrap(), "intact original content");

        // Simulate an interrupted write leaving a temp file behind
        let temp_leftover = dir.path().join("config.json.tmp.1234.5678.9");
        std::fs::write(&temp_leftover, b"incomplete partial write").unwrap();

        // The original file is still completely intact and readable
        assert_eq!(std::fs::read_to_string(&target).unwrap(), "intact original content");

        // Subsequent write succeeds
        write_file_atomic(&target, b"new complete content").unwrap();
        assert_eq!(std::fs::read_to_string(&target).unwrap(), "new complete content");
    }
}

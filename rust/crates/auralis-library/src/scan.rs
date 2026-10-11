//! Filesystem scanner for Library Doctor.
//!
//! Recursively walks music directories, probes tracks with `lofty`,
//! and reports scanning progress or handles cooperative cancellation.

use crate::tag_io::read_track;
use crate::types::{Scan, ScanProgress, Track};
use std::collections::HashSet;
use std::path::{Path, PathBuf};
use walkdir::WalkDir;

const AUDIO_EXTENSIONS: &[&str] = &["flac", "mp3", "m4a", "aac", "ogg", "opus", "wav", "aiff"];
const SIDECAR_EXTENSIONS: &[&str] = &["jpg", "jpeg", "png", "webp", "gif", "lrc", "cue", "log"];

/// Recursively scans a music library directory.
///
/// Supports progress reporting and early cancellation.
pub fn scan_library<P, C>(
    root: impl AsRef<Path>,
    mut on_progress: Option<P>,
    is_cancelled: Option<C>,
) -> Result<Scan, String>
where
    P: FnMut(ScanProgress),
    C: Fn() -> bool,
{
    let root = root.as_ref();
    if !root.is_dir() {
        return Err(format!("library root does not exist: {root:?}"));
    }

    let mut audio_paths: Vec<(String, PathBuf)> = Vec::new();
    let mut dirs_set: HashSet<String> = HashSet::new();
    let mut sidecar_paths: Vec<(String, PathBuf)> = Vec::new();

    for entry_res in WalkDir::new(root).follow_links(false) {
        if let Some(ref cancel) = is_cancelled {
            if cancel() {
                return Err("scan cancelled".to_string());
            }
        }

        let entry = match entry_res {
            Ok(e) => e,
            Err(e) => return Err(format!("walk error: {e}")),
        };

        let path = entry.path();
        if path == root {
            continue;
        }

        // Relative path forward-slashed
        let rel = match path.strip_prefix(root) {
            Ok(p) => p.to_string_lossy().replace('\\', "/"),
            Err(_) => continue,
        };

        // Skip internal incoming scratch dirs
        if rel.contains(".auralis-incoming-") {
            continue;
        }

        if entry.file_type().is_dir() {
            dirs_set.insert(rel);
            continue;
        }

        let ext = path
            .extension()
            .and_then(|s| s.to_str())
            .map(|s| s.to_ascii_lowercase())
            .unwrap_or_default();

        if AUDIO_EXTENSIONS.contains(&ext.as_str()) {
            audio_paths.push((rel, path.to_path_buf()));
        } else if SIDECAR_EXTENSIONS.contains(&ext.as_str()) {
            sidecar_paths.push((rel, path.to_path_buf()));
        }
    }

    let total = audio_paths.len();

    // Fire discovery progress
    if let Some(ref mut progress) = on_progress {
        progress(ScanProgress {
            phase: "discover".to_string(),
            current: total,
            total,
            path: String::new(),
        });
    }

    let mut tracks = Vec::with_capacity(total);
    let mut audio_dirs = HashSet::new();

    for (i, (rel, path)) in audio_paths.into_iter().enumerate() {
        if let Some(ref cancel) = is_cancelled {
            if cancel() {
                return Err("scan cancelled".to_string());
            }
        }

        if let Some(ref mut progress) = on_progress {
            progress(ScanProgress {
                phase: "tags".to_string(),
                current: i + 1,
                total,
                path: rel.clone(),
            });
        }

        match read_track(root, &rel, &path) {
            Ok(track) => tracks.push(track),
            Err(_e) => {
                // Return basic track on failure
                tracks.push(Track {
                    path: path.to_string_lossy().to_string(),
                    rel_path: rel.clone(),
                    ..Default::default()
                });
            }
        }

        if let Some(parent) = Path::new(&rel).parent() {
            let parent_str = parent.to_string_lossy().replace('\\', "/");
            if !parent_str.is_empty() {
                audio_dirs.insert(parent_str);
            }
        }
    }

    // Identify stray sidecars (sidecars in directories containing no audio tracks)
    let mut stray_files = Vec::new();
    for (rel, _path) in sidecar_paths {
        let parent_str = Path::new(&rel)
            .parent()
            .map(|p| p.to_string_lossy().replace('\\', "/"))
            .unwrap_or_default();

        if !audio_dirs.contains(&parent_str) {
            stray_files.push(rel);
        }
    }

    let mut dirs: Vec<String> = dirs_set.into_iter().collect();
    dirs.sort();
    stray_files.sort();

    Ok(Scan {
        root: root.to_string_lossy().to_string(),
        tracks,
        dirs,
        stray_files,
        scanned_at: chrono::Utc::now(),
    })
}

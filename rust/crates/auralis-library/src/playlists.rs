//! M3U8 Playlist generation and path resolver.
//!
//! Generates UTF-8 encoded, LF-terminated Extended M3U playlists
//! supporting multiple path mapping modes (`device`, `root`, `local`, `relative`).

use crate::types::{PlaylistMode, PlaylistResult, PlaylistSkip, PlaylistTrack};
use std::fs::{self, File};
use std::io::Write;
use std::path::Path;

/// Writes an M3U8 playlist with the given tracks according to the requested mode.
pub fn write_playlist(
    dest_path: impl AsRef<Path>,
    tracks: &[PlaylistTrack],
    mode: PlaylistMode,
    library_root: &str,
    device_prefix: &str,
) -> Result<PlaylistResult, String> {
    let dest = dest_path.as_ref();
    if let Some(parent) = dest.parent() {
        fs::create_dir_all(parent).map_err(|e| format!("cannot create dir {parent:?}: {e}"))?;
    }

    let mut out = File::create(dest).map_err(|e| format!("cannot create {dest:?}: {e}"))?;

    let mut result = PlaylistResult {
        path: dest.to_string_lossy().to_string(),
        written: 0,
        skipped: Vec::new(),
    };

    // Extended M3U header
    out.write_all(b"#EXTM3U\n")
        .map_err(|e| format!("failed to write header: {e}"))?;

    for track in tracks {
        if track.path.trim().is_empty() {
            continue;
        }

        let (line, skip_reason) =
            playlist_line(dest, &track.path, mode, library_root, device_prefix);

        if let Some(reason) = skip_reason {
            result.skipped.push(PlaylistSkip {
                path: track.path.clone(),
                reason,
            });
            continue;
        }

        if !track.title.is_empty() || !track.artist.is_empty() {
            let mut seconds = track.duration_seconds.round() as i64;
            if seconds <= 0 {
                seconds = -1;
            }

            let artist_title = if !track.artist.is_empty() && !track.title.is_empty() {
                format!("{} - {}", track.artist, track.title)
            } else if !track.title.is_empty() {
                track.title.clone()
            } else {
                track.artist.clone()
            };

            writeln!(out, "#EXTINF:{seconds},{artist_title}")
                .map_err(|e| format!("failed to write EXTINF: {e}"))?;
        }

        writeln!(out, "{line}").map_err(|e| format!("failed to write track line: {e}"))?;
        result.written += 1;
    }

    Ok(result)
}

/// Generates a single line for an M3U8 entry based on the chosen mode.
pub fn playlist_line(
    m3u8_path: impl AsRef<Path>,
    track_path: &str,
    mode: PlaylistMode,
    music_root: &str,
    device_prefix: &str,
) -> (String, Option<String>) {
    let m3u8_p = m3u8_path.as_ref();
    let m3u8_str = m3u8_p.to_string_lossy();

    match mode {
        PlaylistMode::Device => {
            let (rel, reason) = rel_to_root(track_path, music_root);
            if let Some(r) = reason {
                return (String::new(), Some(r));
            }
            let prefix = device_prefix.trim_end_matches(['/', '\\']);
            if prefix.is_empty() {
                return (String::new(), Some("device prefix is empty".to_string()));
            }
            (format!("{prefix}/{rel}"), None)
        }

        PlaylistMode::Root => {
            let (rel, reason) = rel_to_root(track_path, music_root);
            if let Some(r) = reason {
                return (String::new(), Some(r));
            }
            (rel, None)
        }

        PlaylistMode::Local => {
            let m3u8_dir = parent_dir(&m3u8_str);
            if let Ok(rel) = filepath_rel(m3u8_dir, track_path) {
                (rel, None)
            } else {
                (track_path.to_string(), None)
            }
        }

        PlaylistMode::Relative => {
            let m3u8_dir = parent_dir(&m3u8_str);
            match filepath_rel(m3u8_dir, track_path) {
                Ok(rel) => (rel, None),
                Err(err) => (String::new(), Some(err)),
            }
        }
    }
}

fn rel_to_root(track_path: &str, music_root: &str) -> (String, Option<String>) {
    if music_root.trim().is_empty() {
        return (String::new(), Some("music root is empty".to_string()));
    }
    match filepath_rel(music_root, track_path) {
        Ok(rel) => {
            if rel == ".." || rel.starts_with("../") {
                (
                    String::new(),
                    Some("path is outside the music root".to_string()),
                )
            } else {
                (rel, None)
            }
        }
        Err(err) => (String::new(), Some(err)),
    }
}

fn parent_dir(p: &str) -> &str {
    let norm = p.trim_end_matches(['/', '\\']);
    if let Some(idx) = norm.rfind(['/', '\\']) {
        &norm[..idx]
    } else {
        ""
    }
}

fn volume_name(p: &str) -> &str {
    let bytes = p.as_bytes();
    if bytes.len() >= 2 && bytes[1] == b':' && bytes[0].is_ascii_alphabetic() {
        &p[..2]
    } else {
        ""
    }
}

/// Pure-Rust implementation of Go's `filepath.Rel`.
pub fn filepath_rel(basepath: &str, targpath: &str) -> Result<String, String> {
    let vol_base = volume_name(basepath);
    let vol_targ = volume_name(targpath);

    if !vol_base.eq_ignore_ascii_case(vol_targ) {
        return Err("path is on a different volume".to_string());
    }

    let rest_base = &basepath[vol_base.len()..];
    let rest_targ = &targpath[vol_targ.len()..];

    let base_segs: Vec<&str> = rest_base
        .split(['/', '\\'])
        .filter(|s| !s.is_empty() && *s != ".")
        .collect();

    let targ_segs: Vec<&str> = rest_targ
        .split(['/', '\\'])
        .filter(|s| !s.is_empty() && *s != ".")
        .collect();

    let mut common = 0;
    while common < base_segs.len() && common < targ_segs.len() {
        #[cfg(windows)]
        let eq = base_segs[common].eq_ignore_ascii_case(targ_segs[common]);
        #[cfg(not(windows))]
        let eq = base_segs[common] == targ_segs[common];

        if eq {
            common += 1;
        } else {
            break;
        }
    }

    let mut parts = Vec::new();
    if base_segs.len() > common {
        parts.resize(base_segs.len() - common, "..");
    }
    for seg in &targ_segs[common..] {
        parts.push(*seg);
    }

    if parts.is_empty() {
        Ok(".".to_string())
    } else {
        Ok(parts.join("/"))
    }
}

/// Converts a list of file paths into PlaylistTrack objects with metadata.
pub fn tracks_from_paths(paths: &[String]) -> Vec<PlaylistTrack> {
    let mut out = Vec::new();
    for p in paths {
        let mut track = PlaylistTrack {
            path: p.clone(),
            ..Default::default()
        };

        if let Ok(tags) = crate::tag_io::read_tags(Path::new(p)) {
            track.title = crate::tag_io::first_tag(&tags, &["TITLE"]);
            track.artist = crate::tag_io::first_tag(&tags, &["ARTIST"]);
        }

        out.push(track);
    }
    out
}

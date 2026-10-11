//! Rule for path legality and template conformance.

use crate::profiles::{Profile, TrackFields};
use crate::types::{Issue, Operation, Scan, Severity, Track};
use std::path::{Path, PathBuf};

/// Validates that track paths adhere to profile naming and character rules.
pub fn path_detect(scan: &Scan, profile: &Profile) -> Vec<Issue> {
    let mut issues = Vec::new();

    for t in &scan.tracks {
        if !path_is_legal(profile, &t.rel_path) {
            issues.push(Issue {
                id: String::new(),
                rule_id: "PATH".to_string(),
                severity: Severity::Error,
                path: t.path.clone(),
                album_dir: t.album_dir().to_string(),
                message: "path contains characters illegal for this profile".to_string(),
                fixable: true,
            });
            continue;
        }

        if t.title.trim().is_empty() || t.album.trim().is_empty() {
            // Without tags the template would send every file to
            // "Unknown Album/Unknown Title" and collide.
            continue;
        }

        let expected = expected_rel_path(profile, t);
        let trimmed_rel = trim_extension(&t.rel_path);

        if !expected.eq_ignore_ascii_case(trimmed_rel) {
            issues.push(Issue {
                id: String::new(),
                rule_id: "PATH".to_string(),
                severity: Severity::Error,
                path: t.path.clone(),
                album_dir: t.album_dir().to_string(),
                message: "path does not follow the profile template".to_string(),
                fixable: true,
            });
        }
    }

    issues
}

/// Builds move operations to align paths with the profile template.
pub fn path_fix(scan: &Scan, profile: &Profile, issues: &[Issue]) -> Vec<Operation> {
    let mut ops = Vec::new();

    for issue in issues {
        let t = match scan.tracks.iter().find(|tr| tr.path == issue.path) {
            Some(tr) => tr,
            None => continue,
        };

        let ext = Path::new(&t.rel_path)
            .extension()
            .and_then(|s| s.to_str())
            .map(|s| format!(".{}", s.to_ascii_lowercase()))
            .unwrap_or_default();

        let mut new_rel = format!("{}{ext}", expected_rel_path(profile, t));
        if !path_is_legal(profile, &new_rel) {
            new_rel = profile.sanitize_relative_path(&new_rel);
        }

        let old_no_ext = trim_extension(&t.rel_path);
        let new_no_ext = trim_extension(&new_rel);

        if new_no_ext.eq_ignore_ascii_case(old_no_ext) {
            let sanitized = profile.sanitize_relative_path(&t.rel_path);
            if sanitized.eq_ignore_ascii_case(&t.rel_path) {
                continue;
            }
            new_rel = sanitized;
        }

        let dest = Path::new(&scan.root).join(PathBuf::from(
            new_rel.replace('/', std::path::MAIN_SEPARATOR_STR),
        ));
        let dest_str = dest.to_string_lossy().to_string();

        if dest_str == t.path {
            continue;
        }

        ops.push(Operation {
            op_type: "move".to_string(),
            path: t.path.clone(),
            new_path: Some(dest_str),
            reason: "move to profile template path".to_string(),
            size: t.size,
            mod_time: t.mod_time_unix_nano,
            ..Default::default()
        });
    }

    ops
}

fn path_is_legal(profile: &Profile, rel: &str) -> bool {
    let normalized = rel.replace('\\', "/");
    for seg in normalized.split('/') {
        if profile.sanitize_component(seg) != seg {
            return false;
        }
    }
    true
}

fn expected_rel_path(profile: &Profile, t: &Track) -> String {
    let fields = TrackFields {
        title: t.title.clone(),
        artist: t.artist.clone(),
        album_artist: t.album_artist.clone(),
        album: t.album.clone(),
        year: year4(&t.year),
        track_number: t.track_number,
        disc_number: t.disc_number,
        disc_total: t.disc_total,
    };
    profile.relative_path(&fields)
}

fn year4(year: &str) -> String {
    let y = year.trim();
    if y.len() >= 4 {
        y[..4].to_string()
    } else {
        y.to_string()
    }
}

fn trim_extension(p: &str) -> &str {
    match p.rfind('.') {
        Some(idx) => &p[..idx],
        None => p,
    }
}

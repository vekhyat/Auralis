//! Rule for finding empty directories, stray artwork, and zero-byte files.

use crate::profiles::Profile;
use crate::types::{Issue, Operation, Scan, Severity};
use std::collections::HashSet;
use std::fs;
use std::path::{Path, PathBuf};

/// Identifies orphan folders and files.
pub fn orphan_detect(scan: &Scan, _profile: &Profile) -> Vec<Issue> {
    let mut issues = Vec::new();

    // Directories that contain audio anywhere below them.
    let mut audio_dirs = HashSet::new();
    for t in &scan.tracks {
        let dir = t.album_dir();
        audio_dirs.insert(dir.clone());
        let mut parent = dir.as_str();
        while let Some(idx) = parent.rfind('/') {
            parent = &parent[..idx];
            if parent.is_empty() {
                break;
            }
            audio_dirs.insert(parent.to_string());
        }
    }

    for dir in &scan.dirs {
        if audio_dirs.contains(dir) {
            continue;
        }
        let full_path = Path::new(&scan.root).join(PathBuf::from(
            dir.replace('/', std::path::MAIN_SEPARATOR_STR),
        ));
        if dir_is_empty(&full_path) {
            issues.push(Issue {
                id: String::new(),
                rule_id: "ORPHAN".to_string(),
                severity: Severity::Info,
                path: full_path.to_string_lossy().to_string(),
                album_dir: dir.clone(),
                message: "empty folder".to_string(),
                fixable: true,
            });
        }
    }

    for stray in &scan.stray_files {
        let album_dir = match stray.rfind('/') {
            Some(idx) => &stray[..idx],
            None => "",
        };
        let full_path = Path::new(&scan.root).join(PathBuf::from(
            stray.replace('/', std::path::MAIN_SEPARATOR_STR),
        ));
        issues.push(Issue {
            id: String::new(),
            rule_id: "ORPHAN".to_string(),
            severity: Severity::Info,
            path: full_path.to_string_lossy().to_string(),
            album_dir: album_dir.to_string(),
            message: "stray artwork/lyrics file with no audio in the folder".to_string(),
            fixable: false,
        });
    }

    for t in &scan.tracks {
        if t.size == 0 {
            issues.push(Issue {
                id: String::new(),
                rule_id: "ORPHAN".to_string(),
                severity: Severity::Info,
                path: t.path.clone(),
                album_dir: t.album_dir(),
                message: "zero-byte audio file".to_string(),
                fixable: false,
            });
        }
    }

    issues.sort_by(|a, b| a.path.cmp(&b.path));
    issues
}

/// Builds rmdir operations to clean up empty orphan directories.
pub fn orphan_fix(_scan: &Scan, _profile: &Profile, issues: &[Issue]) -> Vec<Operation> {
    let mut ops = Vec::new();
    for issue in issues {
        if !issue.fixable {
            continue;
        }
        ops.push(Operation {
            op_type: "rmdir".to_string(),
            path: issue.path.clone(),
            reason: "remove empty folder".to_string(),
            ..Default::default()
        });
    }
    ops
}

fn dir_is_empty(dir: &Path) -> bool {
    match fs::read_dir(dir) {
        Ok(mut entries) => entries.next().is_none(),
        Err(_) => false,
    }
}

//! Rule for embedded and folder cover-art limits.

use crate::profiles::Profile;
use crate::types::{Issue, Scan, Severity};
use std::collections::HashSet;

const MAX_EMBEDDED_COVER_BYTES: usize = (1.5 * 1024.0 * 1024.0) as usize;

/// Validates cover art against profile limits.
pub fn cover_detect(scan: &Scan, profile: &Profile) -> Vec<Issue> {
    let mut issues = Vec::new();
    let mut folder_warned = HashSet::new();

    for t in &scan.tracks {
        if t.cover_bytes == 0 {
            issues.push(Issue {
                id: String::new(),
                rule_id: "COVER".to_string(),
                severity: Severity::Warning,
                path: t.path.clone(),
                album_dir: t.album_dir(),
                message: "no embedded cover".to_string(),
                fixable: false,
            });
        } else {
            if profile.max_cover_px > 0
                && (t.cover_width > profile.max_cover_px || t.cover_height > profile.max_cover_px)
            {
                issues.push(Issue {
                    id: String::new(),
                    rule_id: "COVER".to_string(),
                    severity: Severity::Warning,
                    path: t.path.clone(),
                    album_dir: t.album_dir(),
                    message: format!("embedded cover exceeds {}px", profile.max_cover_px),
                    fixable: false,
                });
            }
            if t.cover_bytes > MAX_EMBEDDED_COVER_BYTES {
                issues.push(Issue {
                    id: String::new(),
                    rule_id: "COVER".to_string(),
                    severity: Severity::Warning,
                    path: t.path.clone(),
                    album_dir: t.album_dir(),
                    message: "embedded cover is larger than 1.5 MB".to_string(),
                    fixable: false,
                });
            }
        }

        let dir = t.album_dir();
        if profile.folder_cover && !folder_warned.contains(&dir) && !t.folder_cover {
            folder_warned.insert(dir.clone());
            issues.push(Issue {
                id: String::new(),
                rule_id: "COVER".to_string(),
                severity: Severity::Warning,
                path: dir.clone(),
                album_dir: dir,
                message: "profile expects a folder cover.jpg but none exists".to_string(),
                fixable: false,
            });
        }
    }

    issues
}

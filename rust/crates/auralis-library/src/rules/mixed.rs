//! Rule for detecting mixed audio formats and bit depths within an album folder.

use crate::profiles::Profile;
use crate::rules::helpers::albums;
use crate::types::{Issue, Scan, Severity};
use std::collections::HashSet;

/// Detects albums that mix different formats or bit depths.
pub fn mixed_format_detect(scan: &Scan, _profile: &Profile) -> Vec<Issue> {
    let mut issues = Vec::new();
    let albums_map = albums(scan);

    for (dir, tracks) in albums_map {
        let mut formats = HashSet::new();
        let mut depths = HashSet::new();

        for t in &tracks {
            if !t.format.is_empty() {
                formats.insert(t.format.clone());
            }
            if t.bit_depth > 0 {
                depths.insert(t.bit_depth);
            }
        }

        if formats.len() > 1 {
            let mut values: Vec<String> = formats.into_iter().collect();
            values.sort();
            issues.push(Issue {
                id: String::new(),
                rule_id: "MIXEDFORMAT".to_string(),
                severity: Severity::Info,
                path: dir.clone(),
                album_dir: dir.clone(),
                message: format!("album mixes formats: {}", values.join(", ")),
                fixable: false,
            });
        }

        if depths.len() > 1 {
            issues.push(Issue {
                id: String::new(),
                rule_id: "MIXEDFORMAT".to_string(),
                severity: Severity::Info,
                path: dir.clone(),
                album_dir: dir.clone(),
                message: "album mixes bit depths".to_string(),
                fixable: false,
            });
        }
    }

    issues
}

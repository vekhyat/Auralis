//! Album-level rules: ALBUMARTIST consistency and COMPILATION detection.

use crate::profiles::Profile;
use crate::rules::helpers::{albums, group_distinct, most_common, single_album, tag_op};
use crate::types::{Issue, Operation, Scan, Severity};
use std::collections::{HashMap, HashSet};

/// Detects missing or inconsistent ALBUMARTIST tags within an album folder.
pub fn album_artist_detect(scan: &Scan, _profile: &Profile) -> Vec<Issue> {
    let mut issues = Vec::new();
    let albums_map = albums(scan);

    for (dir, tracks) in albums_map {
        if !single_album(&dir, &tracks) {
            continue;
        }

        let mut seen = HashMap::new();
        for t in &tracks {
            let aa = t.album_artist.trim();
            if !aa.is_empty() {
                *seen.entry(aa.to_string()).or_insert(0) += 1;
            }
        }

        if seen.is_empty() {
            issues.push(Issue {
                id: String::new(),
                rule_id: "ALBUMARTIST".to_string(),
                severity: Severity::Error,
                path: dir.clone(),
                album_dir: dir.clone(),
                message: "album has no ALBUMARTIST on any track".to_string(),
                fixable: true,
            });
            continue;
        }

        if seen.len() > 1 {
            let mut values: Vec<String> = seen.into_keys().collect();
            values.sort();
            issues.push(Issue {
                id: String::new(),
                rule_id: "ALBUMARTIST".to_string(),
                severity: Severity::Error,
                path: dir.clone(),
                album_dir: dir.clone(),
                message: format!("inconsistent ALBUMARTIST values: {}", values.join(", ")),
                fixable: true,
            });
            continue;
        }

        for t in &tracks {
            if t.album_artist.trim().is_empty() {
                issues.push(Issue {
                    id: String::new(),
                    rule_id: "ALBUMARTIST".to_string(),
                    severity: Severity::Error,
                    path: t.path.clone(),
                    album_dir: dir.clone(),
                    message: "track is missing ALBUMARTIST".to_string(),
                    fixable: true,
                });
            }
        }
    }

    issues
}

/// Fixes ALBUMARTIST by setting the most common value across the album.
pub fn album_artist_fix(scan: &Scan, _profile: &Profile, issues: &[Issue]) -> Vec<Operation> {
    let albums_map = albums(scan);
    let mut ops = Vec::new();

    for issue in issues {
        let tracks = match albums_map.get(&issue.album_dir) {
            Some(t) => t,
            None => continue,
        };

        let mut target = most_common(&group_distinct(
            tracks.iter().map(|t| t.album_artist.as_str()),
        ));
        if target.is_empty() {
            target = most_common(&group_distinct(tracks.iter().map(|t| t.artist.as_str())));
        }
        if target.is_empty() {
            target = "Unknown Artist".to_string();
        }

        for t in tracks {
            if t.album_artist.trim() == target {
                continue;
            }
            let mut set = std::collections::BTreeMap::new();
            set.insert("ALBUMARTIST".to_string(), target.clone());
            ops.push(tag_op(t, set, "set consistent ALBUMARTIST"));
        }
    }

    ops
}

/// Detects multi-artist compilations missing COMPILATION=1 or ALBUMARTIST="Various Artists".
pub fn compilation_detect(scan: &Scan, _profile: &Profile) -> Vec<Issue> {
    let mut issues = Vec::new();
    let albums_map = albums(scan);

    for (dir, tracks) in albums_map {
        if !single_album(&dir, &tracks) {
            continue;
        }

        let mut artists = HashSet::new();
        for t in &tracks {
            let a = t.artist.trim().to_lowercase();
            if !a.is_empty() {
                artists.insert(a);
            }
        }

        if artists.len() < 3 {
            continue;
        }

        let album_artist = most_common(&group_distinct(
            tracks.iter().map(|t| t.album_artist.as_str()),
        ))
        .to_lowercase();
        let marked = tracks.iter().any(|t| t.compilation);

        if album_artist == "various artists" || marked {
            continue;
        }

        issues.push(Issue {
            id: String::new(),
            rule_id: "COMPILATION".to_string(),
            severity: Severity::Warning,
            path: dir.clone(),
            album_dir: dir.clone(),
            message: "multiple artists, one folder, but no Various Artists/COMPILATION tag"
                .to_string(),
            fixable: true,
        });
    }

    issues
}

/// Fixes compilations by setting ALBUMARTIST="Various Artists" and COMPILATION=1.
pub fn compilation_fix(scan: &Scan, _profile: &Profile, issues: &[Issue]) -> Vec<Operation> {
    let albums_map = albums(scan);
    let mut ops = Vec::new();

    for issue in issues {
        if let Some(tracks) = albums_map.get(&issue.album_dir) {
            for t in tracks {
                let mut set = std::collections::BTreeMap::new();
                set.insert("ALBUMARTIST".to_string(), "Various Artists".to_string());
                set.insert("COMPILATION".to_string(), "1".to_string());
                ops.push(tag_op(t, set, "mark album as compilation"));
            }
        }
    }

    ops
}

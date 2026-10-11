//! Rule for detecting likely duplicate tracks by ISRC or title/artist/duration.

use crate::profiles::Profile;
use crate::types::{Issue, Scan, Severity, Track};
use std::collections::HashMap;

/// Identifies duplicate tracks across the library.
pub fn duplicate_detect(scan: &Scan, _profile: &Profile) -> Vec<Issue> {
    let mut issues = Vec::new();
    let mut by_isrc: HashMap<String, Vec<String>> = HashMap::new();
    let mut by_tag: HashMap<(String, String), Vec<&Track>> = HashMap::new();

    for t in &scan.tracks {
        let isrc = t.isrc.trim().to_ascii_uppercase();
        if !isrc.is_empty() {
            by_isrc.entry(isrc).or_default().push(t.path.clone());
        }

        let artist = t.artist.trim().to_lowercase();
        let title = t.title.trim().to_lowercase();
        if !artist.is_empty() && !title.is_empty() {
            by_tag.entry((artist, title)).or_default().push(t);
        }
    }

    for (isrc, mut paths) in by_isrc {
        if paths.len() < 2 {
            continue;
        }
        paths.sort();
        for p in &paths {
            let album_dir = scan
                .tracks
                .iter()
                .find(|tr| &tr.path == p)
                .map(|tr| tr.album_dir().to_string())
                .unwrap_or_default();
            issues.push(Issue {
                id: String::new(),
                rule_id: "DUPLICATES".to_string(),
                severity: Severity::Info,
                path: p.clone(),
                album_dir,
                message: format!("same ISRC {} also at {}", isrc, other_path(&paths, p)),
                fixable: false,
            });
        }
    }

    for (_key, tracks) in by_tag {
        if tracks.len() < 2 {
            continue;
        }
        for i in 0..tracks.len() {
            for j in (i + 1)..tracks.len() {
                let a = tracks[i];
                let b = tracks[j];
                if a.duration_seconds <= 0.0 || b.duration_seconds <= 0.0 {
                    continue;
                }
                let diff = (a.duration_seconds - b.duration_seconds).abs();
                if diff <= 2.0 {
                    issues.push(Issue {
                        id: String::new(),
                        rule_id: "DUPLICATES".to_string(),
                        severity: Severity::Info,
                        path: b.path.clone(),
                        album_dir: b.album_dir().to_string(),
                        message: format!(
                            "same artist/title and duration within \u{00b1}2s of {}",
                            a.path
                        ),
                        fixable: false,
                    });
                }
            }
        }
    }

    issues
}

fn other_path(paths: &[String], self_path: &str) -> String {
    for p in paths {
        if p != self_path {
            return p.clone();
        }
    }
    String::new()
}

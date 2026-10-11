//! Rules for TRACKNUMBER and DISCNUMBER validation and normalization.

use crate::profiles::Profile;
use crate::rules::helpers::{
    albums, disc_set, effective_track_number, filename_track_number, highest_track_number,
    looks_multi_disc, single_album, tag_op, track_set, tracks_per_disc,
};
use crate::types::{Issue, Operation, Scan, Severity};
use std::collections::{HashMap, HashSet};

/// Detects missing, unparseable, duplicate, or gapped track numbers.
pub fn track_number_detect(scan: &Scan, _profile: &Profile) -> Vec<Issue> {
    let mut issues = Vec::new();
    let albums_map = albums(scan);

    for (dir, tracks) in albums_map {
        if !single_album(&dir, &tracks) {
            continue;
        }

        let per_disc = tracks_per_disc(&tracks);
        for (disc, disc_tracks) in per_disc {
            let mut seen: HashMap<usize, String> = HashMap::new();
            let mut max_num = 0;

            for t in &disc_tracks {
                if t.track_number == 0 && !t.track_number_raw.is_empty() {
                    issues.push(Issue {
                        id: String::new(),
                        rule_id: "TRACKNUMBER".to_string(),
                        severity: Severity::Warning,
                        path: t.path.clone(),
                        album_dir: dir.clone(),
                        message: format!("unparseable track number \"{}\"", t.track_number_raw),
                        fixable: filename_track_number(&t.rel_path) > 0,
                    });
                    continue;
                } else if t.track_number == 0 {
                    issues.push(Issue {
                        id: String::new(),
                        rule_id: "TRACKNUMBER".to_string(),
                        severity: Severity::Warning,
                        path: t.path.clone(),
                        album_dir: dir.clone(),
                        message: "missing track number".to_string(),
                        fixable: filename_track_number(&t.rel_path) > 0,
                    });
                    continue;
                }

                if let Some(other) = seen.get(&t.track_number) {
                    issues.push(Issue {
                        id: String::new(),
                        rule_id: "TRACKNUMBER".to_string(),
                        severity: Severity::Warning,
                        path: t.path.clone(),
                        album_dir: dir.clone(),
                        message: format!(
                            "track number {} is also used by {}",
                            t.track_number, other
                        ),
                        fixable: false,
                    });
                }
                seen.insert(t.track_number, t.rel_path.clone());
                if t.track_number > max_num {
                    max_num = t.track_number;
                }

                if t.format == "flac" && t.track_number_raw.contains('/') {
                    issues.push(Issue {
                        id: String::new(),
                        rule_id: "TRACKNUMBER".to_string(),
                        severity: Severity::Warning,
                        path: t.path.clone(),
                        album_dir: dir.clone(),
                        message: format!(
                            "track number stored as \"{}\" instead of separate TRACKNUMBER/TRACKTOTAL",
                            t.track_number_raw
                        ),
                        fixable: true,
                    });
                }
            }

            let highest = highest_track_number(&disc_tracks);
            for t in &disc_tracks {
                if t.track_number > 0 && t.track_total > 0 && t.track_total < highest {
                    issues.push(Issue {
                        id: String::new(),
                        rule_id: "TRACKNUMBER".to_string(),
                        severity: Severity::Warning,
                        path: t.path.clone(),
                        album_dir: dir.clone(),
                        message: format!(
                            "track total {} is lower than track number {}",
                            t.track_total, highest
                        ),
                        fixable: true,
                    });
                }
            }

            if max_num > seen.len() {
                issues.push(Issue {
                    id: String::new(),
                    rule_id: "TRACKNUMBER".to_string(),
                    severity: Severity::Warning,
                    path: dir.clone(),
                    album_dir: dir.clone(),
                    message: format!(
                        "disc {} has {} numbered tracks up to {}; some tracks may be missing",
                        disc,
                        seen.len(),
                        max_num
                    ),
                    fixable: false,
                });
            }
        }
    }

    issues
}

/// Fixes track numbers and totals.
pub fn track_number_fix(scan: &Scan, _profile: &Profile, issues: &[Issue]) -> Vec<Operation> {
    let albums_map = albums(scan);
    let mut done = HashSet::new();
    let mut ops = Vec::new();

    for issue in issues {
        let t = match scan.tracks.iter().find(|tr| tr.path == issue.path) {
            Some(tr) => tr,
            None => continue,
        };

        if !done.insert(t.path.clone()) {
            continue;
        }

        let num = effective_track_number(t);
        if num == 0 {
            continue;
        }

        let disc = if t.disc_number == 0 { 1 } else { t.disc_number };
        let mut total = t.track_total;

        if let Some(album_tracks) = albums_map.get(&issue.album_dir) {
            let per_disc = tracks_per_disc(album_tracks);
            if let Some(disc_tracks) = per_disc.get(&disc) {
                let highest = highest_track_number(disc_tracks);
                if total < highest {
                    total = highest;
                }
            }
        }

        ops.push(tag_op(
            t,
            track_set(&t.format, num, total),
            "normalise track number and total",
        ));
    }

    ops
}

/// Detects inconsistent or missing DISCNUMBER tags across multi-disc sets.
pub fn disc_number_detect(scan: &Scan, _profile: &Profile) -> Vec<Issue> {
    let mut issues = Vec::new();
    let albums_map = albums(scan);

    for (dir, tracks) in albums_map {
        if !single_album(&dir, &tracks) {
            continue;
        }

        let mut has_set = false;
        let mut has_missing = false;
        for t in &tracks {
            if t.disc_number > 0 {
                has_set = true;
            } else {
                has_missing = true;
            }
        }

        if has_missing && has_set {
            issues.push(Issue {
                id: String::new(),
                rule_id: "DISCNUMBER".to_string(),
                severity: Severity::Warning,
                path: dir.clone(),
                album_dir: dir.clone(),
                message: "disc numbers are inconsistent across the album".to_string(),
                fixable: true,
            });
        } else if has_missing && looks_multi_disc(&dir) {
            issues.push(Issue {
                id: String::new(),
                rule_id: "DISCNUMBER".to_string(),
                severity: Severity::Warning,
                path: dir.clone(),
                album_dir: dir.clone(),
                message: "folder looks like one disc of a set but has no disc numbers".to_string(),
                fixable: false,
            });
        }
    }

    issues
}

/// Fixes disc numbers by assigning consistent DISCNUMBER and DISCTOTAL.
pub fn disc_number_fix(scan: &Scan, _profile: &Profile, issues: &[Issue]) -> Vec<Operation> {
    let albums_map = albums(scan);
    let mut seen = HashSet::new();
    let mut ops = Vec::new();

    for issue in issues {
        if !seen.insert(issue.album_dir.clone()) {
            continue;
        }

        let tracks = match albums_map.get(&issue.album_dir) {
            Some(t) => t,
            None => continue,
        };

        let mut max_disc = 1;
        for t in tracks {
            if t.disc_number > max_disc {
                max_disc = t.disc_number;
            }
        }

        for t in tracks {
            if t.disc_number != 0 && t.disc_total == max_disc {
                continue;
            }
            let disc = if t.disc_number == 0 { 1 } else { t.disc_number };
            ops.push(tag_op(
                t,
                disc_set(&t.format, disc, max_disc),
                "normalise disc number",
            ));
        }
    }

    ops
}

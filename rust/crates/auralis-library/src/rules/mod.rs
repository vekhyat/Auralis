//! Library Doctor rules registry and execution engine.
//!
//! Provides rule detection, fix planning, health score calculation,
//! and projected tag state planning.

pub mod album;
pub mod covers;
pub mod duplicates;
pub mod helpers;
pub mod mixed;
pub mod orphans;
pub mod paths;
pub mod tags;

use crate::profiles::Profile;
use crate::types::{Issue, Operation, Plan, Scan, Severity};
use std::collections::{BTreeMap, HashMap, HashSet};

pub type DetectFn = fn(&Scan, &Profile) -> Vec<Issue>;
pub type FixFn = fn(&Scan, &Profile, &[Issue]) -> Vec<Operation>;

/// Rule is one Library Doctor check.
#[derive(Clone, Copy)]
pub struct Rule {
    pub id: &'static str,
    pub severity: Severity,
    pub detect: DetectFn,
    pub fix: Option<FixFn>,
}

pub static RULES: &[Rule] = &[
    Rule {
        id: "ALBUMARTIST",
        severity: Severity::Error,
        detect: album::album_artist_detect,
        fix: Some(album::album_artist_fix),
    },
    Rule {
        id: "COMPILATION",
        severity: Severity::Warning,
        detect: album::compilation_detect,
        fix: Some(album::compilation_fix),
    },
    Rule {
        id: "TRACKNUMBER",
        severity: Severity::Warning,
        detect: tags::track_number_detect,
        fix: Some(tags::track_number_fix),
    },
    Rule {
        id: "DISCNUMBER",
        severity: Severity::Warning,
        detect: tags::disc_number_detect,
        fix: Some(tags::disc_number_fix),
    },
    Rule {
        id: "COVER",
        severity: Severity::Warning,
        detect: covers::cover_detect,
        fix: None,
    },
    Rule {
        id: "PATH",
        severity: Severity::Error,
        detect: paths::path_detect,
        fix: Some(paths::path_fix),
    },
    Rule {
        id: "MIXEDFORMAT",
        severity: Severity::Info,
        detect: mixed::mixed_format_detect,
        fix: None,
    },
    Rule {
        id: "DUPLICATES",
        severity: Severity::Info,
        detect: duplicates::duplicate_detect,
        fix: None,
    },
    Rule {
        id: "ORPHAN",
        severity: Severity::Info,
        detect: orphans::orphan_detect,
        fix: Some(orphans::orphan_fix),
    },
];

/// Returns the static slice of registered Doctor rules.
pub fn rules() -> &'static [Rule] {
    RULES
}

/// Finds a rule by its uppercase ID.
pub fn rule_by_id(id: &str) -> Option<&'static Rule> {
    RULES.iter().find(|r| r.id.eq_ignore_ascii_case(id))
}

/// DetectAll runs every rule over the scan and returns all issues, sorted.
pub fn detect_all(scan: &Scan, profile: &Profile) -> Vec<Issue> {
    let mut issues = Vec::new();
    for rule in RULES {
        let mut detected = (rule.detect)(scan, profile);
        for issue in &mut detected {
            if issue.rule_id.is_empty() {
                issue.rule_id = rule.id.to_string();
            }
            if issue.fixable && rule.fix.is_none() {
                issue.fixable = false;
            }
            if issue.id.is_empty() {
                issue.id = format!("{}|{}|{}", rule.id, issue.path, issue.message);
            }
        }
        issues.extend(detected);
    }
    issues.sort_by(|a, b| {
        if a.rule_id != b.rule_id {
            a.rule_id.cmp(&b.rule_id)
        } else {
            a.id.cmp(&b.id)
        }
    });
    dedupe_issue_ids(issues)
}

fn dedupe_issue_ids(issues: Vec<Issue>) -> Vec<Issue> {
    let mut seen = HashSet::new();
    let mut out = Vec::new();
    for issue in issues {
        if seen.insert(issue.id.clone()) {
            out.push(issue);
        }
    }
    out
}

/// BuildPlan transforms fixable issues into a deterministic, ordered list of Operations.
pub fn build_plan(scan: &Scan, profile: &Profile, issues: &[Issue]) -> Plan {
    let mut by_rule: HashMap<String, Vec<Issue>> = HashMap::new();
    for issue in issues {
        if !issue.fixable {
            continue;
        }
        by_rule
            .entry(issue.rule_id.clone())
            .or_default()
            .push(issue.clone());
    }

    let mut plan = Plan::default();
    let mut seen = HashSet::new();
    let mut tag_ops: HashMap<String, usize> = HashMap::new();

    let mut rule_ids: Vec<String> = by_rule.keys().cloned().collect();
    rule_ids.sort_by(|a, b| {
        let a_is_path = a == "PATH";
        let b_is_path = b == "PATH";
        if a_is_path != b_is_path {
            b_is_path.cmp(&a_is_path)
        } else {
            a.cmp(b)
        }
    });

    for rule_id in &rule_ids {
        let rule = match rule_by_id(rule_id) {
            Some(r) => r,
            None => continue,
        };
        let fix = match rule.fix {
            Some(f) => f,
            None => continue,
        };
        let rule_scan = if rule_id == "PATH" {
            project_tags(scan, &plan.operations)
        } else {
            scan.clone()
        };
        let ops = fix(&rule_scan, profile, &by_rule[rule_id]);
        for op in ops {
            if op.op_type == "tags" {
                if let Some(&i) = tag_ops.get(&op.path) {
                    merge_tag_ops(&mut plan.operations[i], &op);
                    continue;
                }
                tag_ops.insert(op.path.clone(), plan.operations.len());
            }
            let key = operation_key(&op);
            if seen.contains(&key) {
                continue;
            }
            seen.insert(key);
            plan.operations.push(op);
        }
    }

    // Read current tag values so preview shows before/after diff
    for op in &mut plan.operations {
        if op.op_type != "tags" {
            continue;
        }
        let set = match op.set {
            Some(ref s) if !s.is_empty() => s,
            _ => continue,
        };
        if let Ok(tags) = crate::tag_io::read_tags(std::path::Path::new(&op.path)) {
            let mut old = BTreeMap::new();
            let mut by_upper: HashMap<String, String> = HashMap::new();
            for key in tags.keys() {
                by_upper.insert(key.to_ascii_uppercase(), key.clone());
            }
            let mut all_keys: Vec<String> = set.keys().cloned().collect();
            if let Some(ref del) = op.delete {
                all_keys.extend(del.clone());
            }
            for key in all_keys {
                if let Some(actual) = by_upper.get(&key.to_ascii_uppercase()) {
                    old.insert(key, crate::tag_io::first_tag(&tags, &[actual]));
                } else {
                    old.insert(key, String::new());
                }
            }
            op.old = Some(old);
        }
    }

    // A move that would overwrite another planned destination is unsafe
    let mut dests: HashMap<String, usize> = HashMap::new();
    for op in &mut plan.operations {
        if op.op_type == "move" {
            if let Some(ref new_p) = op.new_path {
                let lower = new_p.to_lowercase();
                if *dests.get(&lower).unwrap_or(&0) > 0 {
                    op.error = Some("destination conflict with another planned move".to_string());
                }
                *dests.entry(lower).or_insert(0) += 1;
            }
        }
    }

    // Retag before moving, and remove folders last
    plan.operations.sort_by(|a, b| {
        let op_order = |t: &str| match t {
            "tags" => 0,
            "move" => 1,
            "rmdir" => 2,
            _ => 3,
        };
        let ord_a = op_order(&a.op_type);
        let ord_b = op_order(&b.op_type);
        if ord_a != ord_b {
            ord_a.cmp(&ord_b)
        } else {
            a.path.cmp(&b.path)
        }
    });

    plan
}

/// Calculates the overall library health score from 0 to 100.
pub fn calculate_health_score(total_tracks: usize, issues: &[Issue]) -> i64 {
    if total_tracks == 0 || issues.is_empty() {
        return 100;
    }
    let mut penalty = 0;
    for issue in issues {
        match issue.severity {
            Severity::Error => penalty += 4,
            Severity::Warning => penalty += 2,
            _ => penalty += 1,
        }
    }
    let ratio = (penalty as f64) / ((total_tracks * 3) as f64);
    let ratio = if ratio > 1.0 { 1.0 } else { ratio };
    let mut score = ((1.0 - ratio) * 100.0) as i64;
    if score >= 100 {
        score = 99;
    }
    score
}

/// Counts total distinct albums in a scan.
pub fn count_albums(scan: &Scan) -> usize {
    let mut albums = HashSet::new();
    for t in &scan.tracks {
        albums.insert(t.album_dir());
    }
    albums.len()
}

/// Applies a set of tag operations onto a clone of the scan in memory.
pub fn project_tags(scan: &Scan, ops: &[Operation]) -> Scan {
    let mut cloned = scan.clone();
    let mut by_path: HashMap<String, usize> = HashMap::new();
    for (i, t) in cloned.tracks.iter().enumerate() {
        by_path.insert(t.path.clone(), i);
    }

    for op in ops {
        if op.op_type != "tags" || op.error.as_deref().is_some_and(|e| !e.is_empty()) {
            continue;
        }
        if let Some(&i) = by_path.get(&op.path) {
            let t = &mut cloned.tracks[i];
            if let Some(ref set) = op.set {
                for (k, v) in set {
                    match k.to_ascii_uppercase().as_str() {
                        "ALBUMARTIST" => t.album_artist = v.clone(),
                        "TRACKNUMBER" => {
                            let (num, _) = split_number_total(v);
                            t.track_number = num;
                        }
                        "DISCNUMBER" => {
                            let (num, _) = split_number_total(v);
                            t.disc_number = num;
                        }
                        "DISCTOTAL" => {
                            t.disc_total = parse_leading_int(v);
                        }
                        _ => {}
                    }
                }
                if let Some(raw) = set.get("DISCNUMBER") {
                    if raw.contains('/') {
                        let (_, total) = split_number_total(raw);
                        t.disc_total = total;
                    }
                }
            }
        }
    }
    cloned
}

fn split_number_total(s: &str) -> (usize, usize) {
    if let Some((n, tot)) = s.split_once('/') {
        (parse_leading_int(n), parse_leading_int(tot))
    } else {
        (parse_leading_int(s), 0)
    }
}

fn parse_leading_int(s: &str) -> usize {
    let digits: String = s.chars().take_while(|c| c.is_ascii_digit()).collect();
    digits.parse().unwrap_or(0)
}

/// Merges two tag operations targeting the same file.
pub fn merge_tag_ops(dst: &mut Operation, src: &Operation) {
    let dst_set = dst.set.get_or_insert_with(BTreeMap::new);
    if let Some(ref src_set) = src.set {
        for (k, v) in src_set {
            if let Some(prev) = dst_set.get(k) {
                if prev != v {
                    dst.error = Some(format!("conflicting fixes for {k}"));
                    continue;
                }
            }
            dst_set.insert(k.clone(), v.clone());
        }
    }

    if let Some(ref src_del) = src.delete {
        let dst_del = dst.delete.get_or_insert_with(Vec::new);
        for key in src_del {
            if !dst_set.contains_key(key) && !dst_del.iter().any(|k| k.eq_ignore_ascii_case(key)) {
                dst_del.push(key.clone());
            }
        }
    }

    if !src.reason.is_empty() && !dst.reason.contains(&src.reason) {
        if dst.reason.is_empty() {
            dst.reason = src.reason.clone();
        } else {
            dst.reason = format!("{}; {}", dst.reason, src.reason);
        }
    }
}

pub fn operation_key(op: &Operation) -> String {
    let mut b = format!(
        "{}|{}|{}",
        op.op_type,
        op.path,
        op.new_path.as_deref().unwrap_or("")
    );
    if let Some(ref set) = op.set {
        for (k, v) in set {
            b.push('|');
            b.push_str(k);
            b.push('=');
            b.push_str(v);
        }
    }
    b
}

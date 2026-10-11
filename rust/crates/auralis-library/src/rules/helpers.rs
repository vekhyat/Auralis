//! Helper utilities for Library Doctor rules.

use crate::types::{Operation, Scan, Track};
use regex::Regex;
use std::collections::{BTreeMap, HashMap, HashSet};
use std::sync::OnceLock;

static MULTI_DISC_REGEX: OnceLock<Regex> = OnceLock::new();
static FILENAME_TRACK_REGEX: OnceLock<Regex> = OnceLock::new();

fn multi_disc_regex() -> &'static Regex {
    MULTI_DISC_REGEX.get_or_init(|| Regex::new(r"(?i)\b(?:disc|disk|cd)\s*\d").unwrap())
}

fn filename_track_regex() -> &'static Regex {
    FILENAME_TRACK_REGEX
        .get_or_init(|| Regex::new(r"^(?:\d{1,2}-)?(\d{1,3})(?:[ ._)-]|$)").unwrap())
}

/// Checks whether folder name indicates a multi-disc set (e.g. "CD1", "Disc 2").
pub fn looks_multi_disc(dir: &str) -> bool {
    let base = match dir.rfind('/') {
        Some(idx) => &dir[idx + 1..],
        None => dir,
    };
    multi_disc_regex().is_match(base)
}

/// Checks whether a collection of tracks in a directory belong to a single album.
pub fn single_album(dir: &str, tracks: &[Track]) -> bool {
    if dir.is_empty() {
        return false;
    }
    let mut albums = HashSet::new();
    for t in tracks {
        let a = t.album.trim().to_lowercase();
        if !a.is_empty() {
            albums.insert(a);
        }
    }
    albums.len() <= 1
}

/// Groups scan tracks by their album_dir.
pub fn albums(scan: &Scan) -> BTreeMap<String, Vec<Track>> {
    let mut map: BTreeMap<String, Vec<Track>> = BTreeMap::new();
    for t in &scan.tracks {
        let dir = t.album_dir().to_string();
        map.entry(dir).or_default().push(t.clone());
    }
    map
}

/// Recovers a track number from the file name, or returns 0.
pub fn filename_track_number(rel_path: &str) -> usize {
    let base = match rel_path.rfind('/') {
        Some(idx) => &rel_path[idx + 1..],
        None => rel_path,
    };
    let file_stem = match base.rfind('.') {
        Some(idx) => &base[..idx],
        None => base,
    };
    if let Some(caps) = filename_track_regex().captures(file_stem) {
        if let Some(m) = caps.get(1) {
            return m.as_str().parse().unwrap_or(0);
        }
    }
    0
}

/// Returns the tagged track number, or the one parsed from the filename.
pub fn effective_track_number(t: &Track) -> usize {
    if t.track_number > 0 {
        t.track_number
    } else {
        filename_track_number(&t.rel_path)
    }
}

/// Returns the highest effective track number in the slice.
pub fn highest_track_number(tracks: &[&Track]) -> usize {
    let mut highest = 0;
    for t in tracks {
        let n = effective_track_number(t);
        if n > highest {
            highest = n;
        }
    }
    highest
}

/// Groups tracks by disc number (treating 0 as 1).
pub fn tracks_per_disc<'a>(tracks: &'a [Track]) -> BTreeMap<usize, Vec<&'a Track>> {
    let mut map: BTreeMap<usize, Vec<&'a Track>> = BTreeMap::new();
    for t in tracks {
        let disc = if t.disc_number == 0 { 1 } else { t.disc_number };
        map.entry(disc).or_default().push(t);
    }
    map
}

/// Returns the container-appropriate tag overlay for a track number.
pub fn track_set(format: &str, number: usize, total: usize) -> BTreeMap<String, String> {
    let mut m = BTreeMap::new();
    match format {
        "flac" | "ogg" => {
            m.insert("TRACKNUMBER".to_string(), number.to_string());
            if total > 0 {
                m.insert("TRACKTOTAL".to_string(), total.to_string());
                m.insert("TOTALTRACKS".to_string(), total.to_string());
            }
        }
        _ => {
            if total > 0 {
                m.insert("TRACKNUMBER".to_string(), format!("{number}/{total}"));
            } else {
                m.insert("TRACKNUMBER".to_string(), number.to_string());
            }
        }
    }
    m
}

/// Returns the container-appropriate tag overlay for a disc number.
pub fn disc_set(format: &str, number: usize, total: usize) -> BTreeMap<String, String> {
    let mut m = BTreeMap::new();
    match format {
        "flac" | "ogg" => {
            m.insert("DISCNUMBER".to_string(), number.to_string());
            if total > 0 {
                m.insert("DISCTOTAL".to_string(), total.to_string());
                m.insert("TOTALDISCS".to_string(), total.to_string());
            }
        }
        _ => {
            if total > 0 {
                m.insert("DISCNUMBER".to_string(), format!("{number}/{total}"));
            } else {
                m.insert("DISCNUMBER".to_string(), number.to_string());
            }
        }
    }
    m
}

/// Counts frequencies of distinct non-empty strings.
pub fn group_distinct<'a>(values: impl IntoIterator<Item = &'a str>) -> HashMap<String, usize> {
    let mut m = HashMap::new();
    for v in values {
        let trimmed = v.trim();
        if !trimmed.is_empty() {
            *m.entry(trimmed.to_string()).or_default() += 1;
        }
    }
    m
}

/// Returns the most common string with deterministic tie-breaking.
pub fn most_common(counts: &HashMap<String, usize>) -> String {
    let mut best = String::new();
    let mut max = 0;
    for (val, &count) in counts {
        if count > max || (count == max && (best.is_empty() || val < &best)) {
            best = val.clone();
            max = count;
        }
    }
    best
}

/// Builds a tag operation for a track.
pub fn tag_op(t: &Track, set: BTreeMap<String, String>, reason: &str) -> Operation {
    Operation {
        op_type: "tags".to_string(),
        path: t.path.clone(),
        set: Some(set),
        reason: reason.to_string(),
        size: t.size,
        mod_time: t.mod_time_unix_nano,
        ..Default::default()
    }
}

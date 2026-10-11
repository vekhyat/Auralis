//! Fix journal and transaction engine (JSONL append-only journal, apply & undo).
//!
//! Provides strict byte-for-byte JSON format compatibility with the Go implementation,
//! ensuring journal files can be inspected and reversed across Go and Rust implementations.

use crate::tag_io::{
    apply_tags as io_apply_tags, restore_tags as io_restore_tags, system_time_to_unix_nano,
};
use crate::types::{ApplyResult, JournalEntry, Plan};
use std::collections::HashSet;
use std::fs::{self, OpenOptions};
use std::io::{BufRead, BufReader, Write};
use std::path::Path;
use uuid::Uuid;

/// Injected synchronization interface for notifying the library database / search index
/// of moved tracks and sidecar files.
pub trait LibraryIndexSync: Send + Sync {
    fn move_index_file(&self, old_path: &str, new_path: &str);
}

impl<F> LibraryIndexSync for F
where
    F: Fn(&str, &str) + Send + Sync,
{
    fn move_index_file(&self, old_path: &str, new_path: &str) {
        (self)(old_path, new_path);
    }
}

pub struct NoOpIndexSync;
impl LibraryIndexSync for NoOpIndexSync {
    fn move_index_file(&self, _old_path: &str, _new_path: &str) {}
}

/// Appends a single entry to the JSONL journal file using Go-compatible JSON encoding.
pub fn append_journal(journal_path: impl AsRef<Path>, entry: &JournalEntry) -> Result<(), String> {
    let p = journal_path.as_ref();
    if let Some(parent) = p.parent() {
        if !parent.as_os_str().is_empty() {
            fs::create_dir_all(parent).map_err(|e| format!("cannot create dir {parent:?}: {e}"))?;
        }
    }
    let mut file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(p)
        .map_err(|e| format!("cannot open journal {p:?}: {e}"))?;

    let bytes = auralis_core::gojson::to_vec(entry).map_err(|e| format!("json serialize: {e}"))?;
    file.write_all(&bytes)
        .map_err(|e| format!("write journal: {e}"))?;
    file.write_all(b"\n")
        .map_err(|e| format!("write journal newline: {e}"))?;
    Ok(())
}

/// Reads all entries from a JSONL journal file, skipping corrupted lines.
pub fn read_journal(journal_path: impl AsRef<Path>) -> Result<Vec<JournalEntry>, String> {
    let p = journal_path.as_ref();
    if !p.exists() {
        return Ok(Vec::new());
    }
    let file = fs::File::open(p).map_err(|e| format!("cannot open {p:?}: {e}"))?;
    let reader = BufReader::new(file);
    let mut entries = Vec::new();

    for line in reader.lines() {
        let line = match line {
            Ok(l) => l,
            Err(_) => continue,
        };
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        if let Ok(entry) = serde_json::from_str::<JournalEntry>(trimmed) {
            entries.push(entry);
        }
    }

    Ok(entries)
}

/// Identifies the latest active (non-undone) batch of operations.
pub fn last_batch(entries: &[JournalEntry]) -> Vec<JournalEntry> {
    let mut undone = HashSet::new();
    for e in entries {
        if e.op_type == "undo" {
            undone.insert(e.batch_id.clone());
        }
    }

    let mut last_id = String::new();
    for e in entries.iter().rev() {
        if e.op_type == "undo" || undone.contains(&e.batch_id) {
            continue;
        }
        last_id = e.batch_id.clone();
        break;
    }

    if last_id.is_empty() {
        return Vec::new();
    }

    entries
        .iter()
        .filter(|e| e.batch_id == last_id && e.op_type != "undo")
        .cloned()
        .collect()
}

fn unchanged_since(path: &str, size: i64, mod_time: i64) -> Result<(), String> {
    let meta = match fs::metadata(path) {
        Ok(m) => m,
        Err(e) => return Err(format!("cannot stat: {e}")),
    };
    if size != 0 && (meta.len() as i64) != size {
        return Err("file size changed since scan".to_string());
    }
    if mod_time != 0 {
        let current_mod = meta.modified().map(system_time_to_unix_nano).unwrap_or(0);
        if current_mod != mod_time {
            return Err("file changed since scan".to_string());
        }
    }
    Ok(())
}

fn move_sidecars(old_audio: &str, new_audio: &str) {
    let old_ext = Path::new(old_audio)
        .extension()
        .and_then(|s| s.to_str())
        .unwrap_or("");
    let new_ext = Path::new(new_audio)
        .extension()
        .and_then(|s| s.to_str())
        .unwrap_or("");
    let old_ext_with_dot = if old_ext.is_empty() {
        String::new()
    } else {
        format!(".{old_ext}")
    };
    let new_ext_with_dot = if new_ext.is_empty() {
        String::new()
    } else {
        format!(".{new_ext}")
    };
    let old_base = old_audio
        .strip_suffix(&old_ext_with_dot)
        .unwrap_or(old_audio);
    let new_base = new_audio
        .strip_suffix(&new_ext_with_dot)
        .unwrap_or(new_audio);
    for ext in &[".lrc"] {
        let old_sidecar = format!("{old_base}{ext}");
        let new_sidecar = format!("{new_base}{ext}");
        if !Path::new(&old_sidecar).exists() || Path::new(&new_sidecar).exists() {
            continue;
        }
        if let Some(parent) = Path::new(&new_sidecar).parent() {
            let _ = fs::create_dir_all(parent);
        }
        let _ = fs::rename(&old_sidecar, &new_sidecar);
    }
}

/// Applies a planned set of operations, appending each applied step to the journal.
///
/// Returns `Ok(ApplyResult)` on completion, or `Err((ApplyResult, String))` if execution fails
/// or is cancelled (preserving the partial batch result for cancellation).
pub fn apply<C: Fn() -> bool>(
    root: impl AsRef<Path>,
    journal_path: impl AsRef<Path>,
    plan: &Plan,
    index_sync: Option<&dyn LibraryIndexSync>,
    is_cancelled: Option<C>,
) -> Result<ApplyResult, (ApplyResult, String)> {
    let root = root.as_ref();
    let root_str = root.to_string_lossy().trim().to_string();
    if root_str.is_empty() {
        return Err((
            ApplyResult::default(),
            "library root is required".to_string(),
        ));
    }

    let batch_id = Uuid::new_v4().to_string();
    let mut result = ApplyResult {
        batch_id: batch_id.clone(),
        applied: 0,
        skipped: 0,
        errors: Vec::new(),
    };

    // Files retagged in this batch no longer match their scan stamp; the check already
    // passed before that write.
    let mut written = HashSet::new();

    for op in &plan.operations {
        if let Some(ref check) = is_cancelled {
            if check() {
                return Err((result, "context canceled".to_string()));
            }
        }

        if let Some(ref err) = op.error {
            result.skipped += 1;
            result.errors.push(format!("{}: {err}", op.path));
            continue;
        }

        let has_new_path = op
            .new_path
            .as_deref()
            .map(|s| !s.is_empty())
            .unwrap_or(false);
        let new_path_str = op.new_path.as_deref().unwrap_or("");

        if !within_root(root, &op.path) || (has_new_path && !within_root(root, new_path_str)) {
            result.skipped += 1;
            result
                .errors
                .push(format!("{}: path is outside the library root", op.path));
            continue;
        }

        let mut op_size = op.size;
        let mut op_mod_time = op.mod_time;
        if written.contains(&op.path) {
            op_size = 0;
            op_mod_time = 0;
        }

        match op.op_type.as_str() {
            "tags" => {
                if let Err(e) = unchanged_since(&op.path, op_size, op_mod_time) {
                    result.skipped += 1;
                    result.errors.push(format!("{}: {e}", op.path));
                    continue;
                }

                let p = Path::new(&op.path);
                let set = op.set.clone().unwrap_or_default();
                let del = op.delete.clone().unwrap_or_default();

                match io_apply_tags(p, &set, &del) {
                    Ok(old_vals) => {
                        written.insert(op.path.clone());
                        let now = chrono::Utc::now().format("%Y-%m-%dT%H:%M:%SZ").to_string();
                        let entry = JournalEntry {
                            batch_id: batch_id.clone(),
                            time: now,
                            op_type: "tags".to_string(),
                            path: op.path.clone(),
                            set,
                            delete: del,
                            old: old_vals,
                            ..Default::default()
                        };
                        if let Err(err) = append_journal(&journal_path, &entry) {
                            result.errors.push(format!("journal: {err}"));
                        }
                        result.applied += 1;
                    }
                    Err(e) => {
                        result.skipped += 1;
                        result.errors.push(format!("{}: {e}", op.path));
                    }
                }
            }

            "move" => {
                let new_path = new_path_str;
                if let Err(e) = unchanged_since(&op.path, op_size, op_mod_time) {
                    result.skipped += 1;
                    result.errors.push(format!("{}: {e}", op.path));
                    continue;
                }

                if Path::new(new_path).exists() {
                    result.skipped += 1;
                    result.errors.push(format!(
                        "{}: destination already exists: {new_path}",
                        op.path
                    ));
                    continue;
                }

                if let Some(parent) = Path::new(new_path).parent() {
                    if let Err(e) = fs::create_dir_all(parent) {
                        result.skipped += 1;
                        result.errors.push(format!("{}: {e}", op.path));
                        continue;
                    }
                }

                if let Err(e) = fs::rename(&op.path, new_path) {
                    result.skipped += 1;
                    result.errors.push(format!("{}: {e}", op.path));
                    continue;
                }

                move_sidecars(&op.path, new_path);

                if let Some(sync) = index_sync {
                    sync.move_index_file(&op.path, new_path);
                }

                let now = chrono::Utc::now().format("%Y-%m-%dT%H:%M:%SZ").to_string();
                let entry = JournalEntry {
                    batch_id: batch_id.clone(),
                    time: now,
                    op_type: "move".to_string(),
                    path: op.path.clone(),
                    new_path: new_path.to_string(),
                    size: op_size,
                    mod_time: op_mod_time,
                    ..Default::default()
                };
                if let Err(err) = append_journal(&journal_path, &entry) {
                    result.errors.push(format!("journal: {err}"));
                }
                result.applied += 1;
            }

            "rmdir" => {
                let entries = match fs::read_dir(&op.path) {
                    Ok(rd) => rd,
                    Err(e) => {
                        result.skipped += 1;
                        result.errors.push(format!("{}: {e}", op.path));
                        continue;
                    }
                };
                if entries.count() != 0 {
                    result.skipped += 1;
                    result
                        .errors
                        .push(format!("{}: folder is no longer empty", op.path));
                    continue;
                }

                if let Err(e) = fs::remove_dir(&op.path) {
                    result.skipped += 1;
                    result.errors.push(format!("{}: {e}", op.path));
                    continue;
                }

                let now = chrono::Utc::now().format("%Y-%m-%dT%H:%M:%SZ").to_string();
                let entry = JournalEntry {
                    batch_id: batch_id.clone(),
                    time: now,
                    op_type: "rmdir".to_string(),
                    path: op.path.clone(),
                    ..Default::default()
                };
                if let Err(err) = append_journal(&journal_path, &entry) {
                    result.errors.push(format!("journal: {err}"));
                }
                result.applied += 1;
            }

            other => {
                result.skipped += 1;
                result
                    .errors
                    .push(format!("{}: unknown operation type \"{other}\"", op.path));
            }
        }
    }

    Ok(result)
}

/// Reverses the most recent batch recorded in the fix journal.
pub fn undo_last(
    journal_path: impl AsRef<Path>,
    index_sync: Option<&dyn LibraryIndexSync>,
) -> Result<ApplyResult, String> {
    let entries = read_journal(&journal_path)?;
    let batch = last_batch(&entries);

    let mut result = ApplyResult {
        batch_id: String::new(),
        applied: 0,
        skipped: 0,
        errors: Vec::new(),
    };

    if batch.is_empty() {
        return Ok(result);
    }

    result.batch_id = batch[0].batch_id.clone();

    // Replay in reverse order
    for entry in batch.iter().rev() {
        match entry.op_type.as_str() {
            "tags" => {
                let p = Path::new(&entry.path);
                if let Err(e) = io_restore_tags(p, &entry.old) {
                    result.skipped += 1;
                    result.errors.push(format!("{}: {e}", entry.path));
                    continue;
                }
                result.applied += 1;
            }

            "move" => {
                let current_loc = Path::new(&entry.new_path);
                let orig_loc = Path::new(&entry.path);

                // Already gone: return nil / count as applied
                if fs::metadata(current_loc).is_err() {
                    result.applied += 1;
                    continue;
                }
                // Something is already at the old location: count as applied
                if fs::metadata(orig_loc).is_ok() {
                    result.applied += 1;
                    continue;
                }

                if let Some(parent) = orig_loc.parent() {
                    if let Err(e) = fs::create_dir_all(parent) {
                        result.skipped += 1;
                        result.errors.push(format!("{}: {e}", entry.path));
                        continue;
                    }
                }

                if let Err(e) = fs::rename(current_loc, orig_loc) {
                    result.skipped += 1;
                    result.errors.push(format!("{}: {e}", entry.path));
                    continue;
                }

                move_sidecars(&entry.new_path, &entry.path);

                if let Some(sync) = index_sync {
                    sync.move_index_file(&entry.new_path, &entry.path);
                }

                result.applied += 1;
            }

            "rmdir" => {
                let p = Path::new(&entry.path);
                if let Err(e) = fs::create_dir_all(p) {
                    result.skipped += 1;
                    result.errors.push(format!("{}: {e}", entry.path));
                    continue;
                }
                result.applied += 1;
            }

            _ => {
                result.applied += 1;
            }
        }
    }

    // Write undo marker to finalize reversal
    let now = chrono::Utc::now().format("%Y-%m-%dT%H:%M:%SZ").to_string();
    let undo_entry = JournalEntry {
        batch_id: result.batch_id.clone(),
        time: now,
        op_type: "undo".to_string(),
        ..Default::default()
    };
    append_journal(&journal_path, &undo_entry)?;

    Ok(result)
}

/// Helper that verifies a path resides strictly inside the library root.
/// Performs lexical analysis matching Go's filepath.Rel without filesystem calls or case-folding.
pub fn within_root(root: impl AsRef<Path>, target: impl AsRef<Path>) -> bool {
    let r = root.as_ref().to_string_lossy();
    let t = target.as_ref().to_string_lossy();
    within_root_str(&r, &t)
}

fn split_volume_and_path(s: &str) -> (&str, bool, &str) {
    let s = s.trim();
    let bytes = s.as_bytes();
    // Check Windows drive letter: e.g. "C:" or "c:"
    if bytes.len() >= 2 && bytes[0].is_ascii_alphabetic() && bytes[1] == b':' {
        let vol = &s[..2];
        let rest = &s[2..];
        let is_abs = rest.starts_with('/') || rest.starts_with('\\');
        (vol, is_abs, rest)
    } else if s.starts_with("\\\\") || s.starts_with("//") {
        // UNC path: \\server\share\...
        let trimmed = &s[2..];
        if let Some(first_slash) = trimmed.find(['/', '\\']) {
            let server = &trimmed[..first_slash];
            let after_server = &trimmed[first_slash + 1..];
            if let Some(second_slash) = after_server.find(['/', '\\']) {
                let share = &after_server[..second_slash];
                let rest = &after_server[second_slash..];
                let vol_len = 2 + server.len() + 1 + share.len();
                let vol = &s[..vol_len];
                let is_abs = rest.starts_with('/') || rest.starts_with('\\');
                (vol, is_abs, rest)
            } else {
                (s, true, "")
            }
        } else {
            (s, true, "")
        }
    } else {
        let is_abs = s.starts_with('/') || s.starts_with('\\');
        ("", is_abs, s)
    }
}

fn clean_segments(is_abs: bool, rest: &str) -> Vec<&str> {
    let mut segments = Vec::new();
    for part in rest.split(['/', '\\']) {
        if part.is_empty() || part == "." {
            continue;
        }
        if part == ".." {
            if let Some(last) = segments.last() {
                if *last != ".." {
                    segments.pop();
                    continue;
                }
            }
            if !is_abs {
                segments.push("..");
            }
        } else {
            segments.push(part);
        }
    }
    segments
}

pub fn within_root_str(root: &str, target: &str) -> bool {
    let (r_vol, r_abs, r_rest) = split_volume_and_path(root);
    let (t_vol, t_abs, t_rest) = split_volume_and_path(target);

    // Volumes must match (case-insensitive for Windows drive letters)
    if !r_vol.eq_ignore_ascii_case(t_vol) {
        return false;
    }
    // Absolute vs relative must match
    if r_abs != t_abs {
        return false;
    }

    let r_segs = clean_segments(r_abs, r_rest);
    let t_segs = clean_segments(t_abs, t_rest);

    // Target must contain all segments of root as an exact prefix
    if t_segs.len() < r_segs.len() {
        return false;
    }
    for (r, t) in r_segs.iter().zip(t_segs.iter()) {
        if !same_segment(r, t) {
            return false;
        }
    }
    // Go's Rel yields "../..." for a target above an empty relative root.
    !(r_segs.is_empty() && t_segs.first() == Some(&".."))
}

/// Go's `filepath.Rel` compares path elements with `strings.EqualFold` on
/// Windows and exactly elsewhere.
fn same_segment(a: &str, b: &str) -> bool {
    if cfg!(windows) {
        a == b || a.to_lowercase() == b.to_lowercase()
    } else {
        a == b
    }
}

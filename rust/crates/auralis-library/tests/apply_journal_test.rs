mod common;

use auralis_library::journal::within_root;
use auralis_library::tag_io::{
    apply_tags as io_apply_tags, read_tags, restore_tags as io_restore_tags,
    system_time_to_unix_nano,
};
use auralis_library::{
    apply, build_plan, detect_all, first_tag, last_batch, profile_by_id, read_journal,
    scan_library, undo_last, JournalEntry, Operation, Plan, ScanProgress, PROFILE_POWERAMP,
};
use common::make_audio_file;
use std::fs;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use tempfile::tempdir;

#[test]
fn test_apply_tags_preserves_other_tags_and_journal() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let flac = dir.join("track.flac");
    make_audio_file(&flac, &[]);
    io_apply_tags(
        &flac,
        &[
            ("TITLE".to_string(), "Old Title".to_string()),
            ("ARTIST".to_string(), "The Beatles".to_string()),
            ("ALBUM".to_string(), "Abbey Road".to_string()),
        ]
        .into_iter()
        .collect(),
        &[],
    )
    .unwrap();

    let plan = Plan {
        operations: vec![Operation {
            op_type: "tags".to_string(),
            path: flac.to_string_lossy().to_string(),
            set: Some(
                [("TITLE".to_string(), "New Title".to_string())]
                    .into_iter()
                    .collect(),
            ),
            ..Default::default()
        }],
    };

    let journal = dir.join("journal.jsonl");
    let result = apply(dir, &journal, &plan, None, None::<fn() -> bool>).unwrap();
    assert_eq!(result.applied, 1, "apply: {:?}", result);

    let updated = read_tags(&flac).unwrap();
    assert_eq!(first_tag(&updated, &["TITLE"]), "New Title");
    assert_eq!(first_tag(&updated, &["ARTIST"]), "The Beatles");
    assert_eq!(first_tag(&updated, &["ALBUM"]), "Abbey Road");

    let entries = read_journal(&journal).unwrap();
    assert_eq!(entries.len(), 1);
    assert_eq!(entries[0].op_type, "tags");
    assert_eq!(
        entries[0].set.get("TITLE").map(|s| s.as_str()),
        Some("New Title")
    );
    assert_eq!(
        entries[0]
            .old
            .get("TITLE")
            .and_then(|v| v.as_ref())
            .and_then(|v| v.first().map(|s| s.as_str())),
        Some("Old Title")
    );
}

#[test]
fn test_apply_refuses_changed_file_and_undo_restores() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let flac = dir.join("a.flac");
    make_audio_file(&flac, &[("title", "T")]);

    let meta = fs::metadata(&flac).unwrap();
    let plan = Plan {
        operations: vec![Operation {
            op_type: "tags".to_string(),
            path: flac.to_string_lossy().to_string(),
            set: Some(
                [("ALBUMARTIST".to_string(), "Someone".to_string())]
                    .into_iter()
                    .collect(),
            ),
            size: (meta.len() as i64) + 10,
            mod_time: system_time_to_unix_nano(meta.modified().unwrap()),
            ..Default::default()
        }],
    };

    let journal = dir.join("j.jsonl");
    let result = apply(dir, &journal, &plan, None, None::<fn() -> bool>).unwrap();
    assert_eq!(result.applied, 0);
    assert_eq!(result.skipped, 1);
    assert!(!result.errors.is_empty());
    assert!(result.errors[0].contains("file size changed since scan"));

    // Apply for real, then undo must restore the missing albumartist.
    let mut real_plan = plan;
    real_plan.operations[0].size = meta.len() as i64;
    let applied = apply(dir, &journal, &real_plan, None, None::<fn() -> bool>).unwrap();
    assert_eq!(applied.applied, 1);

    let undo = undo_last(&journal, None).unwrap();
    assert_eq!(undo.applied, 1);

    let tags = read_tags(&flac).unwrap();
    assert_eq!(first_tag(&tags, &["ALBUMARTIST"]), "");

    // Undo of an already-undone batch is a no-op.
    let again = undo_last(&journal, None).unwrap();
    assert_eq!(again.applied, 0);
}

#[test]
fn test_move_through_index_and_undo() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let old_dir = dir.join("Old");
    fs::create_dir_all(&old_dir).unwrap();
    let flac = old_dir.join("a.flac");
    make_audio_file(&flac, &[("title", "T")]);
    let meta = fs::metadata(&flac).unwrap();

    let dest = dir.join("New").join("a.flac");
    let plan = Plan {
        operations: vec![Operation {
            op_type: "move".to_string(),
            path: flac.to_string_lossy().to_string(),
            new_path: Some(dest.to_string_lossy().to_string()),
            size: meta.len() as i64,
            mod_time: system_time_to_unix_nano(meta.modified().unwrap()),
            ..Default::default()
        }],
    };

    let journal = dir.join("j.jsonl");
    let result = apply(dir, &journal, &plan, None, None::<fn() -> bool>).unwrap();
    assert_eq!(result.applied, 1);
    assert!(dest.exists());

    // Never overwrite an existing destination.
    let dest2 = dir.join("Old").join("b.flac");
    make_audio_file(&dest2, &[("title", "B")]);
    let colliding = Plan {
        operations: vec![Operation {
            op_type: "move".to_string(),
            path: dest.to_string_lossy().to_string(),
            new_path: Some(dest2.to_string_lossy().to_string()),
            ..Default::default()
        }],
    };
    let res2 = apply(
        dir,
        dir.join("j2.jsonl"),
        &colliding,
        None,
        None::<fn() -> bool>,
    )
    .unwrap();
    assert_eq!(res2.applied, 0);
    assert_eq!(res2.skipped, 1);
    assert!(res2.errors[0].contains("destination already exists:"));

    undo_last(&journal, None).unwrap();
    assert!(flac.exists(), "undo did not restore old path");
}

#[test]
fn test_journal_round_trip() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let journal = dir.join("j.jsonl");

    let now = chrono::Utc::now().format("%Y-%m-%dT%H:%M:%SZ").to_string();
    let mut old_map = std::collections::BTreeMap::new();
    old_map.insert("A".to_string(), None);

    let entries = vec![
        JournalEntry {
            batch_id: "b1".to_string(),
            time: now.clone(),
            op_type: "tags".to_string(),
            path: "C:\\m\\a.flac".to_string(),
            set: [("A".to_string(), "1".to_string())].into_iter().collect(),
            old: old_map,
            ..Default::default()
        },
        JournalEntry {
            batch_id: "b1".to_string(),
            time: now.clone(),
            op_type: "move".to_string(),
            path: "C:\\m\\a.flac".to_string(),
            new_path: "C:\\m\\b\\a.flac".to_string(),
            ..Default::default()
        },
        JournalEntry {
            batch_id: "b2".to_string(),
            time: now.clone(),
            op_type: "tags".to_string(),
            path: "C:\\m\\c.flac".to_string(),
            set: [("B".to_string(), "2".to_string())].into_iter().collect(),
            ..Default::default()
        },
    ];

    for entry in &entries {
        auralis_library::append_journal(&journal, entry).unwrap();
    }

    let read = read_journal(&journal).unwrap();
    assert_eq!(read.len(), 3);

    let last = last_batch(&read);
    assert_eq!(last.len(), 1);
    assert_eq!(last[0].batch_id, "b2");

    // Mark b2 undone, then b1 is the tail.
    auralis_library::append_journal(
        &journal,
        &JournalEntry {
            batch_id: "b2".to_string(),
            time: now,
            op_type: "undo".to_string(),
            ..Default::default()
        },
    )
    .unwrap();

    let read2 = read_journal(&journal).unwrap();
    let last2 = last_batch(&read2);
    assert_eq!(last2.len(), 2);
    assert_eq!(last2[0].batch_id, "b1");
}

#[test]
fn test_undo_last_end_to_end_with_journal() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let flac = dir.join("a.flac");
    make_audio_file(&flac, &[("title", "T"), ("albumartist", "Old")]);
    let meta = fs::metadata(&flac).unwrap();

    let journal = dir.join("j.jsonl");
    let plan = Plan {
        operations: vec![Operation {
            op_type: "tags".to_string(),
            path: flac.to_string_lossy().to_string(),
            set: Some(
                [("ALBUMARTIST".to_string(), "New".to_string())]
                    .into_iter()
                    .collect(),
            ),
            size: meta.len() as i64,
            mod_time: system_time_to_unix_nano(meta.modified().unwrap()),
            ..Default::default()
        }],
    };

    apply(dir, &journal, &plan, None, None::<fn() -> bool>).unwrap();
    undo_last(&journal, None).unwrap();

    let tags = read_tags(&flac).unwrap();
    assert_eq!(first_tag(&tags, &["ALBUMARTIST"]), "Old");

    let entries = read_journal(&journal).unwrap();
    let saw_undo = entries.iter().any(|e| e.op_type == "undo");
    assert!(saw_undo, "expected undo marker in journal");
}

#[test]
fn test_apply_rejects_paths_outside_root() {
    let root = tempdir().unwrap();
    let outside = tempdir().unwrap();
    let flac = outside.path().join("a.flac");
    make_audio_file(&flac, &[("title", "T")]);
    let inside = root.path().join("x.flac");
    make_audio_file(&inside, &[("title", "X")]);

    let plan = Plan {
        operations: vec![
            Operation {
                op_type: "tags".to_string(),
                path: flac.to_string_lossy().to_string(),
                set: Some(
                    [("ALBUMARTIST".to_string(), "Z".to_string())]
                        .into_iter()
                        .collect(),
                ),
                ..Default::default()
            },
            Operation {
                op_type: "move".to_string(),
                path: inside.to_string_lossy().to_string(),
                new_path: Some(
                    root.path()
                        .join("..")
                        .join("escaped.flac")
                        .to_string_lossy()
                        .to_string(),
                ),
                ..Default::default()
            },
        ],
    };

    let journal = root.path().join("j.jsonl");
    let result = apply(root.path(), &journal, &plan, None, None::<fn() -> bool>).unwrap();
    assert_eq!(result.applied, 0);
    assert_eq!(result.skipped, 2);
    assert!(inside.exists(), "file inside root was moved");

    let err = apply("", &journal, &plan, None, None::<fn() -> bool>);
    assert!(err.is_err());
    assert_eq!(err.unwrap_err().1, "library root is required");
}

#[test]
fn test_move_carries_lyrics_sidecar() {
    let root = tempdir().unwrap();
    let old_dir = root.path().join("Old");
    fs::create_dir_all(&old_dir).unwrap();
    let flac = old_dir.join("a.flac");
    make_audio_file(&flac, &[("title", "T")]);
    let lrc = old_dir.join("a.lrc");
    fs::write(&lrc, "[00:01.00]hi").unwrap();
    let meta = fs::metadata(&flac).unwrap();

    let dest = root.path().join("New").join("b.flac");
    let dest_lrc = root.path().join("New").join("b.lrc");
    let journal = root.path().join("j.jsonl");

    let plan = Plan {
        operations: vec![Operation {
            op_type: "move".to_string(),
            path: flac.to_string_lossy().to_string(),
            new_path: Some(dest.to_string_lossy().to_string()),
            size: meta.len() as i64,
            mod_time: system_time_to_unix_nano(meta.modified().unwrap()),
            ..Default::default()
        }],
    };

    apply(root.path(), &journal, &plan, None, None::<fn() -> bool>).unwrap();
    assert!(dest_lrc.exists(), "lyrics sidecar did not follow the track");
    assert!(!lrc.exists());

    undo_last(&journal, None).unwrap();
    assert!(lrc.exists(), "undo did not bring the sidecar back");
    assert!(!dest_lrc.exists());
}

#[test]
fn test_undo_restores_multi_value_tag() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let flac = dir.join("a.flac");
    make_audio_file(&flac, &[("title", "T")]);

    // Seed multiple values for ALBUMARTIST
    let mut initial_tags = std::collections::BTreeMap::new();
    initial_tags.insert(
        "ALBUMARTIST".to_string(),
        Some(vec!["A".to_string(), "B".to_string()]),
    );
    io_restore_tags(&flac, &initial_tags).unwrap();

    let meta = fs::metadata(&flac).unwrap();
    let journal = dir.join("j.jsonl");
    let plan = Plan {
        operations: vec![Operation {
            op_type: "tags".to_string(),
            path: flac.to_string_lossy().to_string(),
            set: Some(
                [("ALBUMARTIST".to_string(), "C".to_string())]
                    .into_iter()
                    .collect(),
            ),
            size: meta.len() as i64,
            mod_time: system_time_to_unix_nano(meta.modified().unwrap()),
            ..Default::default()
        }],
    };

    apply(dir, &journal, &plan, None, None::<fn() -> bool>).unwrap();
    undo_last(&journal, None).unwrap();

    let restored = read_tags(&flac).unwrap();
    let aa = restored.get("ALBUMARTIST").unwrap();
    assert_eq!(aa.len(), 2);
    assert_eq!(aa[0], "A");
    assert_eq!(aa[1], "B");
}

#[test]
fn test_build_plan_carries_old_tag_values() {
    let root = tempdir().unwrap();
    let album = root.path().join("Artist").join("Album");
    fs::create_dir_all(&album).unwrap();
    let flac = album.join("01. Song.flac");
    make_audio_file(
        &flac,
        &[("title", "Song"), ("artist", "Artist"), ("album", "Album")],
    );

    let scan = scan_library(root.path(), None::<fn(ScanProgress)>, None::<fn() -> bool>).unwrap();
    let profile = profile_by_id(PROFILE_POWERAMP);
    let issues = detect_all(&scan, &profile);
    let plan = build_plan(&scan, &profile, &issues);

    let mut saw_album_artist = false;
    for op in &plan.operations {
        if op.op_type != "tags" {
            continue;
        }
        if op.set.as_ref().and_then(|s| s.get("ALBUMARTIST")).is_some() {
            saw_album_artist = true;
            assert!(
                op.old.is_some(),
                "operation is missing the old value for the preview"
            );
            assert!(op.old.as_ref().unwrap().contains_key("ALBUMARTIST"));
        }
    }
    assert!(
        saw_album_artist,
        "expected an ALBUMARTIST fix in operations"
    );
}

#[test]
fn test_plan_retags_and_moves_one_file_in_one_batch() {
    let root = tempdir().unwrap();
    let album = root.path().join("Artist").join("Album");
    fs::create_dir_all(&album).unwrap();
    let src = album.join("03 Song.flac");
    make_audio_file(
        &src,
        &[("title", "Song"), ("artist", "Artist"), ("album", "Album")],
    );

    let scan = scan_library(root.path(), None::<fn(ScanProgress)>, None::<fn() -> bool>).unwrap();
    let profile = profile_by_id(PROFILE_POWERAMP);
    let issues = detect_all(&scan, &profile);
    let plan = build_plan(&scan, &profile, &issues);

    let mut tag_ops = 0;
    for op in &plan.operations {
        if op.op_type == "tags" && op.path == src.to_string_lossy() {
            tag_ops += 1;
            let set = op.set.as_ref().unwrap();
            assert_eq!(set.get("ALBUMARTIST").map(|s| s.as_str()), Some("Artist"));
            assert!(set.get("TRACKNUMBER").is_some());
        }
    }
    assert_eq!(tag_ops, 1, "want one tag operation for the file");

    let journal = root.path().join("journal.jsonl");
    let result = apply(root.path(), &journal, &plan, None, None::<fn() -> bool>).unwrap();
    assert!(
        result.errors.is_empty(),
        "apply errors: {:?}",
        result.errors
    );
    assert!(result.applied >= 1);
}

#[test]
fn test_cancellation_during_apply() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let f1 = dir.join("1.flac");
    let f2 = dir.join("2.flac");
    make_audio_file(&f1, &[]);
    make_audio_file(&f2, &[]);

    let plan = Plan {
        operations: vec![
            Operation {
                op_type: "tags".to_string(),
                path: f1.to_string_lossy().to_string(),
                set: Some(
                    [("TITLE".to_string(), "T1".to_string())]
                        .into_iter()
                        .collect(),
                ),
                ..Default::default()
            },
            Operation {
                op_type: "tags".to_string(),
                path: f2.to_string_lossy().to_string(),
                set: Some(
                    [("TITLE".to_string(), "T2".to_string())]
                        .into_iter()
                        .collect(),
                ),
                ..Default::default()
            },
        ],
    };

    let journal = dir.join("j.jsonl");
    let counter = Arc::new(AtomicUsize::new(0));
    let c = counter.clone();
    let cancel_check = move || {
        let prev = c.fetch_add(1, Ordering::SeqCst);
        prev >= 1 // cancel on the second operation
    };

    let (res, err) = apply(dir, &journal, &plan, None, Some(cancel_check)).unwrap_err();
    assert_eq!(res.applied, 1);
    assert_eq!(err, "context canceled");
}

#[test]
fn test_within_root_lexical_semantics() {
    // Sibling directory with shared prefix must NOT be within root
    assert!(!within_root("/home/music", "/home/music2"));
    assert!(!within_root("/home/music", "/home/music2/song.flac"));
    assert!(!within_root("C:\\music", "C:\\music2\\song.flac"));

    // Traversal via ".." escaping the root must NOT be within root
    assert!(!within_root(
        "/home/music",
        "/home/music/../other/song.flac"
    ));
    assert!(!within_root("C:\\music", "C:\\music\\..\\other\\song.flac"));

    // Subdirectories and root itself MUST be within root
    assert!(within_root("/home/music", "/home/music"));
    assert!(within_root("/home/music", "/home/music/album/song.flac"));
    assert!(within_root("C:\\music", "C:\\music"));
    assert!(within_root("C:\\music", "C:\\music\\album\\song.flac"));
    assert!(within_root("C:/music", "C:\\music\\album\\song.flac"));

    // Go's filepath.Rel is case-insensitive on Windows only.
    assert_eq!(
        within_root("C:\\Music", "c:\\music\\Album\\song.flac"),
        cfg!(windows)
    );
    // A relative root that cleans to nothing never contains "../x".
    assert!(!within_root("a/..", "../x"));
}

#[test]
fn test_journal_go_compat_old_null_and_html_entities() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let song_path = dir.join("Rock & Roll").join("song.flac");
    fs::create_dir_all(song_path.parent().unwrap()).unwrap();
    make_audio_file(&song_path, &[("title", "After"), ("genre", "Rock")]);

    // Go-format serialized JSONL entry with null in old and & in path
    let original_line = format!(
        r#"{{"batch_id":"b1","time":"2026-10-11T12:00:00Z","type":"tags","path":"{}","set":{{"TITLE":"After"}},"old":{{"GENRE":null,"TITLE":["Before"]}}}}"#,
        song_path
            .to_string_lossy()
            .replace('\\', "\\\\")
            .replace('&', "\\u0026")
    );

    let journal = dir.join("journal.jsonl");
    fs::write(&journal, format!("{original_line}\n")).unwrap();

    let entries = read_journal(&journal).unwrap();
    assert_eq!(entries.len(), 1);
    assert_eq!(entries[0].old.get("GENRE"), Some(&None));
    assert_eq!(
        entries[0].old.get("TITLE"),
        Some(&Some(vec!["Before".to_string()]))
    );

    // Reversal of this entry should restore TITLE to "Before" and DELETE GENRE (since it was null)
    undo_last(&journal, None).unwrap();

    let tags = read_tags(&song_path).unwrap();
    assert_eq!(first_tag(&tags, &["TITLE"]), "Before");
    assert_eq!(first_tag(&tags, &["GENRE"]), "");

    // Verify that re-serializing the read entry matches original_line byte-for-byte
    let reserialized =
        String::from_utf8(auralis_core::gojson::to_vec(&entries[0]).unwrap()).unwrap();
    assert_eq!(reserialized, original_line);
}

use auralis_library::rules::helpers::looks_multi_disc;
use auralis_library::{
    build_plan, detect_all, profile_by_id, rule_by_id, Issue, Scan, Track, PROFILE_POWERAMP,
};
use std::collections::{HashMap, HashSet};
use std::fs;
use std::path::Path;
use tempfile::tempdir;

fn scan_fixture() -> Scan {
    Scan {
        root: r"C:\music".to_string(),
        tracks: vec![
            Track {
                path: r"C:\music\Artist\Album\01. Song.flac".to_string(),
                rel_path: "Artist/Album/01. Song.flac".to_string(),
                format: "flac".to_string(),
                title: "Song".to_string(),
                artist: "Artist".to_string(),
                album_artist: "Artist".to_string(),
                album: "Album".to_string(),
                track_number: 1,
                track_total: 2,
                duration_seconds: 100.0,
                ..Default::default()
            },
            Track {
                path: r"C:\music\Artist\Album\02. Song2.flac".to_string(),
                rel_path: "Artist/Album/02. Song2.flac".to_string(),
                format: "flac".to_string(),
                title: "Song2".to_string(),
                artist: "Artist".to_string(),
                album_artist: "Someone Else".to_string(),
                album: "Album".to_string(),
                track_number: 2,
                track_total: 2,
                duration_seconds: 100.0,
                ..Default::default()
            },
        ],
        dirs: vec!["Artist".to_string(), "Artist/Album".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    }
}

#[test]
fn test_detect_album_artist_missing_and_inconsistent() {
    let scan = scan_fixture();
    let rule = rule_by_id("ALBUMARTIST").unwrap();
    let profile = profile_by_id(PROFILE_POWERAMP);
    let issues = (rule.detect)(&scan, &profile);

    let found = issues.iter().any(|i| i.message.contains("inconsistent"));
    assert!(found, "expected inconsistency issue, got {:?}", issues);

    let scan2 = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![Track {
            path: r"C:\m\A\L\1.flac".to_string(),
            rel_path: "A/L/1.flac".to_string(),
            artist: "X".to_string(),
            album: "L".to_string(),
            ..Default::default()
        }],
        dirs: vec!["A".to_string(), "A/L".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let issues2 = (rule.detect)(&scan2, &profile);
    assert_eq!(issues2.len(), 1);
    assert!(
        issues2[0].message.contains("no ALBUMARTIST"),
        "expected missing albumartist issue, got {:?}",
        issues2
    );
}

#[test]
fn test_album_artist_fix_sets_one_value() {
    let scan = scan_fixture();
    let rule = rule_by_id("ALBUMARTIST").unwrap();
    let profile = profile_by_id(PROFILE_POWERAMP);
    let issues = (rule.detect)(&scan, &profile);
    let ops = (rule.fix.unwrap())(&scan, &profile, &issues);
    assert!(!ops.is_empty(), "expected a fix operation");
    for op in ops {
        let set = op.set.as_ref().unwrap();
        assert_eq!(
            set.get("ALBUMARTIST").map(|s| s.as_str()),
            Some("Artist"),
            "expected ALBUMARTIST=Artist, got {:?}",
            set
        );
    }
}

#[test]
fn test_compilation_detect_and_fix() {
    let mut scan = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![
            Track {
                path: r"C:\m\Various\Compilation\1.flac".to_string(),
                rel_path: "Various/Compilation/1.flac".to_string(),
                album: "Compilation".to_string(),
                artist: "A".to_string(),
                ..Default::default()
            },
            Track {
                path: r"C:\m\Various\Compilation\2.flac".to_string(),
                rel_path: "Various/Compilation/2.flac".to_string(),
                album: "Compilation".to_string(),
                artist: "B".to_string(),
                ..Default::default()
            },
            Track {
                path: r"C:\m\Various\Compilation\3.flac".to_string(),
                rel_path: "Various/Compilation/3.flac".to_string(),
                album: "Compilation".to_string(),
                artist: "C".to_string(),
                ..Default::default()
            },
        ],
        dirs: vec!["Various".to_string(), "Various/Compilation".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let rule = rule_by_id("COMPILATION").unwrap();
    let profile = profile_by_id(PROFILE_POWERAMP);
    let issues = (rule.detect)(&scan, &profile);
    assert_eq!(issues.len(), 1, "expected 1 issue, got {:?}", issues);

    let ops = (rule.fix.unwrap())(&scan, &profile, &issues);
    assert_eq!(ops.len(), 3, "expected 3 ops, got {}", ops.len());
    for op in ops {
        let set = op.set.as_ref().unwrap();
        assert_eq!(
            set.get("ALBUMARTIST").map(|s| s.as_str()),
            Some("Various Artists")
        );
        assert_eq!(set.get("COMPILATION").map(|s| s.as_str()), Some("1"));
    }

    for t in &mut scan.tracks {
        t.compilation = true;
    }
    let issues_after = (rule.detect)(&scan, &profile);
    assert_eq!(
        issues_after.len(),
        0,
        "expected no issues, got {:?}",
        issues_after
    );
}

#[test]
fn test_track_number_gap_is_reported_not_renumbered() {
    let scan = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![
            Track {
                path: r"C:\m\A\L\01.flac".to_string(),
                rel_path: "A/L/01.flac".to_string(),
                album: "L".to_string(),
                track_number: 1,
                track_total: 12,
                ..Default::default()
            },
            Track {
                path: r"C:\m\A\L\03.flac".to_string(),
                rel_path: "A/L/03.flac".to_string(),
                album: "L".to_string(),
                track_number: 3,
                track_total: 12,
                ..Default::default()
            },
            Track {
                path: r"C:\m\A\L\04.flac".to_string(),
                rel_path: "A/L/04.flac".to_string(),
                album: "L".to_string(),
                track_number: 4,
                track_total: 12,
                ..Default::default()
            },
        ],
        dirs: vec!["A".to_string(), "A/L".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let issues = detect_all(&scan, &profile_by_id(PROFILE_POWERAMP));
    let mut gap = false;
    for issue in issues {
        if issue.rule_id != "TRACKNUMBER" {
            continue;
        }
        assert!(
            !issue.fixable,
            "incomplete album must not be fixable: {:?}",
            issue
        );
        if issue.message.contains("may be missing") {
            gap = true;
        }
    }
    assert!(gap, "expected a missing-tracks report");
}

#[test]
fn test_track_number_fixes() {
    let scan = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![
            Track {
                path: r"C:\m\A\L\01.flac".to_string(),
                rel_path: "A/L/01.flac".to_string(),
                album: "L".to_string(),
                format: "flac".to_string(),
                track_number: 1,
                track_total: 2,
                track_number_raw: "1".to_string(),
                ..Default::default()
            },
            Track {
                path: r"C:\m\A\L\02.flac".to_string(),
                rel_path: "A/L/02.flac".to_string(),
                album: "L".to_string(),
                format: "flac".to_string(),
                track_number: 2,
                track_total: 2,
                track_number_raw: "2".to_string(),
                ..Default::default()
            },
            Track {
                path: r"C:\m\A\L\03.flac".to_string(),
                rel_path: "A/L/03.flac".to_string(),
                album: "L".to_string(),
                format: "flac".to_string(),
                track_number: 3,
                track_total: 3,
                track_number_raw: "3/3".to_string(),
                ..Default::default()
            },
            Track {
                path: r"C:\m\A\L\04 - Four.flac".to_string(),
                rel_path: "A/L/04 - Four.flac".to_string(),
                album: "L".to_string(),
                format: "flac".to_string(),
                ..Default::default()
            },
        ],
        dirs: vec!["A".to_string(), "A/L".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let profile = profile_by_id(PROFILE_POWERAMP);
    let rule = rule_by_id("TRACKNUMBER").unwrap();
    let fixable: Vec<Issue> = (rule.detect)(&scan, &profile)
        .into_iter()
        .filter(|i| i.fixable)
        .collect();

    let ops = (rule.fix.unwrap())(&scan, &profile, &fixable);
    let mut got: HashMap<String, std::collections::BTreeMap<String, String>> = HashMap::new();
    for op in ops {
        let name = Path::new(&op.path)
            .file_name()
            .unwrap()
            .to_string_lossy()
            .to_string();
        got.insert(name, op.set.unwrap());
    }

    let mut want: HashMap<String, std::collections::BTreeMap<String, String>> = HashMap::new();
    let mut m1 = std::collections::BTreeMap::new();
    m1.insert("TRACKNUMBER".to_string(), "1".to_string());
    m1.insert("TRACKTOTAL".to_string(), "4".to_string());
    m1.insert("TOTALTRACKS".to_string(), "4".to_string());
    want.insert("01.flac".to_string(), m1);

    let mut m2 = std::collections::BTreeMap::new();
    m2.insert("TRACKNUMBER".to_string(), "2".to_string());
    m2.insert("TRACKTOTAL".to_string(), "4".to_string());
    m2.insert("TOTALTRACKS".to_string(), "4".to_string());
    want.insert("02.flac".to_string(), m2);

    let mut m3 = std::collections::BTreeMap::new();
    m3.insert("TRACKNUMBER".to_string(), "3".to_string());
    m3.insert("TRACKTOTAL".to_string(), "4".to_string());
    m3.insert("TOTALTRACKS".to_string(), "4".to_string());
    want.insert("03.flac".to_string(), m3);

    let mut m4 = std::collections::BTreeMap::new();
    m4.insert("TRACKNUMBER".to_string(), "4".to_string());
    m4.insert("TRACKTOTAL".to_string(), "4".to_string());
    m4.insert("TOTALTRACKS".to_string(), "4".to_string());
    want.insert("04 - Four.flac".to_string(), m4);

    assert_eq!(got.len(), want.len(), "ops = {:?}", got);
    for (name, set) in &want {
        for (k, v) in set {
            assert_eq!(got[name].get(k), Some(v), "{name} {k} mismatch");
        }
    }
}

#[test]
fn test_album_wide_rules_skip_loose_files() {
    let scan = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![
            Track {
                path: r"C:\m\a.flac".to_string(),
                rel_path: "a.flac".to_string(),
                artist: "A".to_string(),
                album: "X".to_string(),
                ..Default::default()
            },
            Track {
                path: r"C:\m\b.flac".to_string(),
                rel_path: "b.flac".to_string(),
                artist: "B".to_string(),
                album: "Y".to_string(),
                ..Default::default()
            },
            Track {
                path: r"C:\m\c.flac".to_string(),
                rel_path: "c.flac".to_string(),
                artist: "C".to_string(),
                album: "Z".to_string(),
                ..Default::default()
            },
            Track {
                path: r"C:\m\Mix\1.flac".to_string(),
                rel_path: "Mix/1.flac".to_string(),
                artist: "A".to_string(),
                album: "One".to_string(),
                ..Default::default()
            },
            Track {
                path: r"C:\m\Mix\2.flac".to_string(),
                rel_path: "Mix/2.flac".to_string(),
                artist: "B".to_string(),
                album: "Two".to_string(),
                ..Default::default()
            },
            Track {
                path: r"C:\m\Mix\3.flac".to_string(),
                rel_path: "Mix/3.flac".to_string(),
                artist: "C".to_string(),
                album: "Three".to_string(),
                ..Default::default()
            },
        ],
        dirs: vec!["Mix".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    for issue in detect_all(&scan, &profile_by_id(PROFILE_POWERAMP)) {
        match issue.rule_id.as_str() {
            "COMPILATION" | "ALBUMARTIST" | "TRACKNUMBER" | "DISCNUMBER" => {
                panic!("album rule fired on loose files: {:?}", issue);
            }
            _ => {}
        }
    }
}

#[test]
fn test_looks_multi_disc() {
    let cases = [
        ("A/Album/CD1", true),
        ("A/Album/Disc 2", true),
        ("ACDC/Back in Black", false),
        ("A/Discovery", false),
    ];
    for (dir, want) in cases {
        assert_eq!(looks_multi_disc(dir), want, "looks_multi_disc({dir:?})");
    }
}

#[test]
fn test_issue_ids_are_unique() {
    let scan = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![Track {
            path: r"C:\m\A\L\1.flac".to_string(),
            rel_path: "A/L/1.flac".to_string(),
            album: "L".to_string(),
            format: "flac".to_string(),
            cover_bytes: 2 * 1024 * 1024,
            cover_width: 3000,
            cover_height: 3000,
            ..Default::default()
        }],
        dirs: vec!["A".to_string(), "A/L".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let mut seen = HashSet::new();
    for issue in detect_all(&scan, &profile_by_id(PROFILE_POWERAMP)) {
        assert!(
            seen.insert(issue.id.clone()),
            "duplicate issue ID {:?}",
            issue.id
        );
    }
}

#[test]
fn test_track_number_unparseable() {
    let scan = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![Track {
            path: r"C:\m\A\L\1.flac".to_string(),
            rel_path: "A/L/1.flac".to_string(),
            album: "L".to_string(),
            track_number_raw: "not-a-number".to_string(),
            ..Default::default()
        }],
        dirs: vec!["A".to_string(), "A/L".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let rule = rule_by_id("TRACKNUMBER").unwrap();
    let issues = (rule.detect)(&scan, &profile_by_id(PROFILE_POWERAMP));
    assert!(
        !issues.is_empty() && issues[0].message.contains("unparseable"),
        "expected unparseable issue, got {:?}",
        issues
    );
}

#[test]
fn test_disc_number_inconsistent() {
    let scan = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![
            Track {
                path: r"C:\m\A\L\1.flac".to_string(),
                rel_path: "A/L/1.flac".to_string(),
                disc_number: 1,
                album: "L".to_string(),
                ..Default::default()
            },
            Track {
                path: r"C:\m\A\L\2.flac".to_string(),
                rel_path: "A/L/2.flac".to_string(),
                disc_number: 0,
                album: "L".to_string(),
                ..Default::default()
            },
        ],
        dirs: vec!["A".to_string(), "A/L".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let rule = rule_by_id("DISCNUMBER").unwrap();
    let issues = (rule.detect)(&scan, &profile_by_id(PROFILE_POWERAMP));
    assert_eq!(issues.len(), 1, "expected 1 issue, got {:?}", issues);
}

#[test]
fn test_cover_rules() {
    let scan = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![
            Track {
                path: r"C:\m\A\L\1.flac".to_string(),
                rel_path: "A/L/1.flac".to_string(),
                format: "flac".to_string(),
                cover_bytes: 0,
                ..Default::default()
            },
            Track {
                path: r"C:\m\A\L\2.flac".to_string(),
                rel_path: "A/L/2.flac".to_string(),
                format: "flac".to_string(),
                cover_bytes: 2048 * 2048,
                cover_width: 3000,
                cover_height: 3000,
                ..Default::default()
            },
        ],
        dirs: vec!["A".to_string(), "A/L".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let rule = rule_by_id("COVER").unwrap();
    let issues = (rule.detect)(&scan, &profile_by_id(PROFILE_POWERAMP));
    assert!(
        issues.len() >= 3,
        "expected missing+oversized issues, got {:?}",
        issues
    );
}

#[test]
fn test_path_rule_illegal_and_template() {
    let profile = profile_by_id(PROFILE_POWERAMP); // FATSafe
    let scan = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![Track {
            path: r"C:\m\A&B\L\01. X?.flac".to_string(),
            rel_path: "A&B/L/01. X?.flac".to_string(),
            format: "flac".to_string(),
            title: "X?".to_string(),
            artist: "A&B".to_string(),
            album_artist: "A&B".to_string(),
            album: "L".to_string(),
            track_number: 1,
            ..Default::default()
        }],
        dirs: vec!["A&B".to_string(), "A&B/L".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let rule = rule_by_id("PATH").unwrap();
    let issues = (rule.detect)(&scan, &profile);
    assert_eq!(issues.len(), 1, "expected 1 issue, got {:?}", issues);
    let ops = (rule.fix.unwrap())(&scan, &profile, &issues);
    assert_eq!(ops.len(), 1);
    assert_eq!(ops[0].op_type, "move");
    let new_path = ops[0].new_path.as_ref().unwrap();
    assert!(
        !new_path.contains('?'),
        "destination still illegal: {new_path}"
    );
}

#[test]
fn test_mixed_format_rule() {
    let scan = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![
            Track {
                rel_path: "A/L/1.flac".to_string(),
                format: "flac".to_string(),
                bit_depth: 16,
                album: "L".to_string(),
                ..Default::default()
            },
            Track {
                rel_path: "A/L/2.mp3".to_string(),
                format: "mp3".to_string(),
                album: "L".to_string(),
                ..Default::default()
            },
        ],
        dirs: vec!["A".to_string(), "A/L".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let rule = rule_by_id("MIXEDFORMAT").unwrap();
    let issues = (rule.detect)(&scan, &profile_by_id(PROFILE_POWERAMP));
    assert!(
        issues.len() == 1 && issues[0].message.contains("mixes formats"),
        "expected mixed format, got {:?}",
        issues
    );
}

#[test]
fn test_duplicates_by_isrc_and_duration() {
    let scan = Scan {
        root: r"C:\m".to_string(),
        tracks: vec![
            Track {
                path: r"C:\m\A\L\1.flac".to_string(),
                rel_path: "A/L/1.flac".to_string(),
                isrc: "USRC12345678".to_string(),
                artist: "A".to_string(),
                title: "T".to_string(),
                duration_seconds: 100.0,
                ..Default::default()
            },
            Track {
                path: r"C:\m\A\L\dup.flac".to_string(),
                rel_path: "A/L/dup.flac".to_string(),
                isrc: "usrc12345678".to_string(),
                artist: "A".to_string(),
                title: "T".to_string(),
                duration_seconds: 100.0,
                ..Default::default()
            },
            Track {
                path: r"C:\m\B\M\1.flac".to_string(),
                rel_path: "B/M/1.flac".to_string(),
                artist: "a".to_string(),
                title: "t".to_string(),
                duration_seconds: 101.5,
                ..Default::default()
            },
        ],
        dirs: vec![
            "A".to_string(),
            "A/L".to_string(),
            "B".to_string(),
            "B/M".to_string(),
        ],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let rule = rule_by_id("DUPLICATES").unwrap();
    let issues = (rule.detect)(&scan, &profile_by_id(PROFILE_POWERAMP));
    assert!(
        issues.len() >= 2,
        "expected duplicate issues, got {:?}",
        issues
    );
}

#[test]
fn test_orphan_empty_folder() {
    let td = tempdir().unwrap();
    let root = td.path();
    let album = root.join("A").join("L");
    fs::create_dir_all(&album).unwrap();
    let empty = root.join("Empty");
    fs::create_dir_all(&empty).unwrap();

    let scan = Scan {
        root: root.to_string_lossy().to_string(),
        tracks: vec![Track {
            rel_path: "A/L/1.flac".to_string(),
            path: album.join("1.flac").to_string_lossy().to_string(),
            ..Default::default()
        }],
        dirs: vec!["A".to_string(), "A/L".to_string(), "Empty".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let rule = rule_by_id("ORPHAN").unwrap();
    let issues = (rule.detect)(&scan, &profile_by_id(PROFILE_POWERAMP));
    let found = issues.iter().any(|i| i.message.contains("empty folder"));
    assert!(found, "expected empty folder issue, got {:?}", issues);
}

#[test]
fn test_build_plan_keeps_tag_ops_from_different_rules() {
    let scan = Scan {
        root: r"C:\music".to_string(),
        tracks: vec![Track {
            path: r"C:\music\A\L\01. x.flac".to_string(),
            rel_path: "A/L/01. x.flac".to_string(),
            format: "flac".to_string(),
            artist: "A".to_string(),
            album: "L".to_string(),
            ..Default::default()
        }],
        dirs: vec!["A".to_string(), "A/L".to_string()],
        stray_files: Vec::new(),
        scanned_at: chrono::Utc::now(),
    };
    let profile = profile_by_id(PROFILE_POWERAMP);
    for i in 0..20 {
        let issues = detect_all(&scan, &profile);
        let plan = build_plan(&scan, &profile, &issues);
        let mut saw_album_artist = false;
        let mut saw_track = false;
        for op in &plan.operations {
            if op.op_type != "tags" {
                continue;
            }
            if let Some(ref set) = op.set {
                if set.contains_key("ALBUMARTIST") {
                    saw_album_artist = true;
                }
                if set.contains_key("TRACKNUMBER") {
                    saw_track = true;
                }
            }
        }
        assert!(
            saw_album_artist && saw_track,
            "plan {i} lost an operation: {:?}",
            plan.operations
        );
    }
}

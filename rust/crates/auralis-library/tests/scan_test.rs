mod common;

use auralis_library::{scan_library, ScanProgress};
use common::make_audio_file;
use std::fs;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use tempfile::tempdir;

#[test]
fn test_scan_library_reads_tags_and_folders() {
    let td = tempdir().unwrap();
    let root = td.path();
    let album = root.join("Artist").join("Album");
    fs::create_dir_all(&album).unwrap();

    let flac = album.join("01. Song.flac");
    make_audio_file(
        &flac,
        &[
            ("title", "Song"),
            ("artist", "Artist"),
            ("album", "Album"),
            ("track", "1/2"),
        ],
    );

    let cover = album.join("cover.jpg");
    fs::write(&cover, [0xFF, 0xD8, 0xFF, 0xD9]).unwrap();

    let stray = root.join("stray.jpg");
    fs::write(&stray, [1]).unwrap();

    let mut progress: Vec<ScanProgress> = Vec::new();
    let scan = scan_library(
        root,
        Some(|p: ScanProgress| {
            progress.push(p);
        }),
        None::<fn() -> bool>,
    )
    .unwrap();

    assert_eq!(scan.tracks.len(), 1, "tracks: {:?}", scan.tracks);
    let track = &scan.tracks[0];
    assert_eq!(track.title, "Song");
    assert_eq!(track.artist, "Artist");
    assert_eq!(track.album, "Album");
    assert_eq!(track.track_number, 1);
    assert_eq!(track.track_total, 2);
    assert!(track.folder_cover, "expected folder cover detected");
    assert!(track.duration_seconds > 0.0);
    assert_eq!(track.format, "flac");
    assert!(track.bit_depth > 0);
    assert!(!progress.is_empty(), "expected progress events");

    let found_stray = scan.stray_files.iter().any(|s| s == "stray.jpg");
    assert!(found_stray, "stray files: {:?}", scan.stray_files);
}

#[test]
fn test_scan_library_cancellation() {
    let td = tempdir().unwrap();
    let root = td.path();
    let album = root.join("A");
    fs::create_dir_all(&album).unwrap();
    let flac = album.join("a.flac");
    make_audio_file(&flac, &[("title", "T")]);

    let cancelled = Arc::new(AtomicBool::new(true));
    let cancel_fn = {
        let c = cancelled.clone();
        move || c.load(Ordering::SeqCst)
    };

    let res = scan_library(root, None::<fn(ScanProgress)>, Some(cancel_fn));
    assert!(res.is_err(), "expected cancellation error");
}

use auralis_library::{playlist_line, write_playlist, PlaylistMode, PlaylistTrack};
use std::fs;
use std::path::Path;
use tempfile::tempdir;

#[test]
fn test_playlist_relative_mode() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let out = dir.join("Playlists").join("mix.m3u8");
    let track = PlaylistTrack {
        path: dir
            .join("Artist")
            .join("Al")
            .join("01.flac")
            .to_string_lossy()
            .to_string(),
        title: "S".to_string(),
        artist: "A".to_string(),
        duration_seconds: 62.0,
    };

    let res = write_playlist(&out, &[track], PlaylistMode::Relative, "", "").unwrap();
    assert_eq!(res.written, 1);
    assert_eq!(res.skipped.len(), 0);

    let text = fs::read_to_string(&out).unwrap();
    assert!(text.starts_with("#EXTM3U\n"), "header: {text:?}");
    assert!(text.contains("#EXTINF:62,A - S\n"), "extinf: {text:?}");
    assert!(
        text.contains("../Artist/Al/01.flac\n"),
        "relative path: {text:?}"
    );
}

#[test]
fn test_playlist_root_mode_skips_outside_root() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let out = dir.join("mix.m3u8");

    let td_outside = tempdir().unwrap();
    let outside_dir = td_outside.path();

    let inside = PlaylistTrack {
        path: dir.join("A").join("x.flac").to_string_lossy().to_string(),
        title: "X".to_string(),
        ..Default::default()
    };
    let outside = PlaylistTrack {
        path: outside_dir.join("y.flac").to_string_lossy().to_string(),
        title: "Y".to_string(),
        ..Default::default()
    };

    let res = write_playlist(
        &out,
        &[inside, outside],
        PlaylistMode::Root,
        &dir.to_string_lossy(),
        "",
    )
    .unwrap();
    assert_eq!(res.written, 1);
    assert_eq!(res.skipped.len(), 1);
    assert!(!res.skipped[0].reason.is_empty(), "expected skip reason");

    let data = fs::read_to_string(&out).unwrap();
    assert!(!data.contains("y.flac"), "outside track written: {data}");
}

#[test]
fn test_playlist_device_mode_prefixes_and_skips_foreign_volume() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let out = dir.join("mix.m3u8");

    let inside = PlaylistTrack {
        path: dir.join("A").join("x.flac").to_string_lossy().to_string(),
        title: "X".to_string(),
        ..Default::default()
    };
    let foreign = PlaylistTrack {
        path: r"D:\elsewhere\y.flac".to_string(),
        title: "Y".to_string(),
        ..Default::default()
    };

    let res = write_playlist(
        &out,
        &[inside, foreign],
        PlaylistMode::Device,
        &dir.to_string_lossy(),
        "/storage/emulated/0/Music",
    )
    .unwrap();

    assert_eq!(res.written, 1);
    assert_eq!(res.skipped.len(), 1);

    let text = fs::read_to_string(&out).unwrap().replace('\\', "/");
    assert!(
        text.contains("/storage/emulated/0/Music/A/x.flac"),
        "device path: {text:?}"
    );
    assert!(
        !text.contains("D:") && !text.contains("D:/"),
        "absolute windows path leaked: {text:?}"
    );
}

#[test]
fn test_playlist_relative_different_volume_skipped() {
    let (line, reason) = playlist_line(
        Path::new(r"C:\music\Playlists\mix.m3u8"),
        r"D:\songs\x.flac",
        PlaylistMode::Relative,
        "",
        "",
    );
    assert!(line.is_empty());
    assert!(reason.is_some());
}

#[test]
fn test_playlist_local_keeps_other_drive_absolute() {
    let (line, reason) = playlist_line(
        Path::new(r"C:\music\mix.m3u8"),
        r"D:\songs\x.flac",
        PlaylistMode::Local,
        "",
        "",
    );
    assert!(reason.is_none());
    assert_eq!(line, r"D:\songs\x.flac");

    let (line2, _) = playlist_line(
        Path::new(r"C:\music\mix.m3u8"),
        r"C:\music\A\x.flac",
        PlaylistMode::Local,
        "",
        "",
    );
    assert_eq!(line2, "A/x.flac");
}

#[test]
fn test_playlist_utf8_and_forward_slashes() {
    let td = tempdir().unwrap();
    let dir = td.path();
    let out = dir.join("mix.m3u8");
    let track = PlaylistTrack {
        path: dir
            .join("Björk")
            .join("Debut")
            .join("02. ræ».flac")
            .to_string_lossy()
            .to_string(),
        title: "Ræ»".to_string(),
        artist: "Björk".to_string(),
        duration_seconds: 5.0,
    };
    write_playlist(&out, &[track], PlaylistMode::Relative, "", "").unwrap();
    let text = fs::read_to_string(&out).unwrap();
    assert!(!text.contains('\\'), "backslash in playlist: {text:?}");
    assert!(
        text.contains("Björk/Debut/02. ræ».flac"),
        "utf-8 path: {text:?}"
    );
}

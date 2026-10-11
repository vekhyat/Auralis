use auralis_library::{
    primary_artist, profile_by_id, TrackFields, PROFILE_MEDIASERVER, PROFILE_POWERAMP,
};

#[test]
fn test_relative_path_poweramp() {
    let p = profile_by_id(PROFILE_POWERAMP);
    let got = p.relative_path(&TrackFields {
        title: "Paranoid Android".to_string(),
        artist: "Radiohead".to_string(),
        album_artist: "Radiohead".to_string(),
        album: "OK Computer".to_string(),
        track_number: 2,
        disc_number: 1,
        disc_total: 1,
        ..Default::default()
    });
    assert_eq!(got, "Radiohead/OK Computer/02. Paranoid Android");
}

#[test]
fn test_relative_path_multi_disc_and_missing_track() {
    let p = profile_by_id(PROFILE_POWERAMP);
    let got = p.relative_path(&TrackFields {
        title: "Intro".to_string(),
        album: "Live".to_string(),
        artist: "A".to_string(),
        track_number: 3,
        disc_number: 2,
        disc_total: 2,
        ..Default::default()
    });
    assert_eq!(got, "A/Live/2-03. Intro");

    let got2 = p.relative_path(&TrackFields {
        title: "Intro".to_string(),
        album: "Live".to_string(),
        artist: "A".to_string(),
        ..Default::default()
    });
    assert_eq!(got2, "A/Live/Intro");
}

#[test]
fn test_media_server_year_omitted_when_unknown() {
    let p = profile_by_id(PROFILE_MEDIASERVER);
    let got = p.relative_path(&TrackFields {
        title: "T".to_string(),
        artist: "A".to_string(),
        album: "B".to_string(),
        track_number: 1,
        ..Default::default()
    });
    assert_eq!(got, "A/B/01. T");
}

#[test]
fn test_sanitize_component_fat() {
    let p = profile_by_id(PROFILE_POWERAMP);
    let cases = [
        ("What? Why: \"Now\"", "What Why - Now"),
        ("trailing dots...", "trailing dots"),
        ("a/b\\c", "a b c"),
        ("CON", "_CON"),
        ("con.mp3", "_con.mp3"),
        ("tab\there", "tabhere"),
    ];
    for (input, want) in cases {
        let got = p.sanitize_component(input);
        assert_eq!(got, want, "SanitizeComponent({input:?})");
    }
}

#[test]
fn test_sanitize_component_truncates_on_rune_boundary() {
    let p = profile_by_id(PROFILE_POWERAMP);
    let input = "é".repeat(200); // 400 bytes
    let got = p.sanitize_component(&input);
    assert!(got.len() <= 255);
    assert!(got.starts_with('é'));
    assert!(!got.contains('\u{FFFD}'));
}

#[test]
fn test_sanitize_relative_path_cannot_escape() {
    let p = profile_by_id(PROFILE_POWERAMP);
    let got = p.sanitize_relative_path(r"..\..\Windows/./x");
    assert_eq!(got, "Windows/x");
}

#[test]
fn test_primary_artist() {
    let cases = [
        ("Simon & Garfunkel", "Simon & Garfunkel"),
        ("Drake feat. Rihanna", "Drake"),
        ("Daft Punk; Pharrell", "Daft Punk"),
        ("Earth, Wind & Fire", "Earth, Wind & Fire"),
    ];
    for (input, want) in cases {
        let got = primary_artist(input);
        assert_eq!(got, want, "PrimaryArtist({input:?})");
    }
}

#[test]
fn test_unknown_profile_falls_back_to_poweramp() {
    assert_eq!(profile_by_id("nope").id, PROFILE_POWERAMP);
}

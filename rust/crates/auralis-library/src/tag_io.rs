//! Tag read/write/restore and metadata inspection using Lofty.

use crate::types::Track;
use lofty::config::WriteOptions;
use lofty::file::{AudioFile, TaggedFileExt};
use lofty::prelude::*;
use lofty::probe::Probe;
use lofty::tag::{ItemKey, ItemValue, Tag, TagItem, TagType};
use std::collections::{BTreeMap, HashMap};
use std::fs;
use std::path::Path;
use std::time::SystemTime;

/// Reads all tags from an audio file into an uppercase key -> values map.
pub fn read_tags(path: &Path) -> Result<HashMap<String, Vec<String>>, String> {
    let tagged_file = match Probe::open(path).and_then(|p| p.read()) {
        Ok(tf) => tf,
        Err(e) => return Err(format!("cannot probe tags: {e}")),
    };

    let tag = match tagged_file
        .primary_tag()
        .or_else(|| tagged_file.first_tag())
    {
        Some(t) => t,
        None => return Ok(HashMap::new()),
    };

    let mut map: HashMap<String, Vec<String>> = HashMap::new();
    for item in tag.items() {
        let name = item_key_to_name(&item.key(), tag.tag_type());
        match item.value() {
            ItemValue::Text(s) => {
                map.entry(name).or_default().push(s.clone());
            }
            ItemValue::Locator(s) => {
                map.entry(name).or_default().push(s.clone());
            }
            _ => {}
        }
    }
    Ok(map)
}

/// Reads a Track struct for Library Doctor from a file on disk.
pub fn read_track(root: &Path, rel_path: &str, path: &Path) -> Result<Track, String> {
    let meta = fs::metadata(path).map_err(|e| format!("cannot stat {path:?}: {e}"))?;
    let size = meta.len() as i64;
    let mod_time_unix_nano =
        system_time_to_unix_nano(meta.modified().unwrap_or(SystemTime::UNIX_EPOCH));

    let format = path
        .extension()
        .and_then(|ext| ext.to_str())
        .map(|ext| ext.trim_start_matches('.').to_ascii_lowercase())
        .unwrap_or_default();

    let mut track = Track {
        path: path.to_string_lossy().to_string(),
        rel_path: rel_path.replace('\\', "/"),
        format,
        size,
        mod_time_unix_nano,
        ..Default::default()
    };

    // If file is 0 bytes or empty, return basic track
    if size == 0 {
        return Ok(track);
    }

    let tagged_file = match Probe::open(path).and_then(|p| p.read()) {
        Ok(tf) => tf,
        Err(_) => return Ok(track),
    };

    let props = tagged_file.properties();
    track.duration_seconds = props.duration().as_secs_f64();
    track.bit_depth = props.bit_depth().unwrap_or(0) as usize;

    if let Some(tag) = tagged_file
        .primary_tag()
        .or_else(|| tagged_file.first_tag())
    {
        let mut tags: HashMap<String, Vec<String>> = HashMap::new();
        for item in tag.items() {
            let name = item_key_to_name(&item.key(), tag.tag_type());
            match item.value() {
                ItemValue::Text(s) => tags.entry(name).or_default().push(s.clone()),
                ItemValue::Locator(s) => tags.entry(name).or_default().push(s.clone()),
                _ => {}
            }
        }

        track.title = first_tag(&tags, &["TITLE"]);
        track.artist = first_tag(&tags, &["ARTIST"]);
        track.album = first_tag(&tags, &["ALBUM"]);
        track.album_artist = first_tag(&tags, &["ALBUMARTIST", "ALBUM ARTIST"]);
        track.year = first_tag(&tags, &["DATE", "YEAR"]);
        track.isrc = first_tag(&tags, &["ISRC"]);

        let comp = first_tag(&tags, &["COMPILATION"]);
        track.compilation = comp == "1" || comp.eq_ignore_ascii_case("true");

        let raw_track = first_tag(&tags, &["TRACKNUMBER", "TRACK"]);
        track.track_number_raw = raw_track.clone();
        let (num, tot) = split_number_total(&raw_track);
        track.track_number = num;
        track.track_total = tot;

        let total_val = first_tag(&tags, &["TRACKTOTAL", "TOTALTRACKS"]);
        if track.track_total == 0 && !total_val.is_empty() {
            track.track_total = parse_leading_int(&total_val);
        }

        let raw_disc = first_tag(&tags, &["DISCNUMBER", "DISC"]);
        let (disc_num, disc_tot) = split_number_total(&raw_disc);
        track.disc_number = disc_num;
        track.disc_total = disc_tot;

        let disc_total_val = first_tag(&tags, &["DISCTOTAL", "TOTALDISCS"]);
        if track.disc_total == 0 && !disc_total_val.is_empty() {
            track.disc_total = parse_leading_int(&disc_total_val);
        }

        // Embedded cover inspection
        if let Some(pic) = tag.pictures().first() {
            track.cover_bytes = pic.data().len();
            if let Ok(reader) =
                image::ImageReader::new(std::io::Cursor::new(pic.data())).with_guessed_format()
            {
                if let Ok(dim) = reader.into_dimensions() {
                    track.cover_width = dim.0 as usize;
                    track.cover_height = dim.1 as usize;
                }
            }
        }
    }

    // Sidecar check
    let file_stem = path
        .file_stem()
        .and_then(|s| s.to_str())
        .unwrap_or_default();
    let lrc_path = path.with_file_name(format!("{file_stem}.lrc"));
    track.lrc = lrc_path.is_file();

    // Folder cover check
    let parent = path.parent().unwrap_or(root);
    for name in &["cover.jpg", "cover.jpeg", "folder.jpg", "folder.jpeg"] {
        if parent.join(name).is_file() {
            track.folder_cover = true;
            break;
        }
    }

    Ok(track)
}

/// Applies tag updates and deletions to a file, returning the prior values for the modified tags.
pub fn apply_tags(
    path: &Path,
    set: &BTreeMap<String, String>,
    delete: &[String],
) -> Result<BTreeMap<String, Option<Vec<String>>>, String> {
    let current_tags = read_tags(path).unwrap_or_default();
    let mut by_upper: HashMap<String, String> = HashMap::new();
    for key in current_tags.keys() {
        by_upper.insert(key.to_ascii_uppercase(), key.clone());
    }

    let mut old: BTreeMap<String, Option<Vec<String>>> = BTreeMap::new();
    for k in set.keys() {
        if let Some(actual) = by_upper.get(&k.to_ascii_uppercase()) {
            old.insert(k.clone(), current_tags.get(actual).cloned());
        } else {
            old.insert(k.clone(), None);
        }
    }
    for k in delete {
        if let Some(actual) = by_upper.get(&k.to_ascii_uppercase()) {
            old.insert(k.clone(), current_tags.get(actual).cloned());
        } else {
            old.insert(k.clone(), None);
        }
    }

    let mut tagged_file = Probe::open(path)
        .and_then(|p| p.read())
        .map_err(|e| format!("cannot open {path:?}: {e}"))?;

    let tag_type = tagged_file.primary_tag_type();

    let tag = match tagged_file.tag_mut(tag_type) {
        Some(t) => t,
        None => {
            tagged_file.insert_tag(Tag::new(tag_type));
            tagged_file.tag_mut(tag_type).unwrap()
        }
    };

    // Remove deleted tags
    for del in delete {
        let item_key = string_to_item_key(del, tag_type);
        tag.remove_key(item_key);
    }

    // Set updated tags
    for (k, v) in set {
        let item_key = string_to_item_key(k, tag_type);
        tag.insert_text(item_key, v.clone());
    }

    tag.save_to_path(path, WriteOptions::default())
        .map_err(|e| format!("cannot save tags to {path:?}: {e}"))?;

    Ok(old)
}

/// Restores tags on disk to a previous state captured in `old`.
pub fn restore_tags(
    path: &Path,
    old: &BTreeMap<String, Option<Vec<String>>>,
) -> Result<(), String> {
    let mut tagged_file = Probe::open(path)
        .and_then(|p| p.read())
        .map_err(|e| format!("cannot open {path:?}: {e}"))?;

    let tag_type = tagged_file.primary_tag_type();

    let tag = match tagged_file.tag_mut(tag_type) {
        Some(t) => t,
        None => {
            tagged_file.insert_tag(Tag::new(tag_type));
            tagged_file.tag_mut(tag_type).unwrap()
        }
    };

    for (key, values_opt) in old {
        let item_key = string_to_item_key(key, tag_type);
        tag.remove_key(item_key);
        if let Some(values) = values_opt {
            if values.is_empty() {
                continue;
            }
            for (i, val) in values.iter().enumerate() {
                if i == 0 {
                    tag.insert_text(item_key, val.clone());
                } else {
                    tag.push(TagItem::new(item_key, ItemValue::Text(val.clone())));
                }
            }
        }
    }

    tag.save_to_path(path, WriteOptions::default())
        .map_err(|e| format!("cannot save tags to {path:?}: {e}"))?;

    Ok(())
}

/// Returns the first non-empty value for any of the given keys.
pub fn first_tag(tags: &HashMap<String, Vec<String>>, keys: &[&str]) -> String {
    for &k in keys {
        let upper = k.to_ascii_uppercase();
        if let Some(vals) = tags.get(&upper) {
            for v in vals {
                let trimmed = v.trim();
                if !trimmed.is_empty() {
                    return trimmed.to_string();
                }
            }
        }
    }
    String::new()
}

pub fn system_time_to_unix_nano(st: SystemTime) -> i64 {
    match st.duration_since(SystemTime::UNIX_EPOCH) {
        Ok(dur) => dur.as_nanos() as i64,
        Err(_) => 0,
    }
}

pub fn split_number_total(raw: &str) -> (usize, usize) {
    let trimmed = raw.trim();
    if let Some((n_str, tot_str)) = trimmed.split_once('/') {
        (parse_leading_int(n_str), parse_leading_int(tot_str))
    } else {
        (parse_leading_int(trimmed), 0)
    }
}

pub fn parse_leading_int(s: &str) -> usize {
    let digits: String = s.chars().take_while(|c| c.is_ascii_digit()).collect();
    digits.parse().unwrap_or(0)
}

fn item_key_to_name(key: &ItemKey, tag_type: TagType) -> String {
    match key {
        ItemKey::TrackTitle => "TITLE".to_string(),
        ItemKey::TrackArtist => "ARTIST".to_string(),
        ItemKey::AlbumTitle => "ALBUM".to_string(),
        ItemKey::AlbumArtist => "ALBUMARTIST".to_string(),
        ItemKey::TrackNumber => "TRACKNUMBER".to_string(),
        ItemKey::TrackTotal => "TRACKTOTAL".to_string(),
        ItemKey::DiscNumber => "DISCNUMBER".to_string(),
        ItemKey::DiscTotal => "DISCTOTAL".to_string(),
        ItemKey::FlagCompilation => "COMPILATION".to_string(),
        ItemKey::Genre => "GENRE".to_string(),
        ItemKey::ReplayGainTrackGain => "REPLAYGAIN_TRACK_GAIN".to_string(),
        ItemKey::Year => "YEAR".to_string(),
        ItemKey::RecordingDate => "DATE".to_string(),
        ItemKey::Isrc => "ISRC".to_string(),
        ItemKey::Comment => "COMMENT".to_string(),
        ItemKey::Composer => "COMPOSER".to_string(),
        ItemKey::Lyrics => "LYRICS".to_string(),
        ItemKey::Bpm => "BPM".to_string(),
        _ => {
            if let Some(name) = (*key).map_key(TagType::VorbisComments) {
                name.to_ascii_uppercase()
            } else if let Some(name) = (*key).map_key(tag_type) {
                name.to_ascii_uppercase()
            } else {
                format!("{key:?}").to_ascii_uppercase()
            }
        }
    }
}

fn string_to_item_key(s: &str, tag_type: TagType) -> ItemKey {
    match s.to_ascii_uppercase().as_str() {
        "TITLE" => ItemKey::TrackTitle,
        "ARTIST" => ItemKey::TrackArtist,
        "ALBUM" => ItemKey::AlbumTitle,
        "ALBUMARTIST" | "ALBUM ARTIST" => ItemKey::AlbumArtist,
        "TRACKNUMBER" | "TRACK" => ItemKey::TrackNumber,
        "TRACKTOTAL" | "TOTALTRACKS" => ItemKey::TrackTotal,
        "DISCNUMBER" | "DISC" => ItemKey::DiscNumber,
        "DISCTOTAL" | "TOTALDISCS" => ItemKey::DiscTotal,
        "COMPILATION" => ItemKey::FlagCompilation,
        "GENRE" => ItemKey::Genre,
        "REPLAYGAIN_TRACK_GAIN" => ItemKey::ReplayGainTrackGain,
        "YEAR" => ItemKey::Year,
        "DATE" => ItemKey::RecordingDate,
        "ISRC" => ItemKey::Isrc,
        "COMMENT" => ItemKey::Comment,
        "COMPOSER" => ItemKey::Composer,
        "LYRICS" => ItemKey::Lyrics,
        "BPM" => ItemKey::Bpm,
        other => ItemKey::from_key(tag_type, other)
            .or_else(|| ItemKey::from_key(TagType::VorbisComments, other))
            .or_else(|| ItemKey::from_key(TagType::Id3v2, other))
            .unwrap_or(ItemKey::Comment),
    }
}

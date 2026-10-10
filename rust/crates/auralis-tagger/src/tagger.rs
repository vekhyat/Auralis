//! Lofty-backed audio metadata reader and writer.

use crate::cover::create_lofty_picture;
use crate::error::Result;
use crate::metadata::{AudioMetadata, MetadataTagSelection};
use lofty::config::WriteOptions;
use lofty::picture::PictureType;
use lofty::prelude::*;
use lofty::probe::Probe;
use lofty::tag::{ItemKey, ItemValue, Tag, TagItem};
use std::path::Path;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TagWriteMode {
    Overwrite,
    Merge,
}

pub fn read_audio_tags(path: &Path) -> Result<AudioMetadata> {
    let tagged_file = Probe::open(path)?.read()?;
    let tag = match tagged_file.primary_tag().or_else(|| tagged_file.first_tag()) {
        Some(t) => t,
        None => return Ok(AudioMetadata::default()),
    };

    let title = tag.get_string(ItemKey::TrackTitle).map(String::from);
    let artist = tag.get_string(ItemKey::TrackArtist).map(String::from);
    let album = tag.get_string(ItemKey::AlbumTitle).map(String::from);
    let album_artist = tag.get_string(ItemKey::AlbumArtist).map(String::from);
    let date = tag
        .get_string(ItemKey::RecordingDate)
        .or_else(|| tag.get_string(ItemKey::Year))
        .map(String::from);
    let year = tag
        .get_string(ItemKey::Year)
        .and_then(|y| y.parse::<u32>().ok());
    let track_number = tag.track();
    let total_tracks = tag.track_total();
    let disc_number = tag.disk();
    let total_discs = tag.disk_total();
    let genre = tag.get_string(ItemKey::Genre).map(String::from);
    let composer = tag.get_string(ItemKey::Composer).map(String::from);
    let copyright = tag.get_string(ItemKey::CopyrightMessage).map(String::from);
    let publisher = tag.get_string(ItemKey::Label).map(String::from);
    let isrc = tag.get_string(ItemKey::Isrc).map(String::from);
    let comment = tag.get_string(ItemKey::Comment).map(String::from);
    let lyrics = tag.get_string(ItemKey::Lyrics).map(String::from);

    Ok(AudioMetadata {
        title,
        artist,
        artists: None,
        album,
        album_artist,
        date,
        year,
        track_number,
        total_tracks,
        disc_number,
        total_discs,
        genre,
        composer,
        copyright,
        publisher,
        isrc,
        upc: None,
        comment,
        lyrics,
    })
}

pub fn write_audio_tags(
    path: &Path,
    metadata: &AudioMetadata,
    cover_path: Option<&Path>,
    selection: &MetadataTagSelection,
    mode: TagWriteMode,
) -> Result<()> {
    let tagged_file = Probe::open(path)?.read()?;

    let tag_type = tagged_file.primary_tag_type();
    let mut tag = match mode {
        TagWriteMode::Merge => {
            if let Some(existing) = tagged_file.first_tag() {
                existing.clone()
            } else {
                Tag::new(tag_type)
            }
        }
        TagWriteMode::Overwrite => Tag::new(tag_type),
    };

    apply_metadata_to_tag(&mut tag, metadata, selection);

    if let Some(cp) = cover_path {
        if cp.exists() {
            if let Ok(pic) = create_lofty_picture(cp) {
                tag.remove_picture_type(PictureType::CoverFront);
                tag.push_picture(pic);
            }
        }
    }

    tag.save_to_path(path, WriteOptions::default())?;
    Ok(())
}

pub fn apply_replaygain_tags(
    path: &Path,
    track_gain: Option<f64>,
    track_peak: Option<f64>,
    album_gain: Option<f64>,
    album_peak: Option<f64>,
) -> Result<()> {
    let tagged_file = Probe::open(path)?.read()?;
    let tag_type = tagged_file.primary_tag_type();

    let mut tag = if let Some(existing) = tagged_file.first_tag() {
        existing.clone()
    } else {
        Tag::new(tag_type)
    };

    if let Some(tg) = track_gain {
        let val = crate::replaygain::format_gain_db(tg);
        tag.insert(TagItem::new(
            ItemKey::ReplayGainTrackGain,
            ItemValue::Text(val),
        ));
    }

    if let Some(tp) = track_peak {
        let val = crate::replaygain::format_peak(tp);
        tag.insert(TagItem::new(
            ItemKey::ReplayGainTrackPeak,
            ItemValue::Text(val),
        ));
    }

    if let Some(ag) = album_gain {
        let val = crate::replaygain::format_gain_db(ag);
        tag.insert(TagItem::new(
            ItemKey::ReplayGainAlbumGain,
            ItemValue::Text(val),
        ));
    }

    if let Some(ap) = album_peak {
        let val = crate::replaygain::format_peak(ap);
        tag.insert(TagItem::new(
            ItemKey::ReplayGainAlbumPeak,
            ItemValue::Text(val),
        ));
    }

    tag.save_to_path(path, WriteOptions::default())?;
    Ok(())
}

fn apply_metadata_to_tag(
    tag: &mut Tag,
    metadata: &AudioMetadata,
    selection: &MetadataTagSelection,
) {
    if selection.title {
        if let Some(val) = &metadata.title {
            tag.insert_text(ItemKey::TrackTitle, val.clone());
        }
    }

    if selection.artist {
        if let Some(val) = &metadata.artist {
            tag.insert_text(ItemKey::TrackArtist, val.clone());
        }
    }

    if selection.album {
        if let Some(val) = &metadata.album {
            tag.insert_text(ItemKey::AlbumTitle, val.clone());
        }
    }

    if selection.album_artist {
        if let Some(val) = &metadata.album_artist {
            tag.insert_text(ItemKey::AlbumArtist, val.clone());
        }
    }

    if selection.date {
        if let Some(val) = &metadata.date {
            tag.insert_text(ItemKey::RecordingDate, val.clone());
        } else if let Some(year) = metadata.year {
            tag.insert_text(ItemKey::Year, year.to_string());
        }
    }

    if selection.track_number {
        if let Some(tr) = metadata.track_number {
            tag.set_track(tr);
        }
        if let Some(tot) = metadata.total_tracks {
            tag.set_track_total(tot);
        }
    }

    if selection.disc_number {
        if let Some(d) = metadata.disc_number {
            tag.set_disk(d);
        }
        if let Some(tot) = metadata.total_discs {
            tag.set_disk_total(tot);
        }
    }

    if selection.genre {
        if let Some(val) = &metadata.genre {
            tag.insert_text(ItemKey::Genre, val.clone());
        }
    }

    if selection.composer {
        if let Some(val) = &metadata.composer {
            tag.insert_text(ItemKey::Composer, val.clone());
        }
    }

    if selection.copyright {
        if let Some(val) = &metadata.copyright {
            tag.insert_text(ItemKey::CopyrightMessage, val.clone());
        }
    }

    if selection.label {
        if let Some(val) = &metadata.publisher {
            tag.insert_text(ItemKey::Label, val.clone());
        }
    }

    if selection.isrc {
        if let Some(val) = &metadata.isrc {
            tag.insert_text(ItemKey::Isrc, val.clone());
        }
    }

    if selection.comment {
        if let Some(val) = &metadata.comment {
            tag.insert_text(ItemKey::Comment, val.clone());
        }
    }

    if let Some(val) = &metadata.lyrics {
        tag.insert_text(ItemKey::Lyrics, val.clone());
    }
}

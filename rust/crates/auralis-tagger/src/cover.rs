//! Cover art extraction, resolution scaling, and lofty picture construction.

use crate::error::Result;
use lofty::picture::{MimeType, Picture, PictureType};
use std::path::Path;

const SPOTIFY_SIZE_300: &str = "ab67616d00001e02";
const SPOTIFY_SIZE_640: &str = "ab67616d0000b273";
const SPOTIFY_SIZE_MAX: &str = "ab67616d000082c1";

pub fn upgrade_spotify_cover_url(url: &str) -> String {
    let mut updated = url.to_string();
    if updated.contains(SPOTIFY_SIZE_300) {
        updated = updated.replacen(SPOTIFY_SIZE_300, SPOTIFY_SIZE_640, 1);
    }
    if updated.contains(SPOTIFY_SIZE_640) {
        updated = updated.replacen(SPOTIFY_SIZE_640, SPOTIFY_SIZE_MAX, 1);
    }
    updated
}

pub fn create_lofty_picture(image_path: &Path) -> Result<Picture> {
    let bytes = std::fs::read(image_path)?;
    create_lofty_picture_from_bytes(&bytes)
}

pub fn create_lofty_picture_from_bytes(bytes: &[u8]) -> Result<Picture> {
    let mime_type = match image::guess_format(bytes) {
        Ok(image::ImageFormat::Jpeg) => MimeType::Jpeg,
        Ok(image::ImageFormat::Png) => MimeType::Png,
        _ => MimeType::Jpeg,
    };

    let pic = Picture::unchecked(bytes.to_vec())
        .pic_type(PictureType::CoverFront)
        .mime_type(mime_type)
        .description("Front Cover")
        .build();

    Ok(pic)
}

pub async fn download_cover_art(url: &str, output_path: &Path) -> Result<()> {
    if let Some(parent) = output_path.parent() {
        std::fs::create_dir_all(parent)?;
    }

    let target_url = upgrade_spotify_cover_url(url);
    let client = reqwest::Client::builder()
        .user_agent("Auralis")
        .build()?;

    let bytes = client.get(&target_url).send().await?.bytes().await?;
    std::fs::write(output_path, &bytes)?;
    Ok(())
}

pub fn build_cover_filename(
    track_name: &str,
    artist_name: &str,
    album_name: &str,
    disc_number: u32,
    track_number: u32,
    include_track_number: bool,
) -> String {
    let mut parts = Vec::new();

    if include_track_number && track_number > 0 {
        if disc_number > 1 {
            parts.push(format!("{}-{:02}", disc_number, track_number));
        } else {
            parts.push(format!("{:02}", track_number));
        }
    }

    if !artist_name.is_empty() {
        parts.push(artist_name.trim().to_string());
    }

    if !album_name.is_empty() && album_name != track_name {
        parts.push(album_name.trim().to_string());
    } else if !track_name.is_empty() {
        parts.push(track_name.trim().to_string());
    }

    let sanitized = parts
        .join(" - ")
        .replace(['/', '\\', ':', '*', '?', '"', '<', '>', '|'], "_");

    format!("{}.jpg", sanitized)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_upgrade_spotify_cover_url() {
        let url_300 = format!("https://i.scdn.co/image/{}", SPOTIFY_SIZE_300);
        let upgraded = upgrade_spotify_cover_url(&url_300);
        assert!(upgraded.contains(SPOTIFY_SIZE_MAX));

        let url_640 = format!("https://i.scdn.co/image/{}", SPOTIFY_SIZE_640);
        let upgraded_max = upgrade_spotify_cover_url(&url_640);
        assert!(upgraded_max.contains(SPOTIFY_SIZE_MAX));
    }

    #[test]
    fn test_build_cover_filename() {
        let name = build_cover_filename("Track Title", "Artist", "Album", 1, 3, true);
        assert_eq!(name, "03 - Artist - Album.jpg");

        let name_disc2 = build_cover_filename("Track Title", "Artist", "Album", 2, 4, true);
        assert_eq!(name_disc2, "2-04 - Artist - Album.jpg");
    }
}

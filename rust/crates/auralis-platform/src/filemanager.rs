//! File system exploration, audio file filtering, metadata inspection, and batch rename operations.
//!
//! Port of `backend/filemanager.go` and `backend/upc_tags.go`.

use crate::error::FileManagerError;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::{Path, PathBuf};

/// Preferred UPC tag identifier.
pub const PREFERRED_UPC_TAG_KEY: &str = "UPC";

/// Tag key priority order for ffprobe metadata dictionaries.
pub const FFPROBE_UPC_TAG_KEYS: &[&str] = &[
    "upc",
    "barcode",
    "wm/upc",
    "txxx:upc",
    "txxx:barcode",
    "txxx/upc",
    "txxx/barcode",
    "----:com.apple.itunes:upc",
    "----:com.apple.itunes:barcode",
];

/// Normalized UPC tag keys for uppercase multi-value tag dictionaries.
pub const NORMALIZED_UPC_TAG_KEYS: &[&str] = &[
    PREFERRED_UPC_TAG_KEY,
    "BARCODE",
    "TXXX:UPC",
    "TXXX:BARCODE",
    "TXXX/UPC",
    "TXXX/BARCODE",
    "WM/UPC",
    "----:COM.APPLE.ITUNES:UPC",
    "----:COM.APPLE.ITUNES:BARCODE",
];

/// Recognized audio extensions for scanning and filtering.
pub const AUDIO_EXTENSIONS: &[&str] = &[
    ".flac", ".mp3", ".m4a", ".mp4", ".m4b", ".aac", ".wav", ".aiff", ".aif", ".ogg", ".opus",
    ".ape", ".wv", ".mpc",
];

/// Basic file/folder metadata representation used by frontend tree views.
///
/// Port of `backend.FileInfo`.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct FileInfo {
    pub name: String,
    pub path: String,
    pub is_dir: bool,
    pub size: i64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub children: Option<Vec<FileInfo>>,
}

/// Extracted audio track tags.
///
/// Port of `backend.AudioMetadata`.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Default)]
pub struct AudioMetadata {
    pub title: String,
    pub artist: String,
    pub album: String,
    pub album_artist: String,
    pub track_number: i32,
    pub disc_number: i32,
    pub year: String,
    pub isrc: String,
    pub upc: String,
}

/// Preview of a planned file rename operation.
///
/// Port of `backend.RenamePreview`.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct RenamePreview {
    pub old_path: String,
    pub old_name: String,
    pub new_name: String,
    pub new_path: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    pub metadata: AudioMetadata,
}

/// Result of executing a file rename operation.
///
/// Port of `backend.RenameResult`.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct RenameResult {
    pub old_path: String,
    pub new_path: String,
    pub success: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

/// Lists all files and directories in `dir_path` recursively, skipping `.auralis-incoming-*` staging dirs.
///
/// Entries are sorted by filename to ensure deterministic results.
///
/// Port of `backend.ListDirectory(dirPath)`.
pub fn list_directory(dir_path: impl AsRef<Path>) -> Result<Vec<FileInfo>, FileManagerError> {
    let path = dir_path.as_ref();
    let read_dir = std::fs::read_dir(path)
        .map_err(|e| FileManagerError::ReadDir(format!("failed to read directory: {}", e)))?;

    let mut dir_entries = Vec::new();
    for entry in read_dir {
        let entry = entry.map_err(|e| {
            FileManagerError::ReadDir(format!("failed to read directory entry: {}", e))
        })?;
        dir_entries.push(entry);
    }

    dir_entries.sort_by_key(|e| e.file_name());

    let mut result = Vec::new();
    for entry in dir_entries {
        let file_name = entry.file_name().to_string_lossy().to_string();
        let file_type = match entry.file_type() {
            Ok(ft) => ft,
            Err(_) => continue,
        };

        if file_type.is_dir() && file_name.starts_with(".auralis-incoming-") {
            continue;
        }

        let meta = match entry.metadata() {
            Ok(m) => m,
            Err(_) => continue,
        };

        let is_dir = file_type.is_dir();
        let full_path = entry.path();
        let mut file_info = FileInfo {
            name: file_name,
            path: full_path.to_string_lossy().to_string(),
            is_dir,
            size: meta.len() as i64,
            children: None,
        };

        if is_dir {
            // Go's `omitempty` drops an empty `children` slice entirely.
            if let Ok(children) = list_directory(&full_path) {
                if !children.is_empty() {
                    file_info.children = Some(children);
                }
            }
        }

        result.push(file_info);
    }

    Ok(result)
}

/// Recursively scans `dir_path` and returns all supported audio files.
///
/// Skips any directories prefixed with `.auralis-incoming-`.
///
/// Port of `backend.ListAudioFiles(dirPath)`.
pub fn list_audio_files(dir_path: impl AsRef<Path>) -> Result<Vec<FileInfo>, FileManagerError> {
    let root = dir_path.as_ref();
    if !root.exists() {
        return Err(FileManagerError::WalkDir(format!(
            "failed to walk directory: path '{}' does not exist",
            root.display()
        )));
    }

    let mut result = Vec::new();
    walk_audio_files_recursive(root, &mut result)?;
    Ok(result)
}

fn walk_audio_files_recursive(
    current: &Path,
    result: &mut Vec<FileInfo>,
) -> Result<(), FileManagerError> {
    let read_res = std::fs::read_dir(current);
    let entries = match read_res {
        Ok(e) => e,
        Err(_) => return Ok(()),
    };

    let mut sorted_entries = Vec::new();
    for entry in entries.flatten() {
        sorted_entries.push(entry);
    }
    sorted_entries.sort_by_key(|e| e.file_name());

    for entry in sorted_entries {
        let name = entry.file_name().to_string_lossy().to_string();
        let file_type = match entry.file_type() {
            Ok(ft) => ft,
            Err(_) => continue,
        };

        if file_type.is_dir() {
            if name.starts_with(".auralis-incoming-") {
                continue;
            }
            walk_audio_files_recursive(&entry.path(), result)?;
            continue;
        }

        let ext = entry
            .path()
            .extension()
            .map(|e| format!(".{}", e.to_string_lossy().to_lowercase()))
            .unwrap_or_default();

        if AUDIO_EXTENSIONS.contains(&ext.as_str()) {
            let meta = match entry.metadata() {
                Ok(m) => m,
                Err(_) => continue,
            };
            result.push(FileInfo {
                name,
                path: entry.path().to_string_lossy().to_string(),
                is_dir: false,
                size: meta.len() as i64,
                children: None,
            });
        }
    }

    Ok(())
}

/// Reads audio metadata from a supported file (`.flac`, `.mp3`, or `.m4a`).
///
/// Port of `backend.ReadAudioMetadata(filePath)`.
pub fn read_audio_metadata(file_path: impl AsRef<Path>) -> Result<AudioMetadata, FileManagerError> {
    let path = file_path.as_ref();
    if !path.exists() || path.is_dir() {
        return Err(FileManagerError::NotFound("file does not exist".into()));
    }

    let ext = path
        .extension()
        .map(|e| format!(".{}", e.to_string_lossy().to_lowercase()))
        .unwrap_or_default();

    match ext.as_str() {
        ".flac" => read_flac_metadata(path),
        ".mp3" => read_mp3_metadata(path),
        ".m4a" => read_m4a_metadata(path),
        _ => Err(FileManagerError::UnsupportedFormat(format!(
            "unsupported file format: {}",
            ext
        ))),
    }
}

/// Reads Vorbis comments from a FLAC file.
///
/// Port of `backend.readFlacMetadata(filePath)`.
pub fn read_flac_metadata(file_path: impl AsRef<Path>) -> Result<AudioMetadata, FileManagerError> {
    let path = file_path.as_ref();
    if let Ok(meta) = parse_flac_vorbis_comments(path) {
        return Ok(meta);
    }

    // Fallback to lofty tag reader
    read_audio_tags_lofty(path, lofty::tag::TagType::VorbisComments)
}

fn parse_flac_vorbis_comments(path: &Path) -> Result<AudioMetadata, FileManagerError> {
    use std::io::{Read, Seek, SeekFrom};
    let mut file = std::fs::File::open(path)?;
    let mut magic = [0u8; 4];
    file.read_exact(&mut magic)?;
    if &magic != b"fLaC" {
        return Err(FileManagerError::FlacParse(
            "not a FLAC file (invalid magic)".into(),
        ));
    }

    let mut metadata = AudioMetadata::default();
    let mut found_comments = false;

    loop {
        let mut header = [0u8; 4];
        if file.read_exact(&mut header).is_err() {
            break;
        }
        let is_last = (header[0] & 0x80) != 0;
        let block_type = header[0] & 0x7F;
        let length = ((header[1] as u32) << 16) | ((header[2] as u32) << 8) | (header[3] as u32);

        if block_type == 4 {
            // Vorbis comment block
            found_comments = true;
            let mut block_data = vec![0u8; length as usize];
            file.read_exact(&mut block_data)?;
            parse_vorbis_comment_payload(&block_data, &mut metadata);
        } else {
            file.seek(SeekFrom::Current(length as i64))?;
        }

        if is_last {
            break;
        }
    }

    if found_comments {
        Ok(metadata)
    } else {
        Err(FileManagerError::FlacParse(
            "no Vorbis comment block found in FLAC file".into(),
        ))
    }
}

fn parse_vorbis_comment_payload(data: &[u8], metadata: &mut AudioMetadata) {
    if data.len() < 8 {
        return;
    }
    let mut cursor = 0;
    let vendor_len = u32::from_le_bytes(data[cursor..cursor + 4].try_into().unwrap()) as usize;
    cursor += 4;
    cursor += vendor_len;
    if cursor + 4 > data.len() {
        return;
    }

    let comment_count = u32::from_le_bytes(data[cursor..cursor + 4].try_into().unwrap()) as usize;
    cursor += 4;

    for _ in 0..comment_count {
        if cursor + 4 > data.len() {
            break;
        }
        let comment_len = u32::from_le_bytes(data[cursor..cursor + 4].try_into().unwrap()) as usize;
        cursor += 4;
        if cursor + comment_len > data.len() {
            break;
        }
        let comment_str = String::from_utf8_lossy(&data[cursor..cursor + comment_len]);
        cursor += comment_len;

        if let Some((field, value)) = comment_str.split_once('=') {
            let field_upper = field.to_ascii_uppercase();
            match field_upper.as_str() {
                "TITLE" => metadata.title = value.to_string(),
                "ARTIST" => metadata.artist = value.to_string(),
                "ALBUM" => metadata.album = value.to_string(),
                "ALBUMARTIST" => metadata.album_artist = value.to_string(),
                "TRACKNUMBER" => {
                    let track_str = value.split('/').next().unwrap_or(value);
                    if let Ok(num) = track_str.parse::<i32>() {
                        metadata.track_number = num;
                    }
                }
                "DISCNUMBER" => {
                    let disc_str = value.split('/').next().unwrap_or(value);
                    if let Ok(num) = disc_str.parse::<i32>() {
                        metadata.disc_number = num;
                    }
                }
                "DATE" | "YEAR" => metadata.year = value.to_string(),
                "ISRC" | "TSRC" => metadata.isrc = value.to_string(),
                "UPC" => assign_preferred_upc(&mut metadata.upc, value, true),
                "BARCODE" => assign_preferred_upc(&mut metadata.upc, value, false),
                _ => {}
            }
        }
    }
}

/// Reads ID3v2 metadata from an MP3 file.
///
/// Port of `backend.readMp3Metadata(filePath)`.
pub fn read_mp3_metadata(file_path: impl AsRef<Path>) -> Result<AudioMetadata, FileManagerError> {
    let path = file_path.as_ref();
    read_audio_tags_lofty(path, lofty::tag::TagType::Id3v2)
}

fn read_audio_tags_lofty(
    path: &Path,
    preferred_type: lofty::tag::TagType,
) -> Result<AudioMetadata, FileManagerError> {
    use lofty::prelude::*;
    use lofty::probe::Probe;
    use lofty::tag::ItemKey;

    let probe = Probe::open(path)
        .map_err(|e| FileManagerError::NotFound(format!("failed to open file: {}", e)))?;
    let tagged_file = probe
        .read()
        .map_err(|e| FileManagerError::Probe(format!("failed to read tags: {}", e)))?;

    let tag = match tagged_file
        .tag(preferred_type)
        .or_else(|| tagged_file.primary_tag())
        .or_else(|| tagged_file.first_tag())
    {
        Some(t) => t,
        None => return Ok(AudioMetadata::default()),
    };

    let title = tag
        .get_string(ItemKey::TrackTitle)
        .unwrap_or_default()
        .to_string();
    let artist = tag
        .get_string(ItemKey::TrackArtist)
        .unwrap_or_default()
        .to_string();
    let album = tag
        .get_string(ItemKey::AlbumTitle)
        .unwrap_or_default()
        .to_string();
    let album_artist = tag
        .get_string(ItemKey::AlbumArtist)
        .unwrap_or_default()
        .to_string();
    let year = tag
        .get_string(ItemKey::RecordingDate)
        .or_else(|| tag.get_string(ItemKey::Year))
        .unwrap_or_default()
        .to_string();
    let isrc = tag
        .get_string(ItemKey::Isrc)
        .unwrap_or_default()
        .to_string();
    let track_number = tag.track().unwrap_or(0) as i32;
    let disc_number = tag.disk().unwrap_or(0) as i32;

    let mut upc = tag
        .get_string(ItemKey::Barcode)
        .unwrap_or_default()
        .to_string();

    // Check items for TXXX / UPC / Barcode
    for item in tag.items() {
        if let Some(val) = item.value().text() {
            let key_str = format!("{:?}", item.key());
            let (matched, preferred) = classify_upc_description(&key_str);
            if matched {
                assign_preferred_upc(&mut upc, val, preferred);
                if preferred && !upc.trim().is_empty() {
                    break;
                }
            }
        }
    }

    Ok(AudioMetadata {
        title,
        artist,
        album,
        album_artist,
        track_number,
        disc_number,
        year,
        isrc,
        upc,
    })
}

/// Reads audio metadata by invoking `ffprobe` as a subprocess.
///
/// Port of `backend.readMetadataWithFFprobe(filePath)`.
pub fn read_metadata_with_ffprobe(
    file_path: impl AsRef<Path>,
) -> Result<AudioMetadata, FileManagerError> {
    let path = file_path.as_ref();
    let ffprobe_path = auralis_ffmpeg::locator::get_ffprobe_path()
        .map_err(|e| FileManagerError::Probe(format!("ffprobe not found: {}", e)))?;

    auralis_ffmpeg::locator::validate_executable(&ffprobe_path)
        .map_err(|e| FileManagerError::Probe(format!("invalid ffprobe executable: {}", e)))?;

    let mut cmd = std::process::Command::new(ffprobe_path);
    cmd.args([
        "-v",
        "quiet",
        "-print_format",
        "json",
        "-show_format",
        "-show_streams",
    ]);
    cmd.arg(path);
    auralis_ffmpeg::process::configure_command(&mut cmd);

    let output = cmd
        .output()
        .map_err(|e| FileManagerError::Probe(format!("failed to execute ffprobe: {}", e)))?;

    if !output.status.success() {
        let err = String::from_utf8_lossy(&output.stderr);
        return Err(FileManagerError::Probe(format!(
            "ffprobe exited with error: {}",
            err
        )));
    }

    #[derive(Deserialize, Default)]
    struct ProbeFormat {
        #[serde(default)]
        tags: HashMap<String, String>,
    }

    #[derive(Deserialize, Default)]
    struct ProbeStream {
        #[serde(default)]
        tags: HashMap<String, String>,
    }

    #[derive(Deserialize, Default)]
    struct ProbeJson {
        #[serde(default)]
        format: Option<ProbeFormat>,
        #[serde(default)]
        streams: Vec<ProbeStream>,
    }

    let parsed: ProbeJson = serde_json::from_slice(&output.stdout)
        .map_err(|e| FileManagerError::Probe(format!("failed to parse ffprobe json: {}", e)))?;

    let mut all_tags = HashMap::new();
    for stream in parsed.streams {
        for (k, v) in stream.tags {
            all_tags.insert(k.to_lowercase(), v);
        }
    }
    if let Some(format) = parsed.format {
        for (k, v) in format.tags {
            all_tags.insert(k.to_lowercase(), v);
        }
    }

    let mut metadata = AudioMetadata::default();
    for (k, v) in &all_tags {
        match k.as_str() {
            "title" => metadata.title = v.clone(),
            "artist" => metadata.artist = v.clone(),
            "album" => metadata.album = v.clone(),
            "album_artist" | "albumartist" => metadata.album_artist = v.clone(),
            "track" => {
                let track_str = v.split('/').next().unwrap_or(v);
                if let Ok(num) = track_str.parse::<i32>() {
                    metadata.track_number = num;
                }
            }
            "disc" => {
                let disc_str = v.split('/').next().unwrap_or(v);
                if let Ok(num) = disc_str.parse::<i32>() {
                    metadata.disc_number = num;
                }
            }
            "date" | "year" => {
                if metadata.year.is_empty() || v.len() > metadata.year.len() {
                    metadata.year = v.clone();
                }
            }
            "isrc" | "tsrc" => metadata.isrc = v.clone(),
            _ => {}
        }
    }

    metadata.upc = first_preferred_ffprobe_upc_value(&all_tags);
    Ok(metadata)
}

/// Reads metadata from an M4A file via ffprobe with lofty fallback.
///
/// If probing fails, returns an empty metadata struct without error.
///
/// Port of `backend.readM4aMetadata(filePath)`.
pub fn read_m4a_metadata(file_path: impl AsRef<Path>) -> Result<AudioMetadata, FileManagerError> {
    let path = file_path.as_ref();
    if let Ok(metadata) = read_metadata_with_ffprobe(path) {
        return Ok(metadata);
    }

    if let Ok(metadata) = read_audio_tags_lofty(path, lofty::tag::TagType::Mp4Ilst) {
        return Ok(metadata);
    }

    Ok(AudioMetadata::default())
}

/// Generates a formatted filename string from metadata and a template format string.
///
/// Supported template placeholders:
/// - `{title}`: track title
/// - `{artist}`: artist name
/// - `{album}`: album name
/// - `{album_artist}`: album artist
/// - `{year}`: 4-digit release year
/// - `{date}`: full date/year string
/// - `{isrc}`: ISRC code
/// - `{track}`: 2-digit zero-padded track number
/// - `{disc}`: disc number
///
/// Invalid filename characters (`< > : " / \ | ? *`) are stripped, whitespace is collapsed,
/// and leading/trailing punctuation (` -._`) is trimmed.
///
/// Port of `backend.GenerateFilename(metadata, format, ext)`.
pub fn generate_filename(metadata: Option<&AudioMetadata>, format: &str, ext: &str) -> String {
    let metadata = match metadata {
        Some(m) => m,
        None => return String::new(),
    };

    let mut result = format.to_string();

    let year: String = metadata.year.chars().take(4).collect();

    result = result.replace("{title}", &sanitize_filename_for_rename(&metadata.title));
    result = result.replace("{artist}", &sanitize_filename_for_rename(&metadata.artist));
    result = result.replace("{album}", &sanitize_filename_for_rename(&metadata.album));
    result = result.replace(
        "{album_artist}",
        &sanitize_filename_for_rename(&metadata.album_artist),
    );
    result = result.replace("{year}", &sanitize_filename_for_rename(&year));
    result = result.replace("{date}", &sanitize_filename_for_rename(&metadata.year));
    result = result.replace("{isrc}", &sanitize_filename_for_rename(&metadata.isrc));

    if metadata.track_number > 0 {
        result = result.replace("{track}", &format!("{:02}", metadata.track_number));
    } else {
        result = result.replace("{track}", "");
    }

    if metadata.disc_number > 0 {
        result = result.replace("{disc}", &format!("{}", metadata.disc_number));
    } else {
        result = result.replace("{disc}", "");
    }

    let trimmed = result.trim();
    let collapsed: Vec<&str> = trimmed.split_whitespace().collect();
    let joined = collapsed.join(" ");

    let cleaned = joined.trim_matches(|c: char| c == ' ' || c == '-' || c == '.' || c == '_');

    if cleaned.is_empty() {
        return String::new();
    }

    format!("{}{}", cleaned, ext)
}

/// Strips characters invalid in Windows/POSIX filenames (`< > : " / \ | ? *`) and trims outer whitespace.
///
/// Port of `backend.sanitizeFilenameForRename(name)`.
pub fn sanitize_filename_for_rename(name: &str) -> String {
    let invalid = ['<', '>', ':', '"', '/', '\\', '|', '?', '*'];
    let result: String = name.chars().filter(|c| !invalid.contains(c)).collect();
    result.trim().to_string()
}

/// Generates preview results for batch renaming a list of files using a format template.
///
/// Port of `backend.PreviewRename(files, format)`.
pub fn preview_rename(files: &[impl AsRef<Path>], format: &str) -> Vec<RenamePreview> {
    let mut previews = Vec::with_capacity(files.len());

    for file_path in files {
        let path = file_path.as_ref();
        let old_path = path.to_string_lossy().to_string();
        let old_name = path
            .file_name()
            .map(|n| n.to_string_lossy().to_string())
            .unwrap_or_default();

        let mut preview = RenamePreview {
            old_path,
            old_name,
            new_name: String::new(),
            new_path: String::new(),
            error: None,
            metadata: AudioMetadata::default(),
        };

        let metadata = match read_audio_metadata(path) {
            Ok(m) => m,
            Err(e) => {
                preview.error = Some(e.to_string());
                previews.push(preview);
                continue;
            }
        };

        preview.metadata = metadata.clone();

        let ext = match path.extension() {
            Some(e) => format!(".{}", e.to_string_lossy()),
            None => String::new(),
        };

        let new_name = generate_filename(Some(&metadata), format, &ext);
        if new_name.is_empty() {
            preview.error = Some("Could not generate filename (missing metadata)".to_string());
            previews.push(preview);
            continue;
        }

        let new_path = path
            .parent()
            .map(|p| p.join(&new_name))
            .unwrap_or_else(|| PathBuf::from(&new_name));

        preview.new_name = new_name;
        preview.new_path = new_path.to_string_lossy().to_string();
        previews.push(preview);
    }

    previews
}

/// Returns a map of file paths to their sizes in bytes.
///
/// Port of `backend.GetFileSizes(files)`.
pub fn get_file_sizes(files: &[impl AsRef<Path>]) -> HashMap<String, i64> {
    let mut result = HashMap::new();
    for file_path in files {
        let path = file_path.as_ref();
        if let Ok(metadata) = std::fs::metadata(path) {
            result.insert(path.to_string_lossy().to_string(), metadata.len() as i64);
        }
    }
    result
}

/// Renames files according to metadata and the given format template.
///
/// Port of `backend.RenameFiles(files, format)`.
pub fn rename_files(files: &[impl AsRef<Path>], format: &str) -> Vec<RenameResult> {
    let mut results = Vec::with_capacity(files.len());

    for file_path in files {
        let path = file_path.as_ref();
        let old_path = path.to_string_lossy().to_string();

        let mut result = RenameResult {
            old_path,
            new_path: String::new(),
            success: false,
            error: None,
        };

        let metadata = match read_audio_metadata(path) {
            Ok(m) => m,
            Err(e) => {
                result.error = Some(e.to_string());
                results.push(result);
                continue;
            }
        };

        let ext = match path.extension() {
            Some(e) => format!(".{}", e.to_string_lossy()),
            None => String::new(),
        };

        let new_name = generate_filename(Some(&metadata), format, &ext);
        if new_name.is_empty() {
            result.error = Some("Could not generate filename (missing metadata)".to_string());
            results.push(result);
            continue;
        }

        let new_path_buf = path
            .parent()
            .map(|p| p.join(&new_name))
            .unwrap_or_else(|| PathBuf::from(&new_name));
        let new_path = new_path_buf.to_string_lossy().to_string();
        result.new_path = new_path;

        if new_path_buf != path && new_path_buf.exists() {
            result.error = Some("File already exists".to_string());
            results.push(result);
            continue;
        }

        if let Err(e) = std::fs::rename(path, &new_path_buf) {
            result.error = Some(e.to_string());
            results.push(result);
            continue;
        }

        result.success = true;
        results.push(result);
    }

    results
}

/// Assigns a candidate UPC value to `current` respecting preference priority.
///
/// Port of `backend.assignPreferredUPC`.
pub fn assign_preferred_upc(current: &mut String, incoming: &str, preferred: bool) {
    let incoming = incoming.trim();
    if incoming.is_empty() {
        return;
    }
    if preferred || current.trim().is_empty() {
        *current = incoming.to_string();
    }
}

/// Classifies a tag description as a UPC or barcode field.
///
/// Port of `backend.classifyUPCDescription`.
pub fn classify_upc_description(description: &str) -> (bool, bool) {
    let desc = description.trim().to_ascii_uppercase();
    if desc.ends_with(PREFERRED_UPC_TAG_KEY) || desc == PREFERRED_UPC_TAG_KEY {
        (true, true)
    } else if desc.ends_with("BARCODE") || desc == "BARCODE" {
        (true, false)
    } else {
        (false, false)
    }
}

/// Finds the highest-priority UPC tag value from an ffprobe tags dictionary.
///
/// Port of `backend.firstPreferredFFprobeUPCValue`.
pub fn first_preferred_ffprobe_upc_value(tags: &HashMap<String, String>) -> String {
    for key in FFPROBE_UPC_TAG_KEYS {
        if let Some(val) = tags.get(*key) {
            let trimmed = val.trim();
            if !trimmed.is_empty() {
                return trimmed.to_string();
            }
        }
    }
    String::new()
}

/// Finds the highest-priority UPC tag value from a multi-value normalized tags dictionary.
///
/// Port of `backend.firstPreferredNormalizedUPCValue`.
pub fn first_preferred_normalized_upc_value(tags: &HashMap<String, Vec<String>>) -> String {
    for key in NORMALIZED_UPC_TAG_KEYS {
        let norm_key = key.trim().to_ascii_uppercase();
        if let Some(values) = tags.get(&norm_key) {
            for val in values {
                let trimmed = val.trim();
                if !trimmed.is_empty() {
                    return trimmed.to_string();
                }
            }
        }
    }
    String::new()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_directory_omits_children_like_go() {
        let temp = tempfile::tempdir().unwrap();
        std::fs::create_dir(temp.path().join("empty")).unwrap();
        let listing = list_directory(temp.path()).unwrap();
        assert_eq!(listing.len(), 1);
        assert!(listing[0].children.is_none());
        let json = serde_json::to_value(&listing[0]).unwrap();
        assert!(json.get("children").is_none());
    }

    #[test]
    fn test_library_scan_excludes_incoming_downloads() {
        // Port of TestLibraryScanExcludesIncomingDownloads in backend/library_staging_test.go
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path();

        let stage = root.join(".auralis-incoming-test");
        std::fs::create_dir_all(&stage).unwrap();

        let contents = vec![0u8; 1024];
        std::fs::write(stage.join("partial.flac"), &contents).unwrap();
        std::fs::write(root.join("finished.flac"), &contents).unwrap();

        let files = list_audio_files(root).expect("list audio files");
        assert_eq!(files.len(), 1, "only 1 audio file should be visible");
        assert_eq!(files[0].name, "finished.flac");

        let listing = list_directory(root).expect("list directory");
        assert_eq!(
            listing.len(),
            1,
            "incoming folder must be excluded from listing"
        );
        assert_eq!(listing[0].name, "finished.flac");
    }

    #[test]
    fn test_generate_filename_placeholders_and_sanitization() {
        let metadata = AudioMetadata {
            title: "Song: Title / Test *".to_string(),
            artist: "Artist <1>".to_string(),
            album: "Album? Name".to_string(),
            album_artist: "Various Artists".to_string(),
            track_number: 5,
            disc_number: 1,
            year: "2024-03-15".to_string(),
            isrc: "USRC12345678".to_string(),
            upc: "123456789012".to_string(),
        };

        // Test format with track, artist, title
        let name = generate_filename(Some(&metadata), "{track} - {artist} - {title}", ".flac");
        assert_eq!(name, "05 - Artist 1 - Song Title Test.flac");

        // Test year (takes 4 chars) and date (full date)
        let name_year = generate_filename(
            Some(&metadata),
            "[{year}] {album} - {track} - {title}",
            ".mp3",
        );
        assert_eq!(name_year, "[2024] Album Name - 05 - Song Title Test.mp3");

        let name_date = generate_filename(Some(&metadata), "{date} - {title}", ".m4a");
        assert_eq!(name_date, "2024-03-15 - Song Title Test.m4a");

        // Test zero track and zero disc
        let mut zero_meta = metadata.clone();
        zero_meta.track_number = 0;
        zero_meta.disc_number = 0;
        let zero_name = generate_filename(Some(&zero_meta), "{track} {disc} {title}", ".flac");
        assert_eq!(zero_name, "Song Title Test.flac");

        // Test empty/missing metadata produces empty string
        assert_eq!(generate_filename(None, "{title}", ".flac"), "");
    }

    #[test]
    fn test_sanitize_filename_for_rename() {
        let dirty = "A/B\\C:D*E?F\"G<H>I|J  ";
        let clean = sanitize_filename_for_rename(dirty);
        assert_eq!(clean, "ABCDEFGHIJ");
    }

    #[test]
    fn test_get_file_sizes() {
        let temp = tempfile::tempdir().unwrap();
        let file1 = temp.path().join("a.txt");
        let file2 = temp.path().join("b.txt");

        std::fs::write(&file1, b"hello").unwrap();
        std::fs::write(&file2, b"world 12345").unwrap();

        let sizes = get_file_sizes(&[&file1, &file2]);
        assert_eq!(sizes.get(&file1.to_string_lossy().to_string()), Some(&5));
        assert_eq!(sizes.get(&file2.to_string_lossy().to_string()), Some(&11));
    }

    #[test]
    fn test_assign_preferred_upc_and_classification() {
        let (matched, preferred) = classify_upc_description("UPC");
        assert!(matched && preferred);

        let (matched2, preferred2) = classify_upc_description("BARCODE");
        assert!(matched2 && !preferred2);

        let (matched3, _) = classify_upc_description("OTHER");
        assert!(!matched3);

        let mut upc = String::new();
        assign_preferred_upc(&mut upc, "111", false);
        assert_eq!(upc, "111");

        // Non-preferred does not overwrite non-empty
        assign_preferred_upc(&mut upc, "222", false);
        assert_eq!(upc, "111");

        // Preferred overwrites non-preferred
        assign_preferred_upc(&mut upc, "333", true);
        assert_eq!(upc, "333");
    }

    #[test]
    fn test_first_preferred_ffprobe_upc_value() {
        let mut tags = HashMap::new();
        tags.insert("barcode".to_string(), "barcode_val".to_string());
        tags.insert("upc".to_string(), "upc_val".to_string());
        // "upc" comes before "barcode" in FFPROBE_UPC_TAG_KEYS
        assert_eq!(first_preferred_ffprobe_upc_value(&tags), "upc_val");

        tags.remove("upc");
        assert_eq!(first_preferred_ffprobe_upc_value(&tags), "barcode_val");
    }

    #[test]
    fn test_preview_rename_and_rename_files() {
        let temp = tempfile::tempdir().unwrap();
        let file1 = temp.path().join("track1.flac");

        // Create a minimal synthetic FLAC file with a Vorbis comment block
        let mut flac_bytes = Vec::new();
        flac_bytes.extend_from_slice(b"fLaC");

        // Vorbis comment payload
        let vendor = b"test_vendor";
        let mut payload = Vec::new();
        payload.extend_from_slice(&(vendor.len() as u32).to_le_bytes());
        payload.extend_from_slice(vendor);

        let comments = [
            "TITLE=Echoes",
            "ARTIST=Pink Floyd",
            "ALBUM=Meddle",
            "TRACKNUMBER=6",
            "DATE=1971",
        ];
        payload.extend_from_slice(&(comments.len() as u32).to_le_bytes());
        for c in comments {
            payload.extend_from_slice(&(c.len() as u32).to_le_bytes());
            payload.extend_from_slice(c.as_bytes());
        }

        // Header: is_last = 1, type = 4 (VORBIS_COMMENT), length = 24-bit
        let len = payload.len() as u32;
        flac_bytes.push(0x84); // is_last (0x80) | type 4
        flac_bytes.push(((len >> 16) & 0xFF) as u8);
        flac_bytes.push(((len >> 8) & 0xFF) as u8);
        flac_bytes.push((len & 0xFF) as u8);
        flac_bytes.extend_from_slice(&payload);

        std::fs::write(&file1, &flac_bytes).unwrap();

        // 1. Test preview_rename
        let previews = preview_rename(&[&file1], "{track} - {title}");
        assert_eq!(previews.len(), 1);
        let p = &previews[0];
        assert_eq!(p.error, None);
        assert_eq!(p.new_name, "06 - Echoes.flac");
        assert_eq!(p.metadata.title, "Echoes");
        assert_eq!(p.metadata.artist, "Pink Floyd");
        assert_eq!(p.metadata.track_number, 6);

        // 2. Test rename_files
        let results = rename_files(&[&file1], "{track} - {title}");
        assert_eq!(results.len(), 1);
        assert!(results[0].success);
        assert_eq!(results[0].error, None);
        let expected_new_path = temp.path().join("06 - Echoes.flac");
        assert!(expected_new_path.exists());
        assert!(!file1.exists());
    }
}

//! JioSaavn provider client, search, stream URL resolution, and DES media URL decryption.
//!
//! Port of `backend/jiosaavn.go`.
//!
//! Note: The download crate wires [`search_jiosaavn`], [`resolve_jiosaavn_track`],
//! and [`finalize_jiosaavn_path`] into the `ExtraDownloadParams` entry point.

use crate::error::{ProviderError, Result};
use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine;
use cipher::{BlockDecrypt, KeyInit};
use des::Des;
use regex::Regex;
use serde::Deserialize;
use std::path::{Path, PathBuf};
use std::sync::LazyLock;

pub const JIOSAAVN_OFFICIAL_API: &str = "https://www.jiosaavn.com/api.php";
pub const JIOSAAVN_DES_KEY: &[u8; 8] = b"38346591";

static FEAT_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)\s*[(\[](feat\.?|ft\.?|with|featuring)\s[^)\]]+[)\]]").unwrap()
});
static BRACKETS_RE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"\s*[(\[].*?[)\]]\s*").unwrap());
static CREDIT_SPLIT_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)\s*(?:,|&|/|;|\u{00b7}|\u{2022}|\s-\s|\s+x\s+|\s+feat\.?\s+|\s+ft\.?\s+|\s+featuring\s+)\s*").unwrap()
});
static TRAILING_WRAP_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\s*[(\[]([^)\]]+)[)\]]\s*$").unwrap());
static TRAILING_DASH_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?i)\s+[-–—:]\s+(.+)$").unwrap());
static YEAR_RE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^(19|20)\d{2}$").unwrap());

/// Minimal HTML unescape function handling named entities and numeric references.
pub fn html_unescape(input: &str) -> String {
    if !input.contains('&') {
        return input.to_string();
    }
    let mut out = String::with_capacity(input.len());
    let chars: Vec<char> = input.chars().collect();
    let mut i = 0;
    while i < chars.len() {
        if chars[i] == '&' {
            if let Some(semi) = chars[i..].iter().position(|&c| c == ';') {
                let entity: String = chars[i + 1..i + semi].iter().collect();
                let replacement = match entity.as_str() {
                    "amp" => Some('&'),
                    "quot" => Some('"'),
                    "apos" => Some('\''),
                    "lt" => Some('<'),
                    "gt" => Some('>'),
                    "nbsp" => Some(' '),
                    "copy" => Some('©'),
                    "middot" => Some('·'),
                    "bull" => Some('•'),
                    s if s.starts_with('#') => {
                        let num_str = &s[1..];
                        if let Some(hex) = num_str.strip_prefix(['x', 'X']) {
                            u32::from_str_radix(hex, 16).ok().and_then(char::from_u32)
                        } else {
                            num_str.parse::<u32>().ok().and_then(char::from_u32)
                        }
                    }
                    _ => None,
                };
                if let Some(ch) = replacement {
                    out.push(ch);
                    i += semi + 1;
                    continue;
                }
            }
        }
        out.push(chars[i]);
        i += 1;
    }
    out
}

/// Normalizes a character removing combining marks (unicode.Mn) and decomposing
/// standard precomposed accented characters, mirroring Go's `norm.NFKD` decomposition.
fn normalize_char_nfkd_mn(ch: char) -> Option<char> {
    match ch {
        '\u{0300}'..='\u{036F}'
        | '\u{0483}'..='\u{0489}'
        | '\u{0591}'..='\u{05BD}'
        | '\u{05BF}'
        | '\u{05C1}'..='\u{05C2}'
        | '\u{05C4}'..='\u{05C5}'
        | '\u{05C7}'
        | '\u{0610}'..='\u{061A}'
        | '\u{064B}'..='\u{065F}'
        | '\u{0670}'
        | '\u{06D6}'..='\u{06DC}'
        | '\u{06DF}'..='\u{06E4}'
        | '\u{06E7}'..='\u{06E8}'
        | '\u{06EA}'..='\u{06ED}'
        | '\u{1AB0}'..='\u{1AFF}'
        | '\u{1DC0}'..='\u{1DFF}'
        | '\u{20D0}'..='\u{20FF}'
        | '\u{FE20}'..='\u{FE2F}' => return None,
        _ => {}
    }

    let mapped = match ch {
        'À'..='Å' | 'à'..='å' | 'Ā' | 'ā' | 'Ă' | 'ă' | 'Ą' | 'ą' => 'a',
        'Ç' | 'ç' | 'Ć' | 'ć' | 'Ĉ' | 'ĉ' | 'Ċ' | 'ċ' | 'Č' | 'č' => 'c',
        'Ď' | 'ď' | 'Đ' | 'đ' => 'd',
        'È'..='Ë' | 'è'..='ë' | 'Ē' | 'ē' | 'Ĕ' | 'ĕ' | 'Ė' | 'ė' | 'Ę' | 'ę' | 'Ě' | 'ě' => {
            'e'
        }
        'Ĝ' | 'ĝ' | 'Ğ' | 'ğ' | 'Ġ' | 'ġ' | 'Ģ' | 'ģ' => 'g',
        'Ĥ' | 'ĥ' | 'Ħ' | 'ħ' => 'h',
        'Ì'..='Ï' | 'ì'..='ï' | 'Ĩ' | 'ĩ' | 'Ī' | 'ī' | 'Ĭ' | 'ĭ' | 'Į' | 'į' | 'İ' | 'ı' => {
            'i'
        }
        'Ĵ' | 'ĵ' => 'j',
        'Ķ' | 'ķ' => 'k',
        'Ĺ' | 'ĺ' | 'Ļ' | 'ļ' | 'Ľ' | 'ľ' | 'Ŀ' | 'ŀ' | 'Ł' | 'ł' => 'l',
        'Ñ' | 'ñ' | 'Ń' | 'ń' | 'Ņ' | 'ņ' | 'Ň' | 'ň' | 'ŉ' => 'n',
        'Ò'..='Ö' | 'ò'..='ö' | 'Ō' | 'ō' | 'Ŏ' | 'ŏ' | 'Ő' | 'ő' => 'o',
        'Ŕ' | 'ŕ' | 'Ŗ' | 'ŗ' | 'Ř' | 'ř' => 'r',
        'Ś' | 'ś' | 'Ŝ' | 'ŝ' | 'Ş' | 'ş' | 'Š' | 'š' => 's',
        'Ţ' | 'ţ' | 'Ť' | 'ť' | 'Ŧ' | 'ŧ' => 't',
        'Ù'..='Ü'
        | 'ù'..='ü'
        | 'Ũ'
        | 'ũ'
        | 'Ū'
        | 'ū'
        | 'Ŭ'
        | 'ŭ'
        | 'Ů'
        | 'ů'
        | 'Ű'
        | 'ű'
        | 'Ų'
        | 'ų' => 'u',
        'Ŵ' | 'ŵ' => 'w',
        'Ý' | 'ý' | 'ÿ' | 'Ŷ' | 'ŷ' | 'Ÿ' => 'y',
        'Ź' | 'ź' | 'Ż' | 'ż' | 'Ž' | 'ž' => 'z',
        other => other,
    };
    Some(mapped)
}

/// Folds spaces and strips diacritics/non-alphanumerics, matching Go's `jioSaavnFoldSpaces`.
pub fn jiosaavn_fold_spaces(value: &str) -> String {
    let unescaped = html_unescape(value);
    let mut folded = String::with_capacity(unescaped.len());
    let mut space = true;
    for ch in unescaped.chars() {
        if let Some(r) = normalize_char_nfkd_mn(ch) {
            let r = r.to_lowercase().next().unwrap_or(r);
            if r.is_alphabetic() || r.is_numeric() {
                folded.push(r);
                space = false;
                continue;
            }
            if !space {
                folded.push(' ');
                space = true;
            }
        }
    }
    folded.trim().to_string()
}

/// Checks if an error represents download cancellation.
fn is_cancelled_error(err: &ProviderError) -> bool {
    let s = err.to_string().to_lowercase();
    s.contains("cancel") || s.contains("canceled") || s.contains("cancelled")
}

/// Decrypts a JioSaavn encrypted media URL using DES ECB mode with key `38346591`.
///
/// Port of Go's `decryptJioSaavnURL`.
pub fn decrypt_jiosaavn_url(enc: &str) -> Result<String> {
    let raw = BASE64
        .decode(enc.trim())
        .map_err(|e| ProviderError::Crypto(format!("base64 decode error: {e}")))?;

    if raw.len() % 8 != 0 {
        return Err(ProviderError::Crypto(
            "jiosaavn: encrypted url length".into(),
        ));
    }

    let cipher = Des::new_from_slice(JIOSAAVN_DES_KEY)
        .map_err(|e| ProviderError::Crypto(format!("des key error: {e}")))?;

    let mut out = raw.clone();
    for block in out.as_chunks_mut::<8>().0 {
        let block: &mut [u8] = block;
        let mut b = *cipher::generic_array::GenericArray::from_slice(block);
        cipher.decrypt_block(&mut b);
        block.copy_from_slice(&b);
    }

    let unpadded = pkcs7_unpad(&out);
    let decoded = String::from_utf8_lossy(unpadded);
    let mut decoded = decoded.trim().to_string();
    decoded = decoded.replace("http://", "https://");
    if decoded.is_empty() {
        return Err(ProviderError::Crypto(
            "jiosaavn: decrypted url empty".into(),
        ));
    }

    Ok(decoded)
}

/// Port of Go's `pkcs7Unpad`.
pub fn pkcs7_unpad(data: &[u8]) -> &[u8] {
    if data.is_empty() {
        return data;
    }
    let pad = data[data.len() - 1] as usize;
    if pad == 0 || pad > data.len() {
        return bytes_trim_null(data);
    }
    for &b in &data[data.len() - pad..] {
        if b as usize != pad {
            return bytes_trim_null(data);
        }
    }
    &data[..data.len() - pad]
}

/// Port of Go's `bytesTrimNull`.
pub fn bytes_trim_null(data: &[u8]) -> &[u8] {
    let mut i = data.len();
    while i > 0 && data[i - 1] == 0 {
        i -= 1;
    }
    &data[..i]
}

/// Port of Go's `GetFirstArtist` from `backend/filename.go`.
fn get_first_artist(artist_string: &str) -> &str {
    if artist_string.is_empty() {
        return "";
    }
    let lower = artist_string.to_lowercase();
    let delimiters = [";", " feat. ", " ft. ", " featuring "];
    for d in delimiters {
        if let Some(idx) = lower.find(d) {
            return artist_string[..idx].trim();
        }
    }
    artist_string
}

/// Generates search query variations for title and artist.
///
/// Port of Go's `jioSaavnQueries`.
pub fn jiosaavn_queries(title: &str, artist: &str) -> Vec<String> {
    let title = title.trim();
    let artist = get_first_artist(artist);
    let mut variants = vec![title.to_string()];

    let stripped1 = FEAT_RE.replace_all(title, " ");
    let stripped2 = BRACKETS_RE.replace_all(&stripped1, " ");
    let stripped = stripped2.split_whitespace().collect::<Vec<_>>().join(" ");
    if !stripped.is_empty() && !stripped.eq_ignore_ascii_case(title) {
        variants.push(stripped);
    }

    let mut queries = Vec::new();
    let mut seen = std::collections::HashSet::new();

    for v in variants {
        let with_artist = format!("{v} {artist}").trim().to_string();
        if !with_artist.is_empty() && seen.insert(with_artist.to_lowercase()) {
            queries.push(with_artist);
        }
        let v_trimmed = v.trim().to_string();
        if !v_trimmed.is_empty() && seen.insert(v_trimmed.to_lowercase()) {
            queries.push(v_trimmed);
        }
    }

    queries
}

/// Normalizes JioSaavn media URLs by upgrading to HTTPS and switching quality to 320kbps.
///
/// Port of Go's `normalizeJioSaavnMediaURL`.
pub fn normalize_jiosaavn_media_url(raw: &str) -> String {
    let mut normalized = raw.trim().replace("http://", "https://");
    for quality in ["12", "48", "96", "160"] {
        let pattern_mp4 = format!("_{quality}.mp4");
        normalized = normalized.replace(&pattern_mp4, "_320.mp4");
        let pattern_mp3 = format!("_{quality}.mp3");
        normalized = normalized.replace(&pattern_mp3, "_320.mp3");
        let pattern_m4a = format!("_{quality}.m4a");
        normalized = normalized.replace(&pattern_m4a, "_320.m4a");
    }
    normalized
}

/// Infers the audio file extension from stream URL.
///
/// Port of Go's `inferJioSaavnExt`.
pub fn infer_jiosaavn_ext(stream_url: &str) -> &'static str {
    let lower = stream_url.to_lowercase();
    if lower.contains(".m4a") {
        ".m4a"
    } else if lower.contains(".mp3") {
        ".mp3"
    } else {
        ".m4a"
    }
}

/// Determines whether a stream URL is a lossy audio format.
///
/// Port of Go's `looksLikeLossyURL`.
pub fn looks_like_lossy_url(stream_url: &str) -> bool {
    let lower = stream_url.to_lowercase();
    lower.contains(".m4a") || lower.contains(".mp3") || lower.contains(".mp4")
}

/// The caller's target request criteria for JioSaavn matching.
///
/// Port of Go's `jioSaavnWant`.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct JioSaavnWant {
    pub title: String,
    pub artist: String,
    pub album: String,
    pub isrc: String,
    pub duration: i32,
}

impl JioSaavnWant {
    pub fn new(
        title: impl Into<String>,
        artist: impl Into<String>,
        album: impl Into<String>,
        isrc: impl Into<String>,
        duration: i32,
    ) -> Self {
        Self {
            title: title.into(),
            artist: artist.into(),
            album: album.into(),
            isrc: isrc.into(),
            duration,
        }
    }
}

/// Extracts a string from a JSON object with HTML unescaping and number formatting.
///
/// Port of Go's `jioSaavnString`.
pub fn jiosaavn_string(item: &serde_json::Value, key: &str) -> String {
    let obj = match item {
        serde_json::Value::Object(map) => map,
        _ => return String::new(),
    };
    match obj.get(key) {
        Some(serde_json::Value::String(s)) => {
            let unescaped = html_unescape(s.trim());
            if !unescaped.is_empty() {
                return unescaped;
            }
            String::new()
        }
        Some(serde_json::Value::Number(n)) => {
            if let Some(i) = n.as_i64() {
                i.to_string()
            } else if let Some(u) = n.as_u64() {
                u.to_string()
            } else if let Some(f) = n.as_f64() {
                (f as i64).to_string()
            } else {
                n.to_string()
            }
        }
        _ => String::new(),
    }
}

/// Extracts the title/song name from a song item.
///
/// Port of Go's `jioSaavnItemTitle`.
pub fn jiosaavn_item_title(item: &serde_json::Value) -> String {
    for key in ["title", "name", "song"] {
        let val = jiosaavn_string(item, key);
        if !val.is_empty() {
            return val;
        }
    }
    String::new()
}

/// Appends a raw string after HTML unescaping and trimming.
///
/// Port of Go's `jioSaavnAppendRaw`.
pub fn jiosaavn_append_raw(dst: &mut Vec<String>, raw: &str) {
    let cleaned = html_unescape(raw).trim().to_string();
    if !cleaned.is_empty() {
        dst.push(cleaned);
    }
}

/// Splits description into artist and album parts.
///
/// Port of Go's `jioSaavnDescriptionParts`.
pub fn jiosaavn_description_parts(desc: &str) -> (String, String) {
    let desc = html_unescape(desc);
    let desc = desc.trim();
    if desc.is_empty() {
        return (String::new(), String::new());
    }
    for sep in [" · ", " • ", " ·", "•"] {
        if let Some(idx) = desc.find(sep) {
            if idx > 0 {
                return (
                    desc[..idx].trim().to_string(),
                    desc[idx + sep.len()..].trim().to_string(),
                );
            }
        }
    }
    if let Some(idx) = desc.find('·') {
        if idx > 0 {
            return (
                desc[..idx].trim().to_string(),
                desc[idx + '·'.len_utf8()..].trim().to_string(),
            );
        }
    }
    (String::new(), String::new())
}

/// Identifies metadata keys that are not artist names.
///
/// Port of Go's `jioSaavnIgnoredArtistKey`.
pub fn jiosaavn_ignored_artist_key(key: &str) -> bool {
    matches!(
        key.trim().to_lowercase().as_str(),
        "id" | "name"
            | "role"
            | "type"
            | "url"
            | "image"
            | "primary"
            | "featured"
            | "all"
            | "artists"
            | "singers"
            | "primary_artists"
            | "featured_artists"
            | "language"
            | "album"
            | "title"
            | "song"
    )
}

/// Checks whether a string represents a numeric ID.
///
/// Port of Go's `jioSaavnNumericID`.
pub fn jiosaavn_numeric_id(value: &str) -> bool {
    let value = value.trim();
    if value.is_empty() || value.len() > 16 {
        return false;
    }
    value.chars().all(|c| c.is_ascii_digit())
}

/// Collects artist names from strings, arrays, or objects.
///
/// Port of Go's `jioSaavnCollectNameList`.
pub fn jiosaavn_collect_name_list(dst: &mut Vec<String>, value: &serde_json::Value) {
    match value {
        serde_json::Value::String(s) => {
            jiosaavn_append_raw(dst, s);
        }
        serde_json::Value::Array(arr) => {
            for entry in arr {
                match entry {
                    serde_json::Value::String(s) => {
                        jiosaavn_append_raw(dst, s);
                    }
                    serde_json::Value::Object(_) => {
                        let name = jiosaavn_string(entry, "name");
                        if !name.is_empty() {
                            jiosaavn_append_raw(dst, &name);
                        }
                    }
                    _ => {}
                }
            }
        }
        serde_json::Value::Object(_) => {
            let name = jiosaavn_string(value, "name");
            if !name.is_empty() {
                jiosaavn_append_raw(dst, &name);
            }
        }
        _ => {}
    }
}

/// Collects primary and featured artists from nested structures.
///
/// Port of Go's `jioSaavnCollectNestedArtists`.
pub fn jiosaavn_collect_nested_artists(
    primary: &mut Vec<String>,
    extra: &mut Vec<String>,
    value: &serde_json::Value,
) {
    let obj = match value {
        serde_json::Value::Object(map) => map,
        _ => return,
    };
    if let Some(v) = obj.get("primary") {
        jiosaavn_collect_name_list(primary, v);
    }
    if let Some(v) = obj.get("primary_artists") {
        jiosaavn_collect_name_list(primary, v);
    }
    if let Some(v) = obj.get("featured") {
        jiosaavn_collect_name_list(extra, v);
    }
    if let Some(v) = obj.get("featured_artists") {
        jiosaavn_collect_name_list(extra, v);
    }
    if let Some(v) = obj.get("all") {
        jiosaavn_collect_name_list(extra, v);
    }
    if let Some(v) = obj.get("artists") {
        jiosaavn_collect_nested_artists(primary, extra, v);
    }
    if let Some(v) = obj.get("singers") {
        jiosaavn_collect_name_list(extra, v);
    }
}

/// Collects artists from an artist ID map.
///
/// Port of Go's `jioSaavnCollectArtistIDMap`.
pub fn jiosaavn_collect_artist_id_map(dst: &mut Vec<String>, value: &serde_json::Value) {
    let artists = match value {
        serde_json::Value::Object(map) => map,
        _ => return,
    };
    let mut extra = Vec::new();
    jiosaavn_collect_nested_artists(dst, &mut extra, value);
    dst.extend(extra);
    for (name, raw_id) in artists {
        let id = match raw_id {
            serde_json::Value::String(s) => s.as_str(),
            _ => continue,
        };
        if !jiosaavn_numeric_id(id) || jiosaavn_ignored_artist_key(name) {
            continue;
        }
        jiosaavn_append_raw(dst, name);
    }
}

/// Extracts primary and extra artist names from an item.
///
/// Port of Go's `jioSaavnArtistNames`.
pub fn jiosaavn_artist_names(item: &serde_json::Value) -> (Vec<String>, Vec<String>) {
    let mut primary = Vec::new();
    let mut extra = Vec::new();

    let obj = match item {
        serde_json::Value::Object(map) => map,
        _ => return (primary, extra),
    };

    if let Some(v) = obj.get("primaryArtists") {
        jiosaavn_collect_name_list(&mut primary, v);
    }
    if let Some(v) = obj.get("primary_artists") {
        jiosaavn_collect_name_list(&mut primary, v);
    }
    if let Some(v) = obj.get("singers") {
        jiosaavn_collect_name_list(&mut extra, v);
    }
    if let Some(v) = obj.get("featuredArtists") {
        jiosaavn_collect_name_list(&mut extra, v);
    }
    if let Some(v) = obj.get("featured_artists") {
        jiosaavn_collect_name_list(&mut extra, v);
    }
    if let Some(v) = obj.get("artist") {
        jiosaavn_collect_name_list(&mut extra, v);
    }

    let subtitle = jiosaavn_string(item, "subtitle");
    let item_title = jiosaavn_item_title(item);
    if !subtitle.is_empty() && !subtitle.eq_ignore_ascii_case(&item_title) {
        extra.push(subtitle);
    }

    let (desc_artist, _) = jiosaavn_description_parts(&jiosaavn_string(item, "description"));
    if !desc_artist.is_empty() {
        primary.push(desc_artist);
    }

    if let Some(serde_json::Value::Object(more_obj)) = obj.get("more_info") {
        if let Some(v) = more_obj.get("primary_artists") {
            jiosaavn_collect_name_list(&mut primary, v);
        }
        if let Some(v) = more_obj.get("primaryArtists") {
            jiosaavn_collect_name_list(&mut primary, v);
        }
        if let Some(v) = more_obj.get("singers") {
            jiosaavn_collect_name_list(&mut extra, v);
        }
        if let Some(v) = more_obj.get("featured_artists") {
            jiosaavn_collect_name_list(&mut extra, v);
        }
        if let Some(v) = more_obj.get("featuredArtists") {
            jiosaavn_collect_name_list(&mut extra, v);
        }
        if let Some(v) = more_obj.get("artists") {
            jiosaavn_collect_nested_artists(&mut primary, &mut extra, v);
        }
        if let Some(v) = more_obj.get("artistMap") {
            jiosaavn_collect_artist_id_map(&mut extra, v);
        }
    }

    if let Some(v) = obj.get("artists") {
        jiosaavn_collect_nested_artists(&mut primary, &mut extra, v);
    }
    if let Some(v) = obj.get("artistMap") {
        jiosaavn_collect_artist_id_map(&mut extra, v);
    }

    (primary, extra)
}

/// Splits credits into individual names.
///
/// Port of Go's `jioSaavnCredits`.
pub fn jiosaavn_credits(value: &str) -> Vec<String> {
    let unescaped = html_unescape(value);
    let trimmed = unescaped.trim();
    if trimmed.is_empty() {
        return Vec::new();
    }
    let parts = CREDIT_SPLIT_RE.split(trimmed);
    let mut credits = Vec::new();
    for part in parts {
        let part_trimmed = part.trim();
        if !part_trimmed.is_empty() {
            credits.push(part_trimmed.to_string());
        }
    }
    credits
}

/// Generates a match key dropping leading "the" and removing spaces.
///
/// Port of Go's `jioSaavnMatchKey`.
pub fn jiosaavn_match_key(value: &str) -> String {
    let folded = jiosaavn_fold_spaces(value);
    let mut fields: Vec<&str> = folded.split_whitespace().collect();
    if fields.len() > 1 && fields[0] == "the" {
        fields.remove(0);
    }
    fields.concat()
}

/// Builds set of matching keys for artist names.
///
/// Port of Go's `jioSaavnArtistKeys`.
pub fn jiosaavn_artist_keys(names: &[String]) -> std::collections::HashSet<String> {
    let mut keys = std::collections::HashSet::new();
    for name in names {
        let key = jiosaavn_match_key(name);
        if !key.is_empty() {
            keys.insert(key);
        }
        for credit in jiosaavn_credits(name) {
            let key = jiosaavn_match_key(&credit);
            if !key.is_empty() {
                keys.insert(key);
            }
        }
    }
    keys
}

/// Checks if key is present in keys set.
///
/// Port of Go's `jioSaavnKeyHit`.
pub fn jiosaavn_key_hit(keys: &std::collections::HashSet<String>, key: &str) -> bool {
    if key.is_empty() {
        return false;
    }
    keys.contains(key)
}

/// Checks inner version string against disallowed and allowed keywords.
///
/// Port of Go's `jioSaavnVersionInner`.
pub fn jiosaavn_version_inner(inner: &str) -> bool {
    let inner_lower = inner.trim().to_lowercase();
    if inner_lower.is_empty() {
        return false;
    }
    const BLOCKED: &[&str] = &[
        "karaoke",
        "tribute",
        "cover",
        "instrumental",
        "lullaby",
        "made famous",
        "style of",
        "originally performed",
    ];
    for blocked in BLOCKED {
        if inner_lower.contains(blocked) {
            return false;
        }
    }
    const ALLOWED: &[&str] = &[
        "remaster",
        "mix",
        "mono",
        "stereo",
        "deluxe",
        "radio",
        "edit",
        "version",
        "live",
        "bonus",
        "explicit",
        "clean",
        "anniversary",
        "expanded",
        "original",
    ];
    for allowed in ALLOWED {
        if inner_lower.contains(allowed) {
            return true;
        }
    }
    YEAR_RE.is_match(&inner_lower)
}

/// Strips version suffixes from track titles.
///
/// Port of Go's `jioSaavnStripVersion`.
pub fn jiosaavn_strip_version(title: &str) -> String {
    let unescaped = html_unescape(title);
    let feat_stripped = FEAT_RE.replace_all(&unescaped, " ");
    let mut title = feat_stripped
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ");

    for _ in 0..4 {
        if let Some(caps) = TRAILING_WRAP_RE.captures(&title) {
            let full_match = caps.get(0).unwrap();
            let inner_match = caps.get(1).unwrap();
            if jiosaavn_version_inner(inner_match.as_str()) {
                title = title[..full_match.start()].trim().to_string();
                continue;
            }
        }
        break;
    }

    if let Some(caps) = TRAILING_DASH_RE.captures(&title) {
        let full_match = caps.get(0).unwrap();
        let inner_match = caps.get(1).unwrap();
        if jiosaavn_version_inner(inner_match.as_str()) {
            title = title[..full_match.start()].trim().to_string();
        }
    }

    title.split_whitespace().collect::<Vec<_>>().join(" ")
}

/// Computes the exact title key with feat tags removed and spaces folded.
///
/// Port of Go's `jioSaavnExactTitleKey`.
pub fn jiosaavn_exact_title_key(title: &str) -> String {
    let unescaped = html_unescape(title);
    let feat_stripped = FEAT_RE.replace_all(&unescaped, " ");
    jiosaavn_fold_spaces(&feat_stripped).replace(' ', "")
}

/// Computes the base title key with versions stripped.
///
/// Port of Go's `jioSaavnBaseTitleKey`.
pub fn jiosaavn_base_title_key(title: &str) -> String {
    jiosaavn_fold_spaces(&jiosaavn_strip_version(title)).replace(' ', "")
}

/// Scores candidate item against requested title.
///
/// Port of Go's `jioSaavnTitleScore`.
pub fn jiosaavn_title_score(item: &serde_json::Value, want_title: &str) -> (i32, bool) {
    let got = jiosaavn_item_title(item);
    let want_title = want_title.trim();
    if want_title.is_empty() || got.trim().is_empty() {
        return (0, false);
    }
    if jiosaavn_exact_title_key(want_title) == jiosaavn_exact_title_key(&got) {
        return (1000, true);
    }
    let want_base = jiosaavn_base_title_key(want_title);
    let got_base = jiosaavn_base_title_key(&got);
    if !want_base.is_empty() && want_base == got_base {
        return (700, true);
    }
    (0, false)
}

/// Scores candidate item against requested artist.
///
/// Port of Go's `jioSaavnArtistScore`.
pub fn jiosaavn_artist_score(item: &serde_json::Value, want_artist: &str) -> (i32, bool) {
    let want_artist = want_artist.trim();
    if want_artist.is_empty() {
        return (0, true);
    }
    let (primary, extra) = jiosaavn_artist_names(item);
    let mut all = Vec::with_capacity(primary.len() + extra.len());
    all.extend_from_slice(&primary);
    all.extend_from_slice(&extra);
    if all.is_empty() {
        return (0, false);
    }
    let all_keys = jiosaavn_artist_keys(&all);
    let mut primary_keys = jiosaavn_artist_keys(&primary);
    if primary_keys.is_empty() {
        primary_keys = all_keys.clone();
    }
    let want_key = jiosaavn_match_key(want_artist);
    if jiosaavn_key_hit(&all_keys, &want_key) && jiosaavn_key_hit(&primary_keys, &want_key) {
        return (500, true);
    }
    let credits = jiosaavn_credits(want_artist);
    if credits.is_empty() {
        return (0, false);
    }
    let mut matched = 0;
    for credit in &credits {
        if jiosaavn_key_hit(&all_keys, &jiosaavn_match_key(credit)) {
            matched += 1;
        }
    }
    let first = jiosaavn_match_key(&credits[0]);
    if matched == credits.len() && jiosaavn_key_hit(&primary_keys, &first) {
        return (480, true);
    }
    if credits.len() == 1 && jiosaavn_key_hit(&primary_keys, &first) {
        return (500, true);
    }
    (0, false)
}

/// Checks whether the candidate item contains disallowed variant keywords.
///
/// Port of Go's `jioSaavnDisallowedVariant`.
pub fn jiosaavn_disallowed_variant(item: &serde_json::Value, want: &JioSaavnWant) -> bool {
    let (primary, extra) = jiosaavn_artist_names(item);
    let blob = format!(
        "{} {} {}",
        jiosaavn_item_title(item),
        primary.join(" "),
        extra.join(" ")
    )
    .to_lowercase();
    let requested = format!("{} {}", want.title, want.artist).to_lowercase();
    const PHRASES: &[&str] = &[
        "karaoke",
        "tribute",
        "instrumental",
        "lullaby",
        "as made famous",
        "in the style of",
        "originally performed",
    ];
    for phrase in PHRASES {
        if blob.contains(phrase) && !requested.contains(phrase) {
            return true;
        }
    }
    false
}

/// Extracts ISRC from candidate item.
///
/// Port of Go's `jioSaavnItemISRC`.
pub fn jiosaavn_item_isrc(item: &serde_json::Value) -> String {
    for key in ["isrc", "ISRC"] {
        let val = jiosaavn_string(item, key).trim().to_uppercase();
        if !val.is_empty() {
            return val;
        }
    }
    if let Some(more) = item.get("more_info").and_then(|v| v.as_object()) {
        for key in ["isrc", "ISRC"] {
            if let Some(val_str) = more.get(key) {
                let s = match val_str {
                    serde_json::Value::String(s) => s.trim().to_uppercase(),
                    _ => String::new(),
                };
                if !s.is_empty() {
                    return s;
                }
            }
        }
    }
    String::new()
}

/// Checks whether item ISRC matches wanted ISRC.
///
/// Port of Go's `jioSaavnISRCMatches`.
pub fn jiosaavn_isrc_matches(item: &serde_json::Value, want: &str) -> bool {
    let want = want.trim().to_uppercase();
    let got = jiosaavn_item_isrc(item);
    !want.is_empty() && !got.is_empty() && want == got
}

/// Scores candidate item based on ISRC.
///
/// Port of Go's `jioSaavnISRCScore`.
pub fn jiosaavn_isrc_score(item: &serde_json::Value, want: &str) -> i32 {
    let want = want.trim().to_uppercase();
    if want.is_empty() {
        return 0;
    }
    let got = jiosaavn_item_isrc(item);
    if got.is_empty() {
        return 0;
    }
    if got == want {
        800
    } else {
        -80
    }
}

/// Parses duration in seconds from raw duration string.
///
/// Port of Go's `jioSaavnParseDuration`.
pub fn jiosaavn_parse_duration(raw: &str) -> i32 {
    let raw = raw.trim();
    if raw.is_empty() || raw == "0" {
        return 0;
    }
    if raw.contains(':') {
        let parts: Vec<&str> = raw.split(':').collect();
        if parts.len() < 2 || parts.len() > 3 {
            return 0;
        }
        let mut total = 0;
        for part in parts {
            match part.trim().parse::<i32>() {
                Ok(n) if n >= 0 => total = total * 60 + n,
                _ => return 0,
            }
        }
        return total;
    }
    if let Ok(mut seconds) = raw.parse::<f64>() {
        if seconds <= 0.0 {
            return 0;
        }
        if seconds > 10000.0 {
            seconds /= 1000.0;
        }
        return (seconds + 0.5) as i32;
    }
    0
}

/// Extracts duration in seconds from candidate item.
///
/// Port of Go's `jioSaavnDurationSeconds`.
pub fn jiosaavn_duration_seconds(item: &serde_json::Value) -> i32 {
    for key in ["duration", "duration_seconds", "playtime"] {
        let secs = jiosaavn_parse_duration(&jiosaavn_string(item, key));
        if secs > 0 {
            return secs;
        }
    }
    if let Some(more) = item.get("more_info") {
        for key in ["duration", "duration_seconds", "playtime"] {
            let secs = jiosaavn_parse_duration(&jiosaavn_string(more, key));
            if secs > 0 {
                return secs;
            }
        }
    }
    0
}

/// Scores candidate item based on duration difference.
///
/// Port of Go's `jioSaavnDurationScore`.
pub fn jiosaavn_duration_score(item: &serde_json::Value, want: i32) -> i32 {
    if want <= 0 {
        return 0;
    }
    let got = jiosaavn_duration_seconds(item);
    if got <= 0 {
        return 0;
    }
    let diff = (got - want).abs();
    match diff {
        0..=3 => 220,
        4..=10 => 80,
        11..=19 => 0,
        _ => -160,
    }
}

/// Extracts album name from candidate item.
///
/// Port of Go's `jioSaavnItemAlbum`.
pub fn jiosaavn_item_album(item: &serde_json::Value) -> String {
    if let Some(album) = item.get("album").filter(|v| v.is_object()) {
        let name = jiosaavn_string(album, "name");
        if !name.is_empty() {
            return name;
        }
        let title = jiosaavn_string(album, "title");
        if !title.is_empty() {
            return title;
        }
    }
    let name = jiosaavn_string(item, "album");
    if !name.is_empty() {
        return name;
    }
    if let Some(more) = item.get("more_info") {
        let name = jiosaavn_string(more, "album");
        if !name.is_empty() {
            return name;
        }
    }
    let (_, album) = jiosaavn_description_parts(&jiosaavn_string(item, "description"));
    if !album.is_empty() {
        return album;
    }
    String::new()
}

/// Scores candidate item based on album match.
///
/// Port of Go's `jioSaavnAlbumScore`.
pub fn jiosaavn_album_score(item: &serde_json::Value, want_album: &str) -> i32 {
    let want_key = jiosaavn_base_title_key(want_album);
    if want_key.is_empty() {
        return 0;
    }
    if jiosaavn_base_title_key(&jiosaavn_item_album(item)) == want_key {
        return 60;
    }
    0
}

/// Computes the overall matching score for candidate item.
///
/// Port of Go's `jioSaavnScore`.
pub fn jiosaavn_score(item: &serde_json::Value, want: &JioSaavnWant) -> i32 {
    let (title_score, title_ok) = jiosaavn_title_score(item, &want.title);
    if !title_ok {
        return 0;
    }
    let (artist_score, artist_ok) = jiosaavn_artist_score(item, &want.artist);
    if !want.artist.trim().is_empty() && !artist_ok {
        return 0;
    }
    if jiosaavn_disallowed_variant(item, want) && !jiosaavn_isrc_matches(item, &want.isrc) {
        return 0;
    }
    let mut score = title_score + artist_score;
    score += jiosaavn_isrc_score(item, &want.isrc);
    score += jiosaavn_duration_score(item, want.duration);
    score += jiosaavn_album_score(item, &want.album);
    if score < 1 {
        return 0;
    }
    score
}

/// Picks the best matching item from a candidate list.
///
/// Port of Go's `jioSaavnPickBest`.
pub fn jiosaavn_pick_best<'a>(
    items: &'a [serde_json::Value],
    want: &JioSaavnWant,
) -> (Option<&'a serde_json::Value>, i32) {
    if want.title.trim().is_empty() {
        return (None, 0);
    }
    let mut best = None;
    let mut best_score = 0;
    for item in items {
        let score = jiosaavn_score(item, want);
        if score > best_score {
            best_score = score;
            best = Some(item);
        }
    }
    (best, best_score)
}

/// Picks the best song ID and URL from a candidate list.
///
/// Port of Go's `jioSaavnResultFromList`.
pub fn jiosaavn_result_from_list(
    items: &[serde_json::Value],
    want: &JioSaavnWant,
) -> Result<(String, String)> {
    let (best, score) = jiosaavn_pick_best(items, want);
    if best.is_none() || score <= 0 {
        return Err(ProviderError::Other("jiosaavn: no close match".into()));
    }
    let best = best.unwrap();
    let id = jiosaavn_string(best, "id");
    if id.is_empty() {
        return Err(ProviderError::Other("jiosaavn: missing song id".into()));
    }
    let song_url = format!("https://www.jiosaavn.com/song/{id}");
    Ok((id, song_url))
}

/// Extracts a song object from a song details payload.
///
/// Port of Go's `jioSaavnExtractSong`.
pub fn jiosaavn_extract_song<'a>(
    payload: &'a serde_json::Value,
    song_id: &str,
) -> Option<&'a serde_json::Value> {
    if let Some(direct) = payload.get(song_id).filter(|v| v.is_object()) {
        return Some(direct);
    }
    if let Some(first) = payload
        .get("songs")
        .and_then(|v| v.as_array())
        .and_then(|songs| songs.first())
        .filter(|v| v.is_object())
    {
        return Some(first);
    }
    None
}

/// Go uses a plain `&http.Client{Timeout: ...}` here (default redirect policy:
/// follow up to 10, any host), not the same-host signed client.
fn jiosaavn_http_client(timeout: std::time::Duration) -> Result<reqwest::Client> {
    Ok(reqwest::Client::builder().timeout(timeout).build()?)
}

/// Internal implementation of `jioSaavnOfficialSearch` allowing base URL override.
pub async fn jiosaavn_official_search_with_base_url(
    query: &str,
    want: &JioSaavnWant,
    api_url: &str,
) -> Result<(String, String)> {
    let client = jiosaavn_http_client(std::time::Duration::from_secs(12))?;
    let mut url = url::Url::parse(api_url).map_err(|e| ProviderError::Parse(e.to_string()))?;
    url.query_pairs_mut()
        .append_pair("__call", "autocomplete.get")
        .append_pair("_format", "json")
        .append_pair("_marker", "0")
        .append_pair("cc", "in")
        .append_pair("includeMetaTags", "1")
        .append_pair("query", query);

    let req = crate::http_headers::with_default_headers(client.get(url))?;
    let resp = client.execute(req).await?;
    let status = resp.status();
    if status.as_u16() != 200 {
        return Err(ProviderError::Other(format!(
            "jiosaavn search HTTP {}",
            status.as_u16()
        )));
    }
    let body = crate::bounded_body::read_bounded_response(
        resp,
        crate::bounded_body::MAX_PROVIDER_JSON_BYTES,
    )
    .await?;

    let payload: serde_json::Value = serde_json::from_slice(&body)?;
    let items = payload
        .get("songs")
        .and_then(|s| s.get("data"))
        .and_then(|d| d.as_array());

    let items = match items {
        Some(items) if !items.is_empty() => items,
        _ => {
            return Err(ProviderError::Other(
                "jiosaavn: empty official results".into(),
            ))
        }
    };

    jiosaavn_result_from_list(items, want)
}

/// Official search for candidate tracks.
///
/// Port of Go's `jioSaavnOfficialSearch`.
pub async fn jiosaavn_official_search(
    query: &str,
    want: &JioSaavnWant,
) -> Result<(String, String)> {
    jiosaavn_official_search_with_base_url(query, want, JIOSAAVN_OFFICIAL_API).await
}

/// Internal implementation of `jioSaavnOfficialStreamURL` allowing base URL override.
pub async fn jiosaavn_official_stream_url_with_base_url(
    song_id: &str,
    api_url: &str,
) -> Result<String> {
    let client = jiosaavn_http_client(std::time::Duration::from_secs(12))?;
    let mut url = url::Url::parse(api_url).map_err(|e| ProviderError::Parse(e.to_string()))?;
    url.query_pairs_mut()
        // Same order as Go's url.Values.Encode (sorted by key).
        .append_pair("__call", "song.getDetails")
        .append_pair("_format", "json")
        .append_pair("_marker", "0")
        .append_pair("cc", "in")
        .append_pair("pids", song_id);

    let req = crate::http_headers::with_default_headers(client.get(url))?;
    let resp = client.execute(req).await?;
    let body = crate::bounded_body::read_bounded_response(
        resp,
        crate::bounded_body::MAX_PROVIDER_JSON_BYTES,
    )
    .await?;

    let payload: serde_json::Value = serde_json::from_slice(&body)?;
    let song = jiosaavn_extract_song(&payload, song_id)
        .ok_or_else(|| ProviderError::Other("jiosaavn: song details missing".into()))?;

    let media = jiosaavn_string(song, "media_url");
    if !media.is_empty() {
        return Ok(normalize_jiosaavn_media_url(&media));
    }

    let mut enc = jiosaavn_string(song, "encrypted_media_url");
    if enc.is_empty() {
        enc = jiosaavn_string(song, "encrypted_drm_media_url");
    }
    if enc.is_empty() {
        return Err(ProviderError::Other("jiosaavn: no media url".into()));
    }

    let decrypted = decrypt_jiosaavn_url(&enc)?;
    Ok(normalize_jiosaavn_media_url(&decrypted))
}

/// Resolves the stream URL for a given song ID.
///
/// Port of Go's `jioSaavnOfficialStreamURL`.
pub async fn jiosaavn_official_stream_url(song_id: &str) -> Result<String> {
    jiosaavn_official_stream_url_with_base_url(song_id, JIOSAAVN_OFFICIAL_API).await
}

/// Internal health check implementation with base URL override.
pub async fn jiosaavn_health_with_base_url(api_url: &str) -> bool {
    let client = match jiosaavn_http_client(std::time::Duration::from_secs(8)) {
        Ok(c) => c,
        Err(_) => return false,
    };
    let mut url = match url::Url::parse(api_url) {
        Ok(u) => u,
        Err(_) => return false,
    };
    url.query_pairs_mut()
        .append_pair("__call", "autocomplete.get")
        .append_pair("_format", "json")
        .append_pair("query", "test");

    let req = match crate::http_headers::with_default_headers(client.get(url)) {
        Ok(r) => r,
        Err(_) => return false,
    };
    match client.execute(req).await {
        Ok(resp) => resp.status().as_u16() == 200,
        Err(_) => false,
    }
}

/// Checks the operational health of the official JioSaavn API endpoint.
///
/// Port of Go's `JioSaavnHealth`.
pub async fn jiosaavn_health() -> bool {
    jiosaavn_health_with_base_url(JIOSAAVN_OFFICIAL_API).await
}

/// Search JioSaavn iterating queries until a match is found.
///
/// Port of Go's `searchJioSaavn`.
pub async fn search_jiosaavn(
    want: &JioSaavnWant,
    cancelled: &dyn Fn() -> Result<()>,
) -> Result<(String, String)> {
    search_jiosaavn_with_base_url(want, cancelled, JIOSAAVN_OFFICIAL_API).await
}

/// Search JioSaavn with a custom API base URL.
pub async fn search_jiosaavn_with_base_url(
    want: &JioSaavnWant,
    cancelled: &dyn Fn() -> Result<()>,
    api_url: &str,
) -> Result<(String, String)> {
    cancelled()?;
    let queries = jiosaavn_queries(&want.title, &want.artist);
    let mut last_err = None;
    for query in queries {
        cancelled()?;
        match jiosaavn_official_search_with_base_url(&query, want, api_url).await {
            Ok((id, src)) if !id.is_empty() => return Ok((id, src)),
            Err(search_err) if is_cancelled_error(&search_err) => return Err(search_err),
            Err(search_err) => {
                last_err = Some(search_err);
            }
            _ => {}
        }
    }
    let err = last_err.unwrap_or_else(|| ProviderError::Other("jiosaavn: no match".into()));
    Err(err)
}

/// Resolves search and official stream URL for a requested track.
///
/// Port of Go's `downloadJioSaavnTrack` search + stream URL resolution step.
pub async fn resolve_jiosaavn_track(
    want: &JioSaavnWant,
    cancelled: &dyn Fn() -> Result<()>,
) -> Result<(String, String)> {
    resolve_jiosaavn_track_with_base_url(want, cancelled, JIOSAAVN_OFFICIAL_API).await
}

/// Resolves search and official stream URL with a custom base URL.
pub async fn resolve_jiosaavn_track_with_base_url(
    want: &JioSaavnWant,
    cancelled: &dyn Fn() -> Result<()>,
    api_url: &str,
) -> Result<(String, String)> {
    let (song_id, source_url) = search_jiosaavn_with_base_url(want, cancelled, api_url).await?;
    let stream_url = jiosaavn_official_stream_url_with_base_url(&song_id, api_url).await?;
    Ok((stream_url, source_url))
}

/// Replaces the file extension in a file path, matching Go's `replaceAudioExtension`.
pub fn replace_audio_extension(path: &str, ext: &str) -> String {
    if ext.is_empty() {
        return path.to_string();
    }
    let ext_with_dot = if ext.starts_with('.') {
        ext.to_string()
    } else {
        format!(".{ext}")
    };
    let current = go_filepath_ext(path);
    if current.is_empty() {
        return format!("{path}{ext_with_dot}");
    }
    if let Some(stripped) = path.strip_suffix(current) {
        format!("{stripped}{ext_with_dot}")
    } else {
        format!("{path}{ext_with_dot}")
    }
}

/// Extracts file extension matching Go's `filepath.Ext`.
fn go_filepath_ext(path: &str) -> &str {
    let bytes = path.as_bytes();
    for i in (0..bytes.len()).rev() {
        let b = bytes[i];
        if b == b'/' || b == b'\\' {
            break;
        }
        if b == b'.' {
            return &path[i..];
        }
    }
    ""
}

/// Post-download .flac -> lossy-extension rename logic.
///
/// Port of Go's `downloadJioSaavnTrack` post-download rename step.
pub fn finalize_jiosaavn_path(path: impl AsRef<Path>, stream_url: &str) -> PathBuf {
    let path = path.as_ref();
    let path_str = path.to_string_lossy();
    let current_ext = go_filepath_ext(&path_str);
    if current_ext.eq_ignore_ascii_case(".flac") && looks_like_lossy_url(stream_url) {
        let target_ext = infer_jiosaavn_ext(stream_url);
        let renamed_str = replace_audio_extension(&path_str, target_ext);
        if renamed_str != path_str {
            let renamed_path = PathBuf::from(&renamed_str);
            if std::fs::rename(path, &renamed_path).is_ok() {
                return renamed_path;
            }
        }
    }
    path.to_path_buf()
}

#[derive(Debug, Deserialize)]
pub struct JioSaavnSongDetails {
    pub id: Option<String>,
    pub song: Option<String>,
    pub primary_artists: Option<String>,
    pub media_url: Option<String>,
    pub encrypted_media_url: Option<String>,
    pub encrypted_drm_media_url: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;
    use cipher::BlockEncrypt;

    fn come_together_fixture() -> Vec<serde_json::Value> {
        vec![
            serde_json::json!({
                "id": "WYD9hPrJ",
                "title": "Come Together",
                "subtitle": "",
                "description": "Milovan, Anna · Live At the Iron Horse",
                "more_info": {
                    "primary_artists": "Milovan, Anna",
                    "singers": "Milovan, Anna"
                }
            }),
            serde_json::json!({
                "id": "bs5iaXrI",
                "title": "Come Together",
                "description": "The Shiny Darks · 50 Years Of Aerosmith - A 2020s Tribute",
                "more_info": {
                    "primary_artists": "The Shiny Darks",
                    "singers": "The Shiny Darks"
                }
            }),
            serde_json::json!({
                "id": "buPhYncP",
                "title": "Come Together (Remastered 2009)",
                "subtitle": "",
                "album": "Abbey Road (Remastered)",
                "description": "The Beatles · Abbey Road (Remastered)",
                "more_info": {
                    "primary_artists": "The Beatles",
                    "singers": ""
                }
            }),
            serde_json::json!({
                "id": "VaTL2kp-",
                "title": "Come Together (2019 Mix)",
                "description": "The Beatles · The Beatles 1967 - 1970 (2023 Edition)",
                "more_info": {
                    "primary_artists": "The Beatles",
                    "singers": ""
                }
            }),
        ]
    }

    #[test]
    fn test_jiosaavn_queries_variations() {
        let q = jiosaavn_queries("Blinding Lights (feat. Someone)", "The Weeknd, Daft Punk");
        assert!(q.iter().any(|s| s.contains("The Weeknd")));
        assert!(q.iter().any(|s| s == "Blinding Lights"));
    }

    #[test]
    fn test_decrypt_jiosaavn_url_round_trip() {
        let original_url = "https://aac.saavncdn.com/123/sample_320.mp4";
        let mut padded = original_url.as_bytes().to_vec();
        let pad_len = 8 - (padded.len() % 8);
        padded.extend(vec![pad_len as u8; pad_len]);

        let cipher = Des::new_from_slice(JIOSAAVN_DES_KEY).unwrap();
        let mut encrypted = padded.clone();
        for block in encrypted.as_chunks_mut::<8>().0 {
            let block: &mut [u8] = block;
            let mut b = *cipher::generic_array::GenericArray::from_slice(block);
            cipher.encrypt_block(&mut b);
            block.copy_from_slice(&b);
        }

        let b64 = BASE64.encode(&encrypted);
        let decrypted = decrypt_jiosaavn_url(&b64).unwrap();
        assert_eq!(decrypted, original_url);
    }

    #[test]
    fn test_jio_saavn_pick_best_prefers_close_title_artist() {
        let items = vec![
            serde_json::json!({
                "id": "junk",
                "title": "Jee Karda",
                "primaryArtists": "Garry Sandhu"
            }),
            serde_json::json!({
                "id": "wanted",
                "title": "On Time",
                "primaryArtists": "Huntrix",
                "singers": "EJAE"
            }),
        ];

        let want = JioSaavnWant {
            title: "On Time".into(),
            artist: "Huntrix, EJAE".into(),
            ..Default::default()
        };
        let (got, score) = jiosaavn_pick_best(&items, &want);
        assert!(score > 0, "score = {score}, want a close match");
        let got = got.expect("should pick an item");
        assert_eq!(
            jiosaavn_string(got, "id"),
            "wanted",
            "picked {got:?}, want the close title/artist over Data[0] junk"
        );
    }

    #[test]
    fn test_jio_saavn_original_artist_beats_exact_title_cover() {
        let items = come_together_fixture();
        let want = JioSaavnWant {
            title: "Come Together".into(),
            artist: "The Beatles".into(),
            ..Default::default()
        };
        let (got, score) = jiosaavn_pick_best(&items, &want);
        assert!(
            score > 0 && got.is_some(),
            "expected a Beatles match, got no selection"
        );
        let got = got.unwrap();
        assert_eq!(
            jiosaavn_string(got, "id"),
            "buPhYncP",
            "picked {} ({}), want Beatles id buPhYncP",
            jiosaavn_string(got, "id"),
            jiosaavn_item_title(got)
        );
    }

    #[test]
    fn test_jio_saavn_title_only_fallback_still_uses_requested_artist() {
        let items = come_together_fixture();
        let want = JioSaavnWant {
            title: "Come Together".into(),
            artist: "The Beatles".into(),
            ..Default::default()
        };
        let (id, _) = jiosaavn_result_from_list(&items, &want).expect("should resolve");
        assert_eq!(
            id, "buPhYncP",
            "title-only request selected {id}, want buPhYncP"
        );

        let cover_only = vec![items[0].clone()];
        let (got, score) = jiosaavn_pick_best(&cover_only, &want);
        assert!(
            score == 0 && got.is_none(),
            "title-only result list still selected cover {got:?} score {score}"
        );
    }

    #[test]
    fn test_jio_saavn_wrong_artist_only_is_no_match() {
        let fixture = come_together_fixture();
        let items = vec![fixture[0].clone(), fixture[1].clone()];
        let want = JioSaavnWant {
            title: "Come Together".into(),
            artist: "The Beatles".into(),
            ..Default::default()
        };
        let (got, score) = jiosaavn_pick_best(&items, &want);
        assert!(
            score == 0 && got.is_none(),
            "selected {got:?} score {score}, want no match"
        );
        assert!(
            jiosaavn_result_from_list(&items, &want).is_err(),
            "expected no close match error"
        );
    }

    #[test]
    fn test_jio_saavn_unrelated_title_is_no_match() {
        let items = vec![
            serde_json::json!({
                "id": "sun",
                "title": "Here Comes the Sun (Remastered 2009)",
                "description": "The Beatles · Abbey Road (Remastered)",
                "more_info": { "primary_artists": "The Beatles", "singers": "" }
            }),
            serde_json::json!({
                "id": "now",
                "title": "Come Together Now",
                "description": "The Beatles · Let It Be",
                "more_info": { "primary_artists": "The Beatles" }
            }),
            serde_json::json!({
                "id": "karaoke",
                "title": "Come Together (Karaoke Version)",
                "more_info": { "primary_artists": "The Beatles" }
            }),
        ];
        let want = JioSaavnWant {
            title: "Come Together".into(),
            artist: "The Beatles".into(),
            ..Default::default()
        };
        let (got, score) = jiosaavn_pick_best(&items, &want);
        assert!(
            score == 0 && got.is_none(),
            "selected {got:?} score {score}, want no match"
        );
    }

    #[test]
    fn test_jio_saavn_version_fallback_matches_either_side() {
        let remaster = serde_json::json!({
            "id": "remaster",
            "title": "Come Together (Remastered 2009)",
            "description": "The Beatles · Abbey Road (Remastered)",
            "more_info": { "primary_artists": "The Beatles", "singers": "" }
        });
        let plain = serde_json::json!({
            "id": "plain",
            "title": "Come Together",
            "description": "The Beatles · Abbey Road",
            "more_info": { "primary_artists": "The Beatles" }
        });

        let candidates_remaster = [remaster.clone()];
        let (_, score) = jiosaavn_pick_best(
            &candidates_remaster,
            &JioSaavnWant {
                title: "Come Together".into(),
                artist: "The Beatles".into(),
                ..Default::default()
            },
        );
        assert!(score > 0, "versioned title should match the plain request");

        let candidates_plain = [plain.clone()];
        let (_, score) = jiosaavn_pick_best(
            &candidates_plain,
            &JioSaavnWant {
                title: "Come Together (Remastered 2009)".into(),
                artist: "Beatles".into(),
                ..Default::default()
            },
        );
        assert!(
            score > 0,
            "plain title should match a versioned request, including a leading The"
        );

        let candidates = [plain, remaster];
        let (got, _) = jiosaavn_pick_best(
            &candidates,
            &JioSaavnWant {
                title: "Come Together (Remastered 2009)".into(),
                artist: "The Beatles".into(),
                ..Default::default()
            },
        );
        let got = got.expect("should pick an item");
        assert_eq!(
            jiosaavn_string(got, "id"),
            "remaster",
            "exact version lost to {}",
            jiosaavn_string(got, "id")
        );
    }

    #[test]
    fn test_jio_saavn_unicode_collaboration_artists() {
        let items = vec![
            serde_json::json!({
                "id": "cover",
                "title": "Crazy in Love",
                "description": "Someone Else · Tributes",
                "more_info": { "primary_artists": "Someone Else", "singers": "Someone Else" }
            }),
            serde_json::json!({
                "id": "duet",
                "title": "Crazy in Love",
                "artists": {
                    "primary": [{ "id": "1", "name": "Beyoncé", "role": "primary_artists" }],
                    "featured": [{ "id": "2", "name": "JAY-Z", "role": "featured_artists" }]
                }
            }),
        ];

        for artist in ["Beyonce & Jay-Z", "Beyoncé feat. JAY-Z", "Beyoncé"] {
            let want = JioSaavnWant {
                title: "Crazy in Love".into(),
                artist: artist.into(),
                ..Default::default()
            };
            let (got, score) = jiosaavn_pick_best(&items, &want);
            let id = got.map(|g| jiosaavn_string(g, "id")).unwrap_or_default();
            assert!(
                score > 0 && id == "duet",
                "artist {artist:?} picked {id:?} score {score}, want duet"
            );
        }

        let want_someone_else = JioSaavnWant {
            title: "Crazy in Love".into(),
            artist: "Someone Else".into(),
            ..Default::default()
        };
        let (got, score) = jiosaavn_pick_best(&items, &want_someone_else);
        let id = got.map(|g| jiosaavn_string(g, "id")).unwrap_or_default();
        assert!(
            score > 0 && id == "cover",
            "collaboration track should not steal a different requested artist: {got:?}"
        );
    }

    #[test]
    fn test_jio_saavn_community_nested_artists() {
        let items = vec![
            serde_json::json!({
                "id": "cover",
                "name": "Come Together",
                "album": { "name": "Live At the Iron Horse" },
                "artists": {
                    "primary": [{ "name": "Milovan" }, { "name": "Anna" }],
                    "all": [{ "name": "Milovan" }, { "name": "Anna" }]
                }
            }),
            serde_json::json!({
                "id": "nested",
                "name": "Come Together (Remastered 2009)",
                "duration": "258",
                "album": { "id": "9", "name": "Abbey Road (Remastered)" },
                "artists": {
                    "primary": [{ "id": "624737", "name": "The Beatles", "role": "primary_artists", "type": "artist" }],
                    "all": [{ "name": "The Beatles" }]
                },
                "artistMap": {
                    "primary_artists": [{ "name": "The Beatles" }],
                    "artists": [{ "name": "The Beatles" }]
                }
            }),
        ];

        let want = JioSaavnWant {
            title: "Come Together".into(),
            artist: "The Beatles".into(),
            album: "Abbey Road".into(),
            duration: 259,
            ..Default::default()
        };
        let (got, score) = jiosaavn_pick_best(&items, &want);
        let id = got.map(|g| jiosaavn_string(g, "id")).unwrap_or_default();
        assert!(
            score > 0 && id == "nested",
            "picked {id:?} score {score}, want nested Beatles recording"
        );
    }

    #[test]
    fn test_jio_saavn_description_only_artist_still_matches() {
        let items = vec![
            serde_json::json!({
                "id": "cover",
                "title": "Come Together",
                "description": "Milovan, Anna · Live At the Iron Horse"
            }),
            serde_json::json!({
                "id": "desc",
                "title": "Come Together (2019 Mix)",
                "subtitle": "",
                "description": "The Beatles · The Beatles 1967 - 1970 (2023 Edition)"
            }),
        ];

        let want = JioSaavnWant {
            title: "Come Together".into(),
            artist: "The Beatles".into(),
            ..Default::default()
        };
        let (got, score) = jiosaavn_pick_best(&items, &want);
        let id = got.map(|g| jiosaavn_string(g, "id")).unwrap_or_default();
        assert!(
            score > 0 && id == "desc",
            "description credit was ignored, picked {id:?} score {score}"
        );
    }

    #[test]
    fn test_jio_saavn_duration_and_isrc_are_evidence() {
        let remaster = serde_json::json!({
            "id": "buPhYncP",
            "title": "Come Together (Remastered 2009)",
            "album": "Abbey Road (Remastered)",
            "duration": "258",
            "isrc": "GBAYE0601698",
            "description": "The Beatles · Abbey Road (Remastered)",
            "more_info": { "primary_artists": "The Beatles" }
        });
        let mix = serde_json::json!({
            "id": "VaTL2kp-",
            "title": "Come Together (2019 Mix)",
            "album": "The Beatles 1967 - 1970 (2023 Edition)",
            "duration": 249,
            "isrc": "OTHERISRC0001",
            "description": "The Beatles · The Beatles 1967 - 1970 (2023 Edition)",
            "more_info": { "primary_artists": "The Beatles" }
        });
        let mut cover = come_together_fixture()[0].clone();
        cover["duration"] = serde_json::json!("187");

        let items = vec![mix.clone(), cover, remaster.clone()];
        let want = JioSaavnWant {
            title: "Come Together".into(),
            artist: "The Beatles".into(),
            duration: 259,
            isrc: "gbaye0601698".into(),
            album: "Abbey Road".into(),
        };
        let (got, score) = jiosaavn_pick_best(&items, &want);
        let id = got.map(|g| jiosaavn_string(g, "id")).unwrap_or_default();
        assert!(
            score > 0 && id == "buPhYncP",
            "duration/ISRC/album evidence picked {id:?} score {score}"
        );

        let want_conflicting_isrc = JioSaavnWant {
            title: "Come Together".into(),
            artist: "The Beatles".into(),
            isrc: "GBAYE0601698".into(),
            ..Default::default()
        };
        let only_mix_items = [mix.clone()];
        let (only_mix, mix_score) = jiosaavn_pick_best(&only_mix_items, &want_conflicting_isrc);
        let only_mix_id = only_mix
            .map(|g| jiosaavn_string(g, "id"))
            .unwrap_or_default();
        assert!(
            mix_score > 0 && only_mix_id == "VaTL2kp-",
            "a conflicting ISRC must not erase the only title and artist match"
        );

        let want_loose = JioSaavnWant {
            title: "Come Together".into(),
            artist: "The Beatles".into(),
            ..Default::default()
        };
        let loose_items = [mix, remaster];
        let (loose, loose_score) = jiosaavn_pick_best(&loose_items, &want_loose);
        let loose_id = loose.map(|g| jiosaavn_string(g, "id")).unwrap_or_default();
        assert!(
            loose_score > 0 && !loose_id.is_empty(),
            "title and artist alone should still match a version when duration and ISRC are absent"
        );
    }

    #[tokio::test]
    async fn test_jio_saavn_live_come_together_selection() {
        if std::env::var("AURALIS_JIOSAAVN_LIVE").as_deref() != Ok("1") {
            return;
        }
        let want = JioSaavnWant {
            title: "Come Together".into(),
            artist: "The Beatles".into(),
            ..Default::default()
        };
        let (id, _src) = search_jiosaavn(&want, &|| Ok(()))
            .await
            .expect("search should succeed");
        assert!(!id.is_empty());
        let stream_url = jiosaavn_official_stream_url(&id)
            .await
            .expect("stream url should resolve");
        assert!(!stream_url.is_empty());
    }

    #[test]
    fn test_jiosaavn_queries_prefer_clean_title() {
        let queries = jiosaavn_queries("On Time (with John Legend)", "Huntrix, EJAE");
        assert!(!queries.is_empty(), "no queries");
        let joined = queries.join(" | ").to_lowercase();
        assert!(joined.contains("on time"), "missing clean title: {joined}");
    }

    #[test]
    fn test_replace_audio_extension() {
        let got = replace_audio_extension(r"C:\Music\track.flac", ".m4a");
        assert!(got.ends_with(".m4a"), "got {got:?}");
        assert_eq!(got, r"C:\Music\track.m4a");

        let got2 = replace_audio_extension("/music/track.flac", "m4a");
        assert_eq!(got2, "/music/track.m4a");

        let got3 = replace_audio_extension("track", ".mp3");
        assert_eq!(got3, "track.mp3");
    }

    #[test]
    fn test_finalize_jiosaavn_path() {
        let temp_dir = tempfile::tempdir().unwrap();
        let flac_file = temp_dir.path().join("song.flac");
        std::fs::write(&flac_file, b"test audio content").unwrap();

        let finalized =
            finalize_jiosaavn_path(&flac_file, "https://aac.saavncdn.com/123/sample_320.mp4");
        assert_eq!(finalized.extension().and_then(|s| s.to_str()), Some("m4a"));
        assert!(finalized.exists());
        assert!(!flac_file.exists());

        let flac_file2 = temp_dir.path().join("song2.flac");
        std::fs::write(&flac_file2, b"test audio content").unwrap();
        let finalized2 = finalize_jiosaavn_path(&flac_file2, "https://cdn.example.com/song.flac");
        assert_eq!(finalized2, flac_file2);
        assert!(flac_file2.exists());
    }

    #[tokio::test]
    async fn test_jiosaavn_loopback_search_and_resolve() {
        use tokio::io::{AsyncReadExt, AsyncWriteExt};

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let base_url = format!("http://127.0.0.1:{}/api.php", addr.port());

        let server_task = tokio::spawn(async move {
            let search_fixture = include_bytes!("../tests/fixtures/jiosaavn/search_response.json");
            let details_fixture = include_bytes!("../tests/fixtures/jiosaavn/song_details.json");

            for _ in 0..10 {
                let (mut socket, _) = match listener.accept().await {
                    Ok(res) => res,
                    Err(_) => break,
                };
                let mut buf = [0u8; 4096];
                let n = match socket.read(&mut buf).await {
                    Ok(n) if n > 0 => n,
                    _ => continue,
                };
                let req_str = String::from_utf8_lossy(&buf[..n]);

                let (status, body) = if req_str.contains("__call=autocomplete.get")
                    && req_str.contains("query=test")
                {
                    ("200 OK", b"{}".as_slice())
                } else if req_str.contains("__call=autocomplete.get") {
                    ("200 OK", search_fixture.as_slice())
                } else if req_str.contains("__call=song.getDetails") {
                    ("200 OK", details_fixture.as_slice())
                } else {
                    ("404 Not Found", b"".as_slice())
                };

                let response = format!(
                    "HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
                    body.len()
                );
                let _ = socket.write_all(response.as_bytes()).await;
                let _ = socket.write_all(body).await;
            }
        });

        // 1. Health check test
        let health = jiosaavn_health_with_base_url(&base_url).await;
        assert!(health, "health check should succeed");

        // 2. Search test
        let want = JioSaavnWant {
            title: "Come Together".into(),
            artist: "The Beatles".into(),
            ..Default::default()
        };
        let (song_id, source_url) = search_jiosaavn_with_base_url(&want, &|| Ok(()), &base_url)
            .await
            .expect("search should succeed");
        assert_eq!(song_id, "buPhYncP");
        assert_eq!(source_url, "https://www.jiosaavn.com/song/buPhYncP");

        // 3. Resolve test (search + stream URL)
        let (stream_url, source_url) =
            resolve_jiosaavn_track_with_base_url(&want, &|| Ok(()), &base_url)
                .await
                .expect("resolve track should succeed");
        assert_eq!(source_url, "https://www.jiosaavn.com/song/buPhYncP");
        assert_eq!(stream_url, "https://aac.saavncdn.com/123/sample_320.mp4");

        // 4. Cancelled check test
        let cancel_err = search_jiosaavn_with_base_url(
            &want,
            &|| Err(ProviderError::Other("cancelled by user".into())),
            &base_url,
        )
        .await
        .unwrap_err();
        assert!(cancel_err.to_string().contains("cancelled"));

        server_task.abort();
    }
}

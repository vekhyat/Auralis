//! JioSaavn provider client, search, and DES media URL decryption.

use crate::error::{ProviderError, Result};
use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine;
use cipher::{BlockDecrypt, KeyInit};
use des::Des;
use regex::Regex;
use serde::Deserialize;
use std::sync::LazyLock;

pub const JIOSAAVN_OFFICIAL_API: &str = "https://www.jiosaavn.com/api.php";
pub const JIOSAAVN_COMMUNITY_API: &str = "https://saavn.dev/api";
pub const JIOSAAVN_DES_KEY: &[u8; 8] = b"38346591";

static FEAT_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)\s*[\(\[](feat\.?|ft\.?|with|featuring)\s[^\)\]]+[\)\]]").unwrap()
});
static BRACKETS_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\s*[\(\[].*?[\)\]]\s*").unwrap());

/// Decrypts a JioSaavn encrypted media URL using DES ECB mode with key `38346591`.
pub fn decrypt_jiosaavn_url(encrypted_b64: &str) -> Result<String> {
    let trimmed = encrypted_b64.trim();
    if trimmed.is_empty() {
        return Err(ProviderError::Crypto("empty encrypted media url".into()));
    }

    let raw = BASE64
        .decode(trimmed)
        .map_err(|e| ProviderError::Crypto(format!("base64 decode error: {e}")))?;

    if raw.len() % 8 != 0 || raw.is_empty() {
        return Err(ProviderError::Crypto(format!(
            "invalid encrypted url length: {}",
            raw.len()
        )));
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

    let unpadded = pkcs7_unpad(&out)?;
    let mut url_str = String::from_utf8(unpadded.to_vec())
        .map_err(|e| ProviderError::Crypto(format!("utf8 error: {e}")))?;

    if url_str.starts_with("http://") {
        url_str = url_str.replacen("http://", "https://", 1);
    }

    let clean = url_str.trim().to_string();
    if clean.is_empty() {
        return Err(ProviderError::Crypto("decrypted url empty".into()));
    }

    Ok(clean)
}

fn pkcs7_unpad(data: &[u8]) -> Result<&[u8]> {
    if data.is_empty() {
        return Ok(data);
    }
    let pad = *data.last().unwrap() as usize;
    if pad == 0 || pad > data.len() || pad > 8 {
        // Fallback: trim trailing nulls if not standard pkcs7
        let mut len = data.len();
        while len > 0 && data[len - 1] == 0 {
            len -= 1;
        }
        return Ok(&data[..len]);
    }
    for &b in &data[data.len() - pad..] {
        if b as usize != pad {
            let mut len = data.len();
            while len > 0 && data[len - 1] == 0 {
                len -= 1;
            }
            return Ok(&data[..len]);
        }
    }
    Ok(&data[..data.len() - pad])
}

/// Generates search query variations for title and artist.
pub fn jiosaavn_queries(title: &str, artist: &str) -> Vec<String> {
    let title = title.trim();
    let first_artist = artist
        .split(&[',', '&', '/'][..])
        .next()
        .unwrap_or(artist)
        .trim();

    let mut variants = vec![title.to_string()];
    let stripped1 = FEAT_RE.replace_all(title, " ");
    let stripped2 = BRACKETS_RE.replace_all(&stripped1, " ");
    let cleaned: String = stripped2.split_whitespace().collect::<Vec<_>>().join(" ");

    if !cleaned.is_empty() && !cleaned.eq_ignore_ascii_case(title) {
        variants.push(cleaned);
    }

    let mut queries = Vec::new();
    let mut seen = std::collections::HashSet::new();

    for v in variants {
        let with_artist = format!("{v} {first_artist}").trim().to_string();
        if !with_artist.is_empty() && seen.insert(with_artist.to_lowercase()) {
            queries.push(with_artist);
        }
        if seen.insert(v.to_lowercase()) {
            queries.push(v);
        }
    }

    queries
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
}

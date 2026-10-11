//! Spotify TOTP generator and authentication helper.

use std::time::{SystemTime, UNIX_EPOCH};

use data_encoding::BASE32_NOPAD;
use hmac::{Hmac, Mac};
use sha1::Sha1;

use crate::error::{ProviderError, Result};

pub const SPOTIFY_TOTP_SECRET: &str =
    "GM3TMMJTGYZTQNZVGM4DINJZHA4TGOBYGMZTCMRTGEYDSMJRHE4TEOBUG4YTCMRUGQ4DQOJUGQYTAMRRGA2TCMJSHE3TCMBY";
pub const SPOTIFY_TOTP_VERSION: u32 = 61;
pub const SPOTIFY_TOTP_PERIOD_SECS: u64 = 30;

type HmacSha1 = Hmac<Sha1>;

/// Generates the current Spotify TOTP code using the current system time.
/// Returns `(code, version)`.
pub fn generate_spotify_totp_now() -> Result<(String, u32)> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|e| ProviderError::Auth(format!("system clock error: {e}")))?
        .as_secs();
    generate_spotify_totp(now)
}

/// Generates the Spotify TOTP code for a specific timestamp (in seconds).
/// Returns `(code, version)`.
pub fn generate_spotify_totp(now_secs: u64) -> Result<(String, u32)> {
    let code = compute_totp(SPOTIFY_TOTP_SECRET, now_secs, SPOTIFY_TOTP_PERIOD_SECS, 6)?;
    Ok((code, SPOTIFY_TOTP_VERSION))
}

/// Computes an RFC 6238 TOTP code for an arbitrary Base32 secret, timestamp,
/// period step, and output digit length.
pub fn compute_totp(
    secret_base32: &str,
    timestamp_secs: u64,
    period: u64,
    digits: u32,
) -> Result<String> {
    if period == 0 {
        return Err(ProviderError::Auth("totp period cannot be zero".into()));
    }
    if digits == 0 || digits > 10 {
        return Err(ProviderError::Auth(format!(
            "invalid totp digits: expected 1..=10, got {digits}"
        )));
    }

    let clean_secret = secret_base32.trim().to_uppercase().replace('=', "");
    let secret_bytes = BASE32_NOPAD
        .decode(clean_secret.as_bytes())
        .map_err(|e| ProviderError::Auth(format!("invalid base32 secret: {e}")))?;

    let counter = timestamp_secs / period;
    let counter_bytes = counter.to_be_bytes();

    let mut mac = HmacSha1::new_from_slice(&secret_bytes)
        .map_err(|e| ProviderError::Auth(format!("hmac key init error: {e}")))?;
    mac.update(&counter_bytes);
    let result = mac.finalize().into_bytes();

    let offset = (result[19] & 0x0f) as usize;
    let binary = ((result[offset] as u32 & 0x7f) << 24)
        | ((result[offset + 1] as u32) << 16)
        | ((result[offset + 2] as u32) << 8)
        | (result[offset + 3] as u32);

    let modulo = 10u32.pow(digits);
    let code = binary % modulo;
    Ok(format!("{:0width$}", code, width = digits as usize))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_compute_totp_rfc4226_rfc6238_vectors() {
        let secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ";
        let code1 = compute_totp(secret, 59, 30, 6).unwrap();
        assert_eq!(code1, "287082");

        let code2 = compute_totp(secret, 1111111109, 30, 6).unwrap();
        assert_eq!(code2, "081804");

        let code3 = compute_totp(secret, 1234567890, 30, 8).unwrap();
        assert_eq!(code3, "89005924");
    }

    #[test]
    fn test_spotify_totp_deterministic_timestamps() {
        let (code1, version1) = generate_spotify_totp(1700000000).expect("totp 1");
        assert_eq!(version1, 61);
        assert_eq!(code1, "371599");

        let (code2, version2) = generate_spotify_totp(1777777777).expect("totp 2");
        assert_eq!(version2, 61);
        assert_eq!(code2, "661762");
    }

    #[test]
    fn test_spotify_totp_within_same_window_is_identical() {
        let (code1, v1) = generate_spotify_totp(1700000010).expect("window 1");
        let (code2, v2) = generate_spotify_totp(1700000025).expect("window 1 still");
        assert_eq!(code1, code2);
        assert_eq!(v1, v2);

        // Next window (30s boundary past 1700000039)
        let (code3, _) = generate_spotify_totp(1700000050).expect("window 2");
        assert_ne!(code1, code3);
    }

    #[test]
    fn test_spotify_totp_now() {
        let (code, version) = generate_spotify_totp_now().expect("totp now succeeds");
        assert_eq!(version, 61);
        assert_eq!(code.len(), 6);
        assert!(code.chars().all(|c| c.is_ascii_digit()));
    }

    #[test]
    fn test_invalid_parameters() {
        assert!(compute_totp("INVALID_BASE32!!!", 1000, 30, 6).is_err());
        assert!(compute_totp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", 1000, 0, 6).is_err());
        assert!(compute_totp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", 1000, 30, 0).is_err());
        assert!(compute_totp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", 1000, 30, 11).is_err());
    }
}

//! Deezer Blowfish CBC chunk decryption.
//! Decrypts 2048-byte chunks where `chunk_index % 3 == 0` using Blowfish in CBC mode.
//! Other chunks and trailing partial chunks pass through unmodified.

use std::io::{Read, Write};

use blowfish::Blowfish;
use cipher::{BlockDecrypt, KeyInit};
use md5::{Digest, Md5};

use crate::error::{ProviderError, Result};

/// Deezer encrypted chunk size (2048 bytes).
pub const DEEZER_CHUNK_SIZE: usize = 2048;

/// Fixed 16-byte initialization vector reset at the beginning of each decrypted chunk.
pub const DEEZER_IV: [u8; 8] = [0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07];

/// Deezer fixed secret key used in key derivation.
pub const DEEZER_SECRET_KEY: &[u8; 16] = b"g4el58wc0zvf9na1";

/// Derives the 16-byte Deezer Blowfish decryption key for a given track ID.
///
/// Algorithm matches Go's `deezerBlowfishKey`:
/// 1. MD5 hex string (32 lowercase hex characters) is computed for `track_id`.
/// 2. For `i` in `0..16`: `key[i] = md5_hex[i] ^ md5_hex[i+16] ^ secret[i]`.
pub fn get_blowfish_key(track_id: &str) -> [u8; 16] {
    let mut hasher = Md5::new();
    hasher.update(track_id.as_bytes());
    let digest = hasher.finalize();
    let hash_hex = format!("{digest:032x}");
    let hash_bytes = hash_hex.as_bytes();

    let mut key = [0u8; 16];
    for i in 0..16 {
        key[i] = hash_bytes[i] ^ hash_bytes[i + 16] ^ DEEZER_SECRET_KEY[i];
    }
    key
}

/// Trims whitespace and truncates key to at most 56 bytes (Blowfish max key length).
pub fn deezer_blowfish_key(raw: &str) -> Vec<u8> {
    let bytes = raw.trim().as_bytes();
    if bytes.len() > 56 {
        bytes[..56].to_vec()
    } else {
        bytes.to_vec()
    }
}

/// Decrypts a single 2048-byte chunk in place using Blowfish in CBC mode with [`DEEZER_IV`].
pub fn decrypt_deezer_chunk(chunk: &mut [u8], key: &[u8]) -> Result<()> {
    if chunk.len() != DEEZER_CHUNK_SIZE {
        return Err(ProviderError::Crypto(format!(
            "invalid chunk size for Deezer decryption: expected {DEEZER_CHUNK_SIZE}, got {}",
            chunk.len()
        )));
    }
    let cipher = Blowfish::new_from_slice(key)
        .map_err(|e| ProviderError::Crypto(format!("invalid blowfish key: {e}")))?;
    decrypt_chunk_cbc(&cipher, chunk);
    Ok(())
}

/// Decrypts Deezer audio from `reader` into `writer` using the key derived from `track_id`.
pub fn decrypt_deezer_stream<R: Read, W: Write>(
    reader: R,
    writer: W,
    track_id: &str,
) -> Result<()> {
    let key = get_blowfish_key(track_id);
    decrypt_deezer_stream_with_key(reader, writer, &key)
}

/// Decrypts Deezer audio from `reader` into `writer` using an explicit key.
///
/// Processing rules:
/// - Audio is processed in chunks of up to [`DEEZER_CHUNK_SIZE`] (2048 bytes).
/// - Only chunks where `chunk_index % 3 == 0` AND chunk length is exactly 2048 bytes are decrypted.
/// - All other chunks (where `chunk_index % 3 != 0`) pass through unmodified.
/// - The final trailing chunk, if shorter than 2048 bytes, always passes through unmodified
///   even if its chunk index would otherwise qualify.
pub fn decrypt_deezer_stream_with_key<R: Read, W: Write>(
    mut reader: R,
    mut writer: W,
    key: &[u8],
) -> Result<()> {
    if key.is_empty() {
        return Err(ProviderError::Crypto(
            "empty Deezer blowfish key".to_string(),
        ));
    }
    let cipher = Blowfish::new_from_slice(key)
        .map_err(|e| ProviderError::Crypto(format!("invalid blowfish key: {e}")))?;

    let mut buf = vec![0u8; DEEZER_CHUNK_SIZE];
    let mut block_num = 0usize;

    loop {
        let mut nread = 0usize;
        while nread < DEEZER_CHUNK_SIZE {
            match reader.read(&mut buf[nread..]) {
                Ok(0) => break,
                Ok(n) => nread += n,
                Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
                Err(e) => return Err(ProviderError::Io(e)),
            }
        }

        if nread == 0 {
            break;
        }

        if block_num.is_multiple_of(3) && nread == DEEZER_CHUNK_SIZE {
            decrypt_chunk_cbc(&cipher, &mut buf);
            writer.write_all(&buf).map_err(ProviderError::Io)?;
        } else {
            writer.write_all(&buf[..nread]).map_err(ProviderError::Io)?;
        }

        block_num += 1;
    }

    writer.flush().map_err(ProviderError::Io)?;
    Ok(())
}

/// Decrypts an entire byte slice of Deezer audio using an explicit key.
/// Matches Go's `decryptDeezerStream(encrypted []byte, key []byte) ([]byte, error)`.
pub fn decrypt_deezer_bytes(encrypted: &[u8], key: &[u8]) -> Result<Vec<u8>> {
    let mut cursor = std::io::Cursor::new(encrypted);
    let mut out = Vec::with_capacity(encrypted.len());
    decrypt_deezer_stream_with_key(&mut cursor, &mut out, key)?;
    Ok(out)
}

/// In-place Blowfish CBC decryption of an 8-byte aligned chunk with IV reset to [`DEEZER_IV`].
fn decrypt_chunk_cbc(cipher: &Blowfish, chunk: &mut [u8]) {
    let mut prev_iv = DEEZER_IV;
    for block in chunk.as_chunks_mut::<8>().0 {
        let block: &mut [u8] = block;
        let mut ct_block = *cipher::generic_array::GenericArray::from_slice(block);
        let next_iv: [u8; 8] = block.try_into().expect("block is 8 bytes");
        cipher.decrypt_block(&mut ct_block);
        for i in 0..8 {
            block[i] = ct_block[i] ^ prev_iv[i];
        }
        prev_iv = next_iv;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use cipher::BlockEncrypt;

    fn encrypt_chunk_cbc(cipher: &Blowfish, chunk: &mut [u8]) {
        let mut prev_iv = DEEZER_IV;
        for block in chunk.as_chunks_mut::<8>().0 {
            let block: &mut [u8] = block;
            for i in 0..8 {
                block[i] ^= prev_iv[i];
            }
            let mut pt_block = *cipher::generic_array::GenericArray::from_slice(block);
            cipher.encrypt_block(&mut pt_block);
            block.copy_from_slice(&pt_block);
            prev_iv.copy_from_slice(block);
        }
    }

    #[test]
    fn test_get_blowfish_key_derivation_vectors() {
        let key1 = get_blowfish_key("12345678");
        assert_eq!(&key1, b"357mcn%5?x$d=i04");
        assert_eq!(hex::encode(key1), "3335376d636e25353f7824643d693034");

        let key2 = get_blowfish_key("3135556");
        assert_eq!(hex::encode(key2), "6c6c666b39662c37652575603c643439");
    }

    #[test]
    fn test_deezer_blowfish_key_trim_and_truncate() {
        assert_eq!(deezer_blowfish_key("  secret123  "), b"secret123");
        let long = "a".repeat(100);
        assert_eq!(deezer_blowfish_key(&long).len(), 56);
    }

    #[test]
    fn test_decrypt_deezer_chunk() {
        let key = b"my_secret_key_16";
        let cipher = Blowfish::new_from_slice(key).unwrap();

        let original = vec![0x37u8; DEEZER_CHUNK_SIZE];
        let mut chunk = original.clone();
        encrypt_chunk_cbc(&cipher, &mut chunk);
        assert_ne!(chunk, original);

        decrypt_deezer_chunk(&mut chunk, key).expect("chunk decrypts");
        assert_eq!(chunk, original);

        let mut bad_chunk = vec![0u8; 100];
        assert!(decrypt_deezer_chunk(&mut bad_chunk, key).is_err());
    }

    #[test]
    fn test_decrypt_deezer_stream_and_bytes_multi_chunk() {
        let key = b"g4el58wc0zvf9na1";
        let cipher = Blowfish::new_from_slice(key).unwrap();

        let total_size = DEEZER_CHUNK_SIZE * 4 + 1234;
        let mut original = vec![0u8; total_size];
        for (i, byte) in original.iter_mut().enumerate() {
            *byte = (i % 251) as u8;
        }

        let mut encrypted = original.clone();
        encrypt_chunk_cbc(&cipher, &mut encrypted[0..DEEZER_CHUNK_SIZE]);
        let c3_start = DEEZER_CHUNK_SIZE * 3;
        let c3_end = DEEZER_CHUNK_SIZE * 4;
        encrypt_chunk_cbc(&cipher, &mut encrypted[c3_start..c3_end]);

        assert_ne!(
            &encrypted[0..DEEZER_CHUNK_SIZE],
            &original[0..DEEZER_CHUNK_SIZE]
        );
        assert_eq!(
            &encrypted[DEEZER_CHUNK_SIZE..c3_start],
            &original[DEEZER_CHUNK_SIZE..c3_start]
        );
        assert_ne!(&encrypted[c3_start..c3_end], &original[c3_start..c3_end]);
        assert_eq!(&encrypted[c3_end..], &original[c3_end..]);

        let decrypted_bytes =
            decrypt_deezer_bytes(&encrypted, key).expect("decrypt bytes succeeds");
        assert_eq!(decrypted_bytes, original);

        let mut stream_out = Vec::new();
        let reader = std::io::Cursor::new(&encrypted);
        decrypt_deezer_stream_with_key(reader, &mut stream_out, key)
            .expect("decrypt stream succeeds");
        assert_eq!(stream_out, original);
    }

    #[test]
    fn test_decrypt_deezer_stream_by_track_id() {
        let track_id = "12345678";
        let derived_key = get_blowfish_key(track_id);
        let cipher = Blowfish::new_from_slice(&derived_key).unwrap();

        let original = vec![0xabu8; DEEZER_CHUNK_SIZE];
        let mut encrypted = original.clone();
        encrypt_chunk_cbc(&cipher, &mut encrypted);

        let mut decrypted = Vec::new();
        let reader = std::io::Cursor::new(encrypted);
        decrypt_deezer_stream(reader, &mut decrypted, track_id)
            .expect("stream decrypts with track_id");
        assert_eq!(decrypted, original);
    }

    #[test]
    fn test_empty_key_fails() {
        let data = vec![0u8; 100];
        assert!(decrypt_deezer_bytes(&data, b"").is_err());

        let mut out = Vec::new();
        assert!(
            decrypt_deezer_stream_with_key(std::io::Cursor::new(&data), &mut out, b"").is_err()
        );
    }
}

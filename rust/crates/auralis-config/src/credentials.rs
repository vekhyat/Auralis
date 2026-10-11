//! OS-protected credential blobs (port of `backend/credentials`).
//!
//! On Windows each secret is encrypted with DPAPI (`CryptProtectData`) scoped to
//! the current user, using the entropy string `Auralis credentials v1` encoded as
//! UTF-16LE without a trailing NUL and the flag `CRYPTPROTECT_UI_FORBIDDEN`.
//! That blob is what the Go app writes, so tokens saved by Go still load.
//! Other platforms fail closed and never store plaintext.

use crate::{ensure_app_dir, restrict_private_file, write_file_atomic};
use sha2::{Digest, Sha256};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use thiserror::Error;

const ENTROPY: &str = "Auralis credentials v1";

/// Errors from the credential store. `NotFound` matches Go's `ErrNotFound`.
#[derive(Debug, Error)]
pub enum Error {
    #[error("credentials: not found")]
    NotFound,
    #[error("credentials: empty directory")]
    EmptyDirectory,
    #[error("credentials: empty key")]
    EmptyKey,
    #[error("credentials: empty secret")]
    EmptySecret,
    #[error("credentials: empty blob")]
    EmptyBlob,
    #[error("credentials: encrypted credential storage is not supported on this platform")]
    Unsupported,
    #[error("credentials: create dir: {0}")]
    CreateDir(io::Error),
    #[error("credentials: write: {0}")]
    Write(io::Error),
    #[error("credentials: read: {0}")]
    Read(io::Error),
    #[error("credentials: decrypt: {0}")]
    Decrypt(String),
    #[error("credentials: delete: {0}")]
    Delete(io::Error),
    #[error("{0}")]
    Os(io::Error),
}

/// A directory of encrypted secret blobs, one file per key.
#[derive(Debug)]
pub struct Store {
    dir: PathBuf,
    mu: Mutex<()>,
}

impl Store {
    /// Opens the default store at `<app data dir>/credentials`.
    pub fn open() -> Result<Self, Error> {
        let app_dir = ensure_app_dir().map_err(Error::CreateDir)?;
        Self::open_at(app_dir.join("credentials"))
    }

    /// Opens a store rooted at `dir`. Pass a temp directory in tests.
    pub fn open_at(dir: impl AsRef<Path>) -> Result<Self, Error> {
        let dir = dir.as_ref();
        if dir.as_os_str().to_string_lossy().trim().is_empty() {
            return Err(Error::EmptyDirectory);
        }
        fs::create_dir_all(dir).map_err(Error::CreateDir)?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let _ = fs::set_permissions(dir, fs::Permissions::from_mode(0o700));
        }
        Ok(Self {
            dir: dir.to_path_buf(),
            mu: Mutex::new(()),
        })
    }

    pub fn dir(&self) -> &Path {
        &self.dir
    }

    fn path_for(&self, key: &str) -> Result<PathBuf, Error> {
        let key = key.trim();
        if key.is_empty() {
            return Err(Error::EmptyKey);
        }
        let mut safe = sanitize_key(key);
        if safe.len() > 64 {
            let sum = Sha256::digest(key.as_bytes());
            safe = format!("{}-{}", &safe[..48], hex::encode(&sum[..8]));
        }
        Ok(self.dir.join(format!("{safe}.bin")))
    }

    /// Encrypts `value` and stores it under `key`, replacing any previous value.
    pub fn put(&self, key: &str, value: &str) -> Result<(), Error> {
        let path = self.path_for(key)?;
        let blob = protect(value.as_bytes())?;
        let _guard = self.mu.lock().unwrap_or_else(|e| e.into_inner());
        write_file_atomic(&path, &blob).map_err(Error::Write)?;
        if let Err(err) = restrict_private_file(&path) {
            println!("credentials: harden {}: {err}", path.display());
        }
        Ok(())
    }

    /// Returns the decrypted value stored under `key`.
    pub fn get(&self, key: &str) -> Result<String, Error> {
        let path = self.path_for(key)?;
        let _guard = self.mu.lock().unwrap_or_else(|e| e.into_inner());
        let blob = match fs::read(&path) {
            Ok(bytes) => bytes,
            Err(err) if err.kind() == io::ErrorKind::NotFound => return Err(Error::NotFound),
            Err(err) => return Err(Error::Read(err)),
        };
        let plain = unprotect(&blob).map_err(|err| Error::Decrypt(err.to_string()))?;
        String::from_utf8(plain).map_err(|err| Error::Decrypt(err.to_string()))
    }

    /// Removes `key`. Missing keys are not an error.
    pub fn delete(&self, key: &str) -> Result<(), Error> {
        let path = self.path_for(key)?;
        let _guard = self.mu.lock().unwrap_or_else(|e| e.into_inner());
        match fs::remove_file(&path) {
            Ok(()) => Ok(()),
            Err(err) if err.kind() == io::ErrorKind::NotFound => Ok(()),
            Err(err) => Err(Error::Delete(err)),
        }
    }
}

/// Replaces each run of characters outside `[A-Za-z0-9._-]` with a single `_`.
fn sanitize_key(key: &str) -> String {
    let mut out = String::with_capacity(key.len());
    let mut prev_invalid = false;
    for c in key.chars() {
        if c.is_ascii_alphanumeric() || c == '.' || c == '_' || c == '-' {
            out.push(c);
            prev_invalid = false;
        } else if !prev_invalid {
            out.push('_');
            prev_invalid = true;
        }
    }
    out
}

#[cfg(windows)]
fn protect(data: &[u8]) -> Result<Vec<u8>, Error> {
    if data.is_empty() {
        return Err(Error::EmptySecret);
    }
    dpapi(data, true)
}

#[cfg(windows)]
fn unprotect(blob: &[u8]) -> Result<Vec<u8>, Error> {
    if blob.is_empty() {
        return Err(Error::EmptyBlob);
    }
    dpapi(blob, false)
}

#[cfg(windows)]
fn dpapi(data: &[u8], encrypt: bool) -> Result<Vec<u8>, Error> {
    use std::ffi::c_void;
    use windows_sys::Win32::Foundation::LocalFree;
    use windows_sys::Win32::Security::Cryptography::{
        CryptProtectData, CryptUnprotectData, CRYPTPROTECT_UI_FORBIDDEN, CRYPT_INTEGER_BLOB,
    };

    let entropy_units: Vec<u16> = ENTROPY.encode_utf16().collect();
    let entropy = CRYPT_INTEGER_BLOB {
        cbData: (entropy_units.len() * 2) as u32,
        pbData: entropy_units.as_ptr() as *mut u8,
    };
    let input = CRYPT_INTEGER_BLOB {
        cbData: data.len() as u32,
        pbData: data.as_ptr() as *mut u8,
    };
    let mut out = CRYPT_INTEGER_BLOB {
        cbData: 0,
        pbData: std::ptr::null_mut(),
    };
    let ok = unsafe {
        if encrypt {
            CryptProtectData(
                &input,
                std::ptr::null(),
                &entropy,
                std::ptr::null(),
                std::ptr::null(),
                CRYPTPROTECT_UI_FORBIDDEN,
                &mut out,
            )
        } else {
            CryptUnprotectData(
                &input,
                std::ptr::null_mut(),
                &entropy,
                std::ptr::null(),
                std::ptr::null(),
                CRYPTPROTECT_UI_FORBIDDEN,
                &mut out,
            )
        }
    };
    if ok == 0 {
        return Err(Error::Os(io::Error::last_os_error()));
    }
    struct FreeOnDrop(*mut u8);
    impl Drop for FreeOnDrop {
        fn drop(&mut self) {
            if !self.0.is_null() {
                unsafe {
                    LocalFree(self.0 as *mut c_void);
                }
            }
        }
    }
    let _guard = FreeOnDrop(out.pbData);
    if out.pbData.is_null() || out.cbData == 0 {
        return Ok(Vec::new());
    }
    let slice = unsafe { std::slice::from_raw_parts(out.pbData, out.cbData as usize) };
    Ok(slice.to_vec())
}

#[cfg(not(windows))]
fn protect(_data: &[u8]) -> Result<Vec<u8>, Error> {
    Err(Error::Unsupported)
}

#[cfg(not(windows))]
fn unprotect(_blob: &[u8]) -> Result<Vec<u8>, Error> {
    Err(Error::Unsupported)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_store_open_at_validates_dir() {
        let err = Store::open_at("   ").unwrap_err();
        assert_eq!(err.to_string(), "credentials: empty directory");
    }

    #[cfg(windows)]
    #[test]
    fn test_store_round_trip_windows() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::open_at(dir.path().join("credentials")).unwrap();
        let secret = "spotify-refresh-token-abc123-SECRET";
        store.put("spotify.refresh", secret).unwrap();
        let got = store.get("spotify.refresh").unwrap();
        assert_eq!(got, secret);

        let entries: Vec<_> = fs::read_dir(store.dir()).unwrap().collect();
        assert_eq!(entries.len(), 1);
        let entry = entries[0].as_ref().unwrap();
        assert_eq!(entry.file_name(), "spotify.refresh.bin");
        let blob = fs::read(entry.path()).unwrap();
        let text = String::from_utf8_lossy(&blob);
        assert!(
            !text.contains(secret) && !text.contains("spotify-refresh-token"),
            "plaintext secret found in stored blob"
        );

        store.delete("spotify.refresh").unwrap();
        let err = store.get("spotify.refresh").unwrap_err();
        assert!(
            matches!(err, Error::NotFound),
            "expected NotFound after delete, got {err}"
        );
        store.delete("spotify.refresh").unwrap();
    }

    #[cfg(not(windows))]
    #[test]
    fn test_store_fails_closed_off_windows() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::open_at(dir.path()).unwrap();
        let err = store.put("k", "v").unwrap_err();
        assert!(
            matches!(err, Error::Unsupported),
            "expected unsupported error on Put, got {err}"
        );
    }
}

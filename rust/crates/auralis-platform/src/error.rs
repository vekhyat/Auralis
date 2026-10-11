//! Error types for `auralis-platform`.

use thiserror::Error;

/// Errors arising from application path resolution and creation.
#[derive(Debug, Error)]
pub enum AppPathsError {
    #[error("AURALIS_APP_DIR must be an absolute path")]
    RelativeAppDir,

    #[error("failed to get home directory: {0}")]
    HomeDirNotFound(String),

    #[error("create app directory: {0}")]
    CreateAppDir(#[source] std::io::Error),

    #[error(transparent)]
    Io(#[from] std::io::Error),
}

/// Errors arising from file manager operations and audio metadata reading.
#[derive(Debug, Error)]
pub enum FileManagerError {
    #[error("file does not exist")]
    NotFound(String),

    #[error("unsupported file format: {0}")]
    UnsupportedFormat(String),

    #[error("failed to read directory: {0}")]
    ReadDir(String),

    #[error("failed to walk directory: {0}")]
    WalkDir(String),

    #[error("failed to parse FLAC file: {0}")]
    FlacParse(String),

    #[error("failed to open MP3 file: {0}")]
    Mp3Open(String),

    #[error("failed to probe metadata: {0}")]
    Probe(String),

    #[error(transparent)]
    Io(#[from] std::io::Error),
}

/// Errors arising from setting file icons.
#[derive(Debug, Error)]
pub enum FileIconError {
    #[error("{0}")]
    InvalidPath(String),

    #[error("image error: {0}")]
    ImageOpen(String),

    #[error("image encode error: {0}")]
    ImageEncode(String),

    #[error("process error: {0}")]
    ProcessSpawn(#[source] std::io::Error),

    #[error("script failed: {0}")]
    ScriptFailed(String),

    #[error(transparent)]
    Io(#[from] std::io::Error),
}

/// Errors arising from protocol registration.
#[derive(Debug, Error)]
pub enum ProtocolError {
    #[error("failed to get executable path: {0}")]
    Executable(#[source] std::io::Error),

    #[error("registry error: {0}")]
    Registry(#[source] std::io::Error),

    #[error(transparent)]
    Io(#[from] std::io::Error),
}

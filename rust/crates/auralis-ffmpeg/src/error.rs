//! Error types for FFmpeg / FFprobe operations.

use thiserror::Error;

#[derive(Error, Debug)]
pub enum FfmpegError {
    #[error("FFmpeg executable not found: {0}")]
    NotFound(String),

    #[error("Invalid executable name: {0}")]
    InvalidName(String),

    #[error("Validation failed for {0}: {1}")]
    ValidationFailed(String, String),

    #[error("Execution failed: {0}")]
    ExecutionFailed(String),

    #[error("Checksum mismatch for {0}: expected {1}, got {2}")]
    ChecksumMismatch(String, String, String),

    #[error("Release asset not found: {0}")]
    AssetNotFound(String),

    #[error("Archive extraction failed: {0}")]
    ExtractionFailed(String),

    #[error("Media probe failed: {0}")]
    ProbeFailed(String),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),

    #[error("HTTP error: {0}")]
    Http(#[from] reqwest::Error),
}

pub type Result<T> = std::result::Result<T, FfmpegError>;

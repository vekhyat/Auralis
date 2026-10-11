//! Error types for tagging, cover art, and lyrics operations.

use thiserror::Error;

#[derive(Error, Debug)]
pub enum TaggerError {
    #[error("Lofty audio error: {0}")]
    Lofty(String),

    #[error("Image error: {0}")]
    Image(#[from] image::ImageError),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    #[error("FFmpeg error: {0}")]
    Ffmpeg(#[from] auralis_ffmpeg::FfmpegError),

    #[error("HTTP error: {0}")]
    Http(#[from] reqwest::Error),

    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),

    #[error("Invalid metadata: {0}")]
    InvalidMetadata(String),

    #[error("Tag not supported for format: {0}")]
    UnsupportedFormat(String),
}

impl From<lofty::error::FileParseError> for TaggerError {
    fn from(err: lofty::error::FileParseError) -> Self {
        TaggerError::Lofty(err.to_string())
    }
}

impl From<lofty::error::FileEncodingError> for TaggerError {
    fn from(err: lofty::error::FileEncodingError) -> Self {
        TaggerError::Lofty(err.to_string())
    }
}

pub type Result<T> = std::result::Result<T, TaggerError>;

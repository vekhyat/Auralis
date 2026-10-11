//! Provider errors.

use thiserror::Error;

#[derive(Error, Debug)]
pub enum ProviderError {
    #[error("HTTP request error: {0}")]
    Http(#[from] reqwest::Error),

    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    #[error("Crypto error: {0}")]
    Crypto(String),

    #[error("Auth error: {0}")]
    Auth(String),

    #[error("Resolution failed: {0}")]
    ResolutionFailed(String),

    #[error("Mirror unavailable: {0}")]
    MirrorUnavailable(String),

    #[error("Parse error: {0}")]
    Parse(String),

    /// Plain message, shown verbatim like Go's `fmt.Errorf` text.
    #[error("{0}")]
    Other(String),

    /// Go's `ErrDownloadCancelled`.
    #[error(transparent)]
    Cancelled(#[from] auralis_core::download_cancel::DownloadCancelled),
}

pub type Result<T> = std::result::Result<T, ProviderError>;

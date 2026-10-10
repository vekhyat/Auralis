//! Error definitions for Auralis.

use thiserror::Error;

pub mod store {
    use super::*;

    #[derive(Error, Debug)]
    pub enum StoreError {
        #[error("Redb error: {0}")]
        Redb(String),

        #[error("Legacy DB parse error: {0}")]
        LegacyParse(String),

        #[error("Key not found: {0}")]
        NotFound(String),

        #[error("Invalid data format: {0}")]
        InvalidData(String),
    }
}

#[derive(Error, Debug)]
pub enum AuralisError {
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    #[error("JSON serialization error: {0}")]
    Json(#[from] serde_json::Error),

    #[error("Store error: {0}")]
    Store(#[from] store::StoreError),

    #[error("Validation error: {0}")]
    Validation(String),

    #[error("Configuration error: {0}")]
    Config(String),

    #[error("Migration error: {0}")]
    Migration(String),

    #[error("Other error: {0}")]
    Other(String),
}

pub type Result<T> = std::result::Result<T, AuralisError>;

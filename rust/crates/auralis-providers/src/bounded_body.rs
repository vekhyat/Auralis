//! Port of `backend/bounded_body.go`.
//!
//! Size-limited response readers so a misbehaving provider cannot exhaust
//! memory: at most `limit` bytes are accepted, and anything larger fails with
//! the same `response exceeds <limit>-byte limit` message as the Go version.

use tokio::io::AsyncReadExt;

use crate::error::{ProviderError, Result};

/// Port of Go's `maxProviderJSONBytes` (`2 << 20` = 2097152).
pub const MAX_PROVIDER_JSON_BYTES: u64 = 2 << 20;

fn limit_exceeded(limit: u64) -> ProviderError {
    ProviderError::Other(format!("response exceeds {limit}-byte limit"))
}

/// Port of Go's `readBoundedBody`: reads `reader` up to `limit` bytes.
/// Mirrors the Go implementation by reading at most `limit + 1` bytes and
/// failing when the input is longer than `limit`.
pub async fn read_bounded_body(
    reader: impl tokio::io::AsyncRead + Unpin,
    limit: u64,
) -> Result<Vec<u8>> {
    let mut data = Vec::new();
    reader
        .take(limit.saturating_add(1))
        .read_to_end(&mut data)
        .await?;
    if data.len() as u64 > limit {
        return Err(limit_exceeded(limit));
    }
    Ok(data)
}

/// Streaming variant for HTTP responses: reads `response` chunk by chunk and
/// fails with the same limit error as soon as more than `limit` bytes arrive,
/// without waiting for the rest of the body.
pub async fn read_bounded_response(response: reqwest::Response, limit: u64) -> Result<Vec<u8>> {
    let mut response = response;
    let mut data = Vec::new();
    while let Some(chunk) = response.chunk().await? {
        if data.len() as u64 + chunk.len() as u64 > limit {
            return Err(limit_exceeded(limit));
        }
        data.extend_from_slice(&chunk);
    }
    Ok(data)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn read_bounded_body_accepts_under_limit() {
        let body = read_bounded_body(std::io::Cursor::new(b"123"), 4)
            .await
            .expect("under-limit body must be accepted");
        assert_eq!(body, b"123");
    }

    #[tokio::test]
    async fn read_bounded_body_accepts_exactly_at_limit() {
        let body = read_bounded_body(std::io::Cursor::new(b"1234"), 4)
            .await
            .expect("at-limit body must be accepted");
        assert_eq!(body, b"1234");
    }

    #[tokio::test]
    async fn read_bounded_body_rejects_over_limit() {
        let err = read_bounded_body(std::io::Cursor::new(b"12345"), 4)
            .await
            .expect_err("over-limit body must be rejected");
        assert_eq!(
            err.to_string(),
            "Other error: response exceeds 4-byte limit"
        );
    }

    #[tokio::test]
    async fn read_bounded_body_accepts_empty() {
        let body = read_bounded_body(std::io::Cursor::new(b""), 4)
            .await
            .expect("empty body must be accepted");
        assert!(body.is_empty());
    }

    #[test]
    fn max_provider_json_bytes_matches_go() {
        assert_eq!(MAX_PROVIDER_JSON_BYTES, 2 << 20);
        assert_eq!(MAX_PROVIDER_JSON_BYTES, 2_097_152);
    }
}

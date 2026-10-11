//! Amazon Music provider endpoints (port of `backend/provider_endpoints.go`).

/// Base URL for the Amazon Music API proxy service.
pub const AMAZON_MUSIC_API_BASE_URL: &str = "https://amazon.spotbye.qzz.io";

/// Returns the base URL for the Amazon Music API proxy service.
pub fn get_amazon_music_api_base_url() -> &'static str {
    AMAZON_MUSIC_API_BASE_URL
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_amazon_music_api_base_url() {
        assert_eq!(
            get_amazon_music_api_base_url(),
            "https://amazon.spotbye.qzz.io"
        );
        assert_eq!(AMAZON_MUSIC_API_BASE_URL, "https://amazon.spotbye.qzz.io");
    }
}

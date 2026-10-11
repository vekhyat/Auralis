//! Provider rate limiting and cooldown state (port of `backend/provider_cooldown.go`).

use std::collections::HashMap;
use std::sync::{LazyLock, Mutex};
use std::time::{Duration, Instant};

/// Minimum duration to cool down a rate-limited provider (5 minutes).
pub const PROVIDER_RATE_LIMIT_FLOOR: Duration = Duration::from_secs(5 * 60);

/// Maximum duration to cool down a rate-limited provider (10 minutes).
pub const PROVIDER_RATE_LIMIT_CAP: Duration = Duration::from_secs(10 * 60);

/// Error returned when an operation is skipped because the provider is in cooldown.
#[derive(Debug, Clone)]
pub struct ProviderRateLimitError {
    pub provider: String,
    pub remaining: Duration,
}

impl std::fmt::Display for ProviderRateLimitError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "{} temporarily skipped after rate limit ({} remaining)",
            self.provider,
            format_duration_rounded_to_seconds(self.remaining)
        )
    }
}

impl std::error::Error for ProviderRateLimitError {}

/// Formats a duration rounded to seconds matching Go's `d.Round(time.Second).String()`.
pub fn format_duration_rounded_to_seconds(d: Duration) -> String {
    let total_secs = d.as_secs_f64().round() as u64;
    let hours = total_secs / 3600;
    let mins = (total_secs % 3600) / 60;
    let secs = total_secs % 60;

    if hours > 0 {
        format!("{hours}h{mins}m{secs}s")
    } else if mins > 0 {
        format!("{mins}m{secs}s")
    } else {
        format!("{secs}s")
    }
}

/// Registry managing cooldown expiration for rate-limited providers.
pub struct ProviderCooldownRegistry {
    cooldowns: Mutex<HashMap<String, Instant>>,
}

impl Default for ProviderCooldownRegistry {
    fn default() -> Self {
        Self::new()
    }
}

impl ProviderCooldownRegistry {
    /// Creates a new empty cooldown registry.
    pub fn new() -> Self {
        Self {
            cooldowns: Mutex::new(HashMap::new()),
        }
    }

    /// Marks a provider as rate-limited for `wait` duration, clamped between
    /// `PROVIDER_RATE_LIMIT_FLOOR` and `PROVIDER_RATE_LIMIT_CAP`.
    pub fn mark_provider_rate_limited(&self, provider: &str, mut wait: Duration) {
        let provider = provider.trim().to_lowercase();
        if provider.is_empty() {
            return;
        }
        if wait < PROVIDER_RATE_LIMIT_FLOOR {
            wait = PROVIDER_RATE_LIMIT_FLOOR;
        }
        if wait > PROVIDER_RATE_LIMIT_CAP {
            wait = PROVIDER_RATE_LIMIT_CAP;
        }
        let until = Instant::now() + wait;
        let mut map = self.cooldowns.lock().unwrap_or_else(|e| e.into_inner());
        map.insert(provider.clone(), until);
        drop(map);
        println!(
            "{} rate limited; skipping that provider for {}",
            provider,
            format_duration_rounded_to_seconds(wait)
        );
    }

    /// Checks if a provider is currently rate limited, returning a [`ProviderRateLimitError`]
    /// if so, or `Ok(())` if it is ready.
    pub fn skip_if_provider_rate_limited(
        &self,
        provider: &str,
    ) -> Result<(), ProviderRateLimitError> {
        let provider = provider.trim().to_lowercase();
        if provider.is_empty() {
            return Ok(());
        }
        let map = self.cooldowns.lock().unwrap_or_else(|e| e.into_inner());
        let until = map.get(&provider).copied();
        drop(map);

        if let Some(until) = until {
            let now = Instant::now();
            if until > now {
                let remaining = until - now;
                return Err(ProviderRateLimitError {
                    provider,
                    remaining,
                });
            }
        }
        Ok(())
    }

    /// Clears all provider cooldowns in this registry.
    pub fn clear(&self) {
        let mut map = self.cooldowns.lock().unwrap_or_else(|e| e.into_inner());
        map.clear();
    }
}

/// Global registry used by the application process.
pub static GLOBAL_PROVIDER_COOLDOWN: LazyLock<ProviderCooldownRegistry> =
    LazyLock::new(ProviderCooldownRegistry::new);

/// Maps short Zarz provider codes to their public canonical provider names.
pub fn zarz_public_provider_name(provider: &str) -> String {
    match provider.trim().to_lowercase().as_str() {
        "tid" => "tidal".to_string(),
        "qbz" => "qobuz".to_string(),
        "amazeamazeamaze" => "amazon".to_string(),
        other => other.to_string(),
    }
}

/// Marks a provider as rate-limited in the global cooldown registry.
pub fn mark_provider_rate_limited(provider: &str, wait: Duration) {
    GLOBAL_PROVIDER_COOLDOWN.mark_provider_rate_limited(provider, wait);
}

/// Returns an error if the provider is currently rate-limited in the global registry.
pub fn skip_if_provider_rate_limited(provider: &str) -> Result<(), ProviderRateLimitError> {
    GLOBAL_PROVIDER_COOLDOWN.skip_if_provider_rate_limited(provider)
}

/// Structured error response from the Zarz API.
#[derive(Debug, Clone, Default)]
pub struct ZarzApiError {
    pub status: u16,
    pub message: String,
    pub code: String,
    pub origin: String,
    pub retryable: bool,
    pub retry_mode: String,
    pub retry_after_seconds: i64,
    pub body: Vec<u8>,
}

impl ZarzApiError {
    pub fn is_rate_limited(&self) -> bool {
        if self.status == 429 {
            return true;
        }
        let combined = format!("{} {}", self.message, self.code).to_lowercase();
        let combined = combined.trim();
        combined.contains("server busy")
            || combined.contains("rate limit")
            || combined.contains("too many requests")
            || combined.contains("retry shortly")
    }

    pub fn same_operation_retry(&self) -> bool {
        self.status == 503 && self.retryable && self.retry_mode == "same_operation"
    }

    pub fn should_retry(&self) -> bool {
        self.same_operation_retry() || self.is_rate_limited()
    }
}

impl std::fmt::Display for ZarzApiError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if !self.message.trim().is_empty() {
            write!(f, "zarz HTTP {}: {}", self.status, self.message)
        } else {
            let preview = if self.body.len() <= 180 {
                String::from_utf8_lossy(&self.body).to_string()
            } else {
                String::from_utf8_lossy(&self.body[..180]).to_string()
            };
            write!(f, "zarz HTTP {}: {}", self.status, preview)
        }
    }
}

impl std::error::Error for ZarzApiError {}

/// Returns whether an error represents a rate limit condition.
pub fn is_rate_limited_error(err: &(dyn std::error::Error + 'static)) -> bool {
    if let Some(_skip_err) = err.downcast_ref::<ProviderRateLimitError>() {
        return true;
    }
    if let Some(api_err) = err.downcast_ref::<ZarzApiError>() {
        if api_err.is_rate_limited() {
            return true;
        }
    }
    let msg = err.to_string().to_lowercase();
    msg.contains("http 429")
        || msg.contains("rate limited")
        || msg.contains("server busy")
        || msg.contains("too many requests")
        || msg.contains("retry shortly")
        || msg.contains("temporarily skipped after rate limit")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_zarz_rate_limited_error() {
        let err = ZarzApiError {
            status: 429,
            message: "Server busy, retry shortly".to_string(),
            code: String::new(),
            origin: String::new(),
            retryable: false,
            retry_mode: String::new(),
            retry_after_seconds: 0,
            body: Vec::new(),
        };

        assert!(
            err.is_rate_limited() && err.should_retry(),
            "429 busy errors should be retried"
        );
        assert!(
            is_rate_limited_error(&err),
            "wrapped 429 should be detected as rate limited"
        );
    }

    #[test]
    fn test_zarz_public_provider_names() {
        assert_eq!(zarz_public_provider_name("tid"), "tidal");
        assert_eq!(zarz_public_provider_name("qbz"), "qobuz");
        assert_eq!(zarz_public_provider_name("amazeamazeamaze"), "amazon");
        assert_eq!(zarz_public_provider_name("DEEZER"), "deezer");
        assert_eq!(zarz_public_provider_name(" unknown "), "unknown");
    }

    #[test]
    fn test_cooldown_clamping_and_check() {
        let registry = ProviderCooldownRegistry::new();

        // Clamping to floor (5m)
        registry.mark_provider_rate_limited("tidal", Duration::from_secs(10));
        let err = registry
            .skip_if_provider_rate_limited("tidal")
            .expect_err("provider should be limited");
        assert_eq!(err.provider, "tidal");
        assert!(err.remaining >= Duration::from_secs(295));

        // Unknown provider is not limited
        assert!(registry.skip_if_provider_rate_limited("deezer").is_ok());

        // Error message string formatting check
        let msg = err.to_string();
        assert!(msg.contains("tidal temporarily skipped after rate limit"));
        assert!(msg.contains("remaining"));
        assert!(is_rate_limited_error(&err));
    }

    #[test]
    fn test_format_duration_rounded_to_seconds() {
        assert_eq!(
            format_duration_rounded_to_seconds(Duration::from_secs(0)),
            "0s"
        );
        assert_eq!(
            format_duration_rounded_to_seconds(Duration::from_secs(45)),
            "45s"
        );
        assert_eq!(
            format_duration_rounded_to_seconds(Duration::from_secs(125)),
            "2m5s"
        );
        assert_eq!(
            format_duration_rounded_to_seconds(Duration::from_secs(3665)),
            "1h1m5s"
        );
    }
}

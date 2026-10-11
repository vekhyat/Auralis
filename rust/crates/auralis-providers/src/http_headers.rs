//! Port of `backend/http_headers.go`.
//!
//! Shared downloader HTTP constants: the default `User-Agent`, the default
//! `Accept` header, and the same-host-only redirect policy used for signed
//! provider requests so session/HMAC headers are never forwarded to a
//! different origin.
//!
//! Go's `WithDownloadContext` (tying a request to the active download so Stop
//! interrupts it) is `auralis_core::download_cancel::with_download_context`:
//! wrap the request future in it wherever Go calls `WithDownloadContext`.

use std::time::Duration;

/// Byte-for-byte port of Go's `DefaultDownloaderUserAgent`.
pub const DEFAULT_DOWNLOADER_USER_AGENT: &str = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36";

/// Value set for `Accept` by Go's `NewRequestWithDefaultHeaders`.
pub const DEFAULT_ACCEPT_HEADER: &str = "application/json, text/plain, */*";

/// Maximum redirects followed before giving up.
/// Mirrors Go's `len(via) >= 10` check in `SameHostCheckRedirect`.
pub const MAX_REDIRECTS: usize = 10;

/// Reason a redirect was not followed.
/// Both variants map to stopping the redirect chain, which is reqwest's
/// equivalent of Go returning `http.ErrUseLastResponse`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RedirectBlocked {
    /// Mirrors Go's `"stopped after 10 redirects"` error.
    TooManyRedirects,
    /// Mirrors Go's `http.ErrUseLastResponse` for cross-origin redirects.
    CrossOrigin,
}

impl std::fmt::Display for RedirectBlocked {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::TooManyRedirects => write!(f, "stopped after {MAX_REDIRECTS} redirects"),
            Self::CrossOrigin => write!(f, "use last response"),
        }
    }
}

impl std::error::Error for RedirectBlocked {}

/// Port of Go's `SameHostCheckRedirect`.
///
/// Returns `Ok(())` when the redirect to `next` may be followed given the
/// chain `via` (whose first entry is the original request URL, mirroring Go's
/// `via[0]` comparison), or the reason following must stop.
///
/// Like the Go version this compares scheme and host case-insensitively and
/// requires the effective port to match; an empty chain is always followed.
pub fn same_host_check_redirect(next: &url::Url, via: &[url::Url]) -> Result<(), RedirectBlocked> {
    if via.len() >= MAX_REDIRECTS {
        return Err(RedirectBlocked::TooManyRedirects);
    }
    let Some(orig) = via.first() else {
        return Ok(());
    };
    let same_scheme = next.scheme().eq_ignore_ascii_case(orig.scheme());
    let same_host = match (next.host_str(), orig.host_str()) {
        (Some(next_host), Some(orig_host)) => next_host.eq_ignore_ascii_case(orig_host),
        (None, None) => true,
        _ => false,
    };
    if same_scheme && same_host && next.port_or_known_default() == orig.port_or_known_default() {
        Ok(())
    } else {
        Err(RedirectBlocked::CrossOrigin)
    }
}

/// reqwest redirect policy implementing [`same_host_check_redirect`].
/// Port of the `CheckRedirect: SameHostCheckRedirect` wiring in Go's
/// `NewSignedHTTPClient`.
pub fn same_host_redirect_policy() -> reqwest::redirect::Policy {
    reqwest::redirect::Policy::custom(|attempt: reqwest::redirect::Attempt| {
        match same_host_check_redirect(attempt.url(), attempt.previous()) {
            Ok(()) => attempt.follow(),
            // Go returns an error here, so the request fails instead of
            // yielding the last 3xx response.
            Err(RedirectBlocked::TooManyRedirects) => {
                attempt.error(RedirectBlocked::TooManyRedirects)
            }
            Err(RedirectBlocked::CrossOrigin) => attempt.stop(),
        }
    })
}

/// Port of Go's `NewSignedHTTPClient`: a client with the given timeout that
/// only follows redirects while scheme and host stay the same.
pub fn new_signed_http_client(timeout: Duration) -> Result<reqwest::Client, reqwest::Error> {
    reqwest::Client::builder()
        .timeout(timeout)
        .redirect(same_host_redirect_policy())
        .build()
}

/// Default downloader headers, mirroring what Go's
/// `NewRequestWithDefaultHeaders` sets via `Header.Set` (insert/overwrite).
pub fn default_headers() -> reqwest::header::HeaderMap {
    let mut headers = reqwest::header::HeaderMap::new();
    headers.insert(
        reqwest::header::USER_AGENT,
        reqwest::header::HeaderValue::from_static(DEFAULT_DOWNLOADER_USER_AGENT),
    );
    headers.insert(
        reqwest::header::ACCEPT,
        reqwest::header::HeaderValue::from_static(DEFAULT_ACCEPT_HEADER),
    );
    headers
}

/// Port of Go's `NewRequestWithDefaultHeaders`: applies the default
/// `User-Agent` and `Accept` headers to a request builder, overwriting any
/// values already set.
///
/// reqwest's `RequestBuilder::header` appends rather than replacing, so this
/// builds the request first and uses `HeaderMap::insert`, matching Go's
/// `Header.Set` overwrite semantics exactly.
pub fn with_default_headers(
    builder: reqwest::RequestBuilder,
) -> Result<reqwest::Request, reqwest::Error> {
    let mut request = builder.build()?;
    request.headers_mut().insert(
        reqwest::header::USER_AGENT,
        reqwest::header::HeaderValue::from_static(DEFAULT_DOWNLOADER_USER_AGENT),
    );
    request.headers_mut().insert(
        reqwest::header::ACCEPT,
        reqwest::header::HeaderValue::from_static(DEFAULT_ACCEPT_HEADER),
    );
    Ok(request)
}

/// Constructor mirroring Go's `NewRequestWithDefaultHeaders(method, rawURL,
/// body)`: starts a request for `method` + `raw_url` on `client` with the
/// default headers applied. A body, if needed, is chained afterwards with
/// `RequestBuilder::body`, which does not disturb the headers.
pub fn new_request_with_default_headers(
    client: &reqwest::Client,
    method: reqwest::Method,
    raw_url: &str,
) -> crate::error::Result<reqwest::Request> {
    let parsed = url::Url::parse(raw_url)
        .map_err(|err| crate::error::ProviderError::Parse(err.to_string()))?;
    with_default_headers(client.request(method, parsed)).map_err(crate::error::ProviderError::Http)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn url(raw: &str) -> url::Url {
        url::Url::parse(raw).expect("test URL must parse")
    }

    #[test]
    fn same_host_check_redirect_blocks_cross_origin() {
        let orig = url("https://api.zarz.moe/v2/dl");
        let next = url("https://attacker.example/steal");
        assert_eq!(
            same_host_check_redirect(&next, &[orig]),
            Err(RedirectBlocked::CrossOrigin)
        );
    }

    #[test]
    fn same_host_check_redirect_allows_same_host() {
        let orig = url("https://api.zarz.moe/v2/dl");
        let next = url("https://api.zarz.moe/v2/dl/retry");
        assert_eq!(same_host_check_redirect(&next, &[orig]), Ok(()));
    }

    #[test]
    fn same_host_check_redirect_blocks_scheme_change() {
        let orig = url("https://api.zarz.moe/v2/dl");
        let next = url("http://api.zarz.moe/v2/dl");
        assert_eq!(
            same_host_check_redirect(&next, &[orig]),
            Err(RedirectBlocked::CrossOrigin)
        );
    }

    #[test]
    fn same_host_check_redirect_allows_empty_chain() {
        let next = url("https://api.zarz.moe/v2/dl");
        assert_eq!(same_host_check_redirect(&next, &[]), Ok(()));
    }

    #[test]
    fn same_host_check_redirect_stops_after_ten_redirects() {
        let orig = url("https://api.zarz.moe/v2/dl");
        let via = vec![orig.clone(); MAX_REDIRECTS];
        assert_eq!(
            same_host_check_redirect(&orig, &via),
            Err(RedirectBlocked::TooManyRedirects)
        );
        let via = vec![orig.clone(); MAX_REDIRECTS - 1];
        assert_eq!(same_host_check_redirect(&orig, &via), Ok(()));
    }

    #[test]
    fn default_headers_sets_user_agent_and_accept() {
        let headers = default_headers();
        assert_eq!(
            headers
                .get(reqwest::header::USER_AGENT)
                .and_then(|value| value.to_str().ok()),
            Some(DEFAULT_DOWNLOADER_USER_AGENT)
        );
        assert_eq!(
            headers
                .get(reqwest::header::ACCEPT)
                .and_then(|value| value.to_str().ok()),
            Some(DEFAULT_ACCEPT_HEADER)
        );
    }

    #[test]
    fn with_default_headers_overwrites_existing_values() {
        let builder = reqwest::Client::new()
            .get("https://example.com/api")
            .header(reqwest::header::USER_AGENT, "old-agent")
            .header(reqwest::header::ACCEPT, "old-accept");
        let Ok(request) = with_default_headers(builder) else {
            panic!("locally built request must build");
        };
        assert_eq!(
            request
                .headers()
                .get(reqwest::header::USER_AGENT)
                .and_then(|value| value.to_str().ok()),
            Some(DEFAULT_DOWNLOADER_USER_AGENT)
        );
        assert_eq!(
            request
                .headers()
                .get(reqwest::header::ACCEPT)
                .and_then(|value| value.to_str().ok()),
            Some(DEFAULT_ACCEPT_HEADER)
        );
    }

    #[test]
    fn new_request_with_default_headers_sets_headers() {
        let client = reqwest::Client::new();
        let Ok(request) = new_request_with_default_headers(
            &client,
            reqwest::Method::GET,
            "https://example.com/api",
        ) else {
            panic!("locally built request must build");
        };
        assert_eq!(request.method(), reqwest::Method::GET);
        assert_eq!(request.url().as_str(), "https://example.com/api");
        assert_eq!(
            request
                .headers()
                .get(reqwest::header::USER_AGENT)
                .and_then(|value| value.to_str().ok()),
            Some(DEFAULT_DOWNLOADER_USER_AGENT)
        );
        assert_eq!(
            request
                .headers()
                .get(reqwest::header::ACCEPT)
                .and_then(|value| value.to_str().ok()),
            Some(DEFAULT_ACCEPT_HEADER)
        );
    }

    #[test]
    fn new_request_with_default_headers_rejects_bad_url() {
        let client = reqwest::Client::new();
        assert!(
            new_request_with_default_headers(&client, reqwest::Method::GET, "://bad-url").is_err()
        );
    }

    #[test]
    fn product_constants_match_go_byte_for_byte() {
        assert_eq!(
            DEFAULT_DOWNLOADER_USER_AGENT,
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
        );
        assert_eq!(DEFAULT_ACCEPT_HEADER, "application/json, text/plain, */*");
    }

    /// Serves `count` HTTP/1.1 responses, each built by `respond(path)`.
    async fn serve(
        count: usize,
        respond: impl Fn(&str) -> String + Send + 'static,
    ) -> std::net::SocketAddr {
        use tokio::io::{AsyncReadExt, AsyncWriteExt};
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            for _ in 0..count {
                let Ok((mut socket, _)) = listener.accept().await else {
                    return;
                };
                let mut buf = vec![0u8; 4096];
                let n = socket.read(&mut buf).await.unwrap_or(0);
                let head = String::from_utf8_lossy(&buf[..n]).to_string();
                let path = head.split_whitespace().nth(1).unwrap_or("/").to_string();
                let _ = socket.write_all(respond(&path).as_bytes()).await;
                let _ = socket.shutdown().await;
            }
        });
        addr
    }

    #[tokio::test]
    async fn signed_client_errors_after_ten_same_host_redirects() {
        let addr = serve(20, |path| {
            let n: u32 = path.trim_start_matches("/r").parse().unwrap_or(0);
            format!(
                "HTTP/1.1 302 Found
Location: /r{}
Content-Length: 0
Connection: close

",
                n + 1
            )
        })
        .await;
        let client = new_signed_http_client(Duration::from_secs(5)).unwrap();
        let err = client
            .get(format!("http://{addr}/r0"))
            .send()
            .await
            .expect_err("redirect loop must fail like Go");
        assert!(err.is_redirect(), "unexpected error: {err}");
        let blocked = std::error::Error::source(&err)
            .and_then(|e| e.downcast_ref::<RedirectBlocked>())
            .copied();
        assert_eq!(blocked, Some(RedirectBlocked::TooManyRedirects));
        assert_eq!(
            RedirectBlocked::TooManyRedirects.to_string(),
            "stopped after 10 redirects"
        );
    }

    #[tokio::test]
    async fn signed_client_returns_last_response_on_cross_origin_redirect() {
        let addr = serve(1, |_| {
            "HTTP/1.1 302 Found
Location: http://example.invalid/x
Content-Length: 0
Connection: close

"
            .to_string()
        })
        .await;
        let client = new_signed_http_client(Duration::from_secs(5)).unwrap();
        let resp = client.get(format!("http://{addr}/")).send().await.unwrap();
        assert_eq!(resp.status(), reqwest::StatusCode::FOUND);
        assert_eq!(
            resp.headers().get(reqwest::header::LOCATION).unwrap(),
            "http://example.invalid/x"
        );
    }
}

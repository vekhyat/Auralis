//! Amazon Music provider: community and Zarz gateway download, stream copy, and remux.
//!
//! Community HMAC signing, the 401 re-verify dance, Zarz ticket signing, download
//! cancellation, and the Antra mirror are not available in this crate. HTTP shape,
//! status handling, and error strings follow `backend/amazon.go`.

use regex::Regex;
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use std::fs::OpenOptions;
use std::io::{Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use std::sync::LazyLock;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use auralis_ffmpeg::process::configure_command;
use auralis_ffmpeg::{get_ffmpeg_path, validate_executable};

const DEFAULT_DOWNLOADER_USER_AGENT: &str = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36";
const AMAZON_COMMUNITY_BASE_URL: &str = "https://amz-oss.spotbye.qzz.io";
const COMMUNITY_DOWNLOAD_PATH: &str = "/api/dl";
const ZARZ_AMAZON_DOWNLOAD_URL: &str = "https://api.zarz.moe/v2/dl/amazeamazeamaze";
const ZARZ_AMAZON_USER_AGENT: &str = "Auralis/amzn@2.2.0";
const COMMUNITY_COOLDOWN_NOTICE: &str = "This is normal and not a bug. ";
const MAX_PROVIDER_JSON_BYTES: u64 = 2 << 20;
const ZARZ_BODY_LIMIT: u64 = 8 << 20;
const COMMUNITY_PREVIEW_LIMIT: u64 = 4096;

/// `amazonAPITimeout` from `backend/amazon.go`.
pub const AMAZON_API_TIMEOUT: Duration = Duration::from_secs(120);
/// Declared in Go and unused. Stream transfers use [`MEDIA_DOWNLOAD_TIMEOUT`].
pub const AMAZON_STREAM_TIMEOUT: Duration = Duration::from_secs(5 * 60);
pub const AMAZON_STREAM_RETRIES: u32 = 3;
pub const AMAZON_STREAM_RETRY_WAIT: Duration = Duration::from_secs(2);
pub const MEDIA_DOWNLOAD_TIMEOUT: Duration = Duration::from_secs(30 * 60);
pub const MEDIA_HEADER_TIMEOUT: Duration = Duration::from_secs(30);
pub const DOWNLOAD_IDLE_TIMEOUT: Duration = Duration::from_secs(25);
pub const AMAZON_MUSIC_API_BASE_URL: &str = "https://amazon.spotbye.qzz.io";

static AMAZON_TRACK_ASIN_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?i)/tracks/([a-z0-9]{10})(?:[/?#]|$)").unwrap());
static AMAZON_GATEWAY_ASIN_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(B[0-9A-Z]{9})").unwrap());

pub fn extract_amazon_track_asin(url: &str) -> Option<String> {
    AMAZON_TRACK_ASIN_RE
        .captures(url.trim())
        .and_then(|c| c.get(1))
        .map(|m| m.as_str().to_string())
}

/// Case-sensitive gateway ASIN match used by `downloadFromGateway`.
pub fn extract_amazon_gateway_asin(url: &str) -> Option<String> {
    AMAZON_GATEWAY_ASIN_RE
        .find(url)
        .map(|m| m.as_str().to_string())
}

pub fn normalize_amazon_quality(quality: &str) -> &'static str {
    match quality.trim().to_lowercase().as_str() {
        "16" | "lossless" | "cd" => "16",
        "atmos" | "eac3" | "dolby" => "atmos",
        _ => "24",
    }
}

pub fn amazon_music_api_base_url() -> &'static str {
    AMAZON_MUSIC_API_BASE_URL
}

pub fn amazon_community_download_url() -> String {
    format!("{AMAZON_COMMUNITY_BASE_URL}{COMMUNITY_DOWNLOAD_PATH}")
}

pub fn map_amazon_quality_to_zarz_codec(quality: &str) -> &'static str {
    if normalize_amazon_quality(quality) == "atmos" {
        "eac3"
    } else {
        "flac"
    }
}

pub fn amazon_should_fallback_to_flac(quality: &str) -> bool {
    map_amazon_quality_to_zarz_codec(quality) != "flac"
}

fn amazon_target_ext(quality: &str, codec: &str) -> &'static str {
    let codec = codec.trim().to_lowercase();
    if normalize_amazon_quality(quality) == "atmos"
        || codec == "eac3"
        || codec == "ec-3"
        || codec == "ac-3"
    {
        ".m4a"
    } else {
        ".flac"
    }
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
pub struct AmazonCommunityResponse {
    #[serde(default)]
    pub asin: String,
    #[serde(default)]
    pub codec: String,
    #[serde(rename = "bit_depth", default)]
    pub bit_depth: u32,
    #[serde(default)]
    pub url: String,
    #[serde(rename = "stream_url", default)]
    pub stream_url: String,
    #[serde(default)]
    pub key: String,
    #[serde(rename = "key_specs", default)]
    pub key_specs: Vec<String>,
    #[serde(default)]
    pub captcha: String,
}

#[derive(Debug, Clone)]
pub struct AmazonConfig {
    pub community_url: String,
    pub zarz_url: String,
    pub api_timeout: Duration,
    pub stream_timeout: Duration,
    pub response_header_timeout: Duration,
    pub idle_timeout: Duration,
    pub connect_timeout: Duration,
    pub retry_wait: Duration,
    pub stream_retries: u32,
    pub rate_limit_max_wait: Duration,
    pub rate_limit_fallback: Duration,
    pub rate_limit_pad: Duration,
    pub gateway_retry_step: Duration,
    pub community_max_retries: u32,
    pub zarz_timeout: Duration,
    pub app_version: String,
    pub use_env_proxy: bool,
}

impl Default for AmazonConfig {
    fn default() -> Self {
        Self {
            community_url: amazon_community_download_url(),
            zarz_url: ZARZ_AMAZON_DOWNLOAD_URL.to_string(),
            api_timeout: AMAZON_API_TIMEOUT,
            stream_timeout: MEDIA_DOWNLOAD_TIMEOUT,
            response_header_timeout: MEDIA_HEADER_TIMEOUT,
            idle_timeout: DOWNLOAD_IDLE_TIMEOUT,
            connect_timeout: Duration::from_secs(15),
            retry_wait: AMAZON_STREAM_RETRY_WAIT,
            stream_retries: AMAZON_STREAM_RETRIES,
            rate_limit_max_wait: Duration::from_secs(30),
            rate_limit_fallback: Duration::from_secs(30),
            rate_limit_pad: Duration::from_millis(250),
            gateway_retry_step: Duration::from_secs(5),
            community_max_retries: 6,
            zarz_timeout: Duration::from_secs(60),
            app_version: "Unknown".to_string(),
            use_env_proxy: true,
        }
    }
}

#[derive(Debug)]
pub struct AmazonError {
    message: String,
    timeout: bool,
    deadline: bool,
    stalled: bool,
    cancelled: bool,
}

impl AmazonError {
    fn plain(message: impl Into<String>) -> Self {
        Self {
            message: message.into(),
            timeout: false,
            deadline: false,
            stalled: false,
            cancelled: false,
        }
    }

    fn stalled() -> Self {
        let mut err = Self::plain("download stalled");
        err.stalled = true;
        err
    }

    pub fn deadline() -> Self {
        let mut err = Self::plain("context deadline exceeded");
        err.deadline = true;
        err
    }

    fn net_timeout(message: impl Into<String>) -> Self {
        let mut err = Self::plain(message);
        err.timeout = true;
        err
    }

    fn header_timeout() -> Self {
        Self::net_timeout("net/http: timeout awaiting response headers")
    }

    fn io(err: std::io::Error) -> Self {
        Self::plain(err.to_string())
    }

    pub fn cancelled(message: impl Into<String>) -> Self {
        let mut err = Self::plain(message);
        err.cancelled = true;
        err
    }
}

impl std::fmt::Display for AmazonError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for AmazonError {}

/// Mirrors `amazonStreamHTTPClient`: total timeout and response-header timeout.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AmazonStreamClient {
    pub timeout: Duration,
    pub response_header_timeout: Duration,
}

pub fn amazon_stream_http_client() -> AmazonStreamClient {
    AmazonStreamClient {
        timeout: MEDIA_DOWNLOAD_TIMEOUT,
        response_header_timeout: MEDIA_HEADER_TIMEOUT,
    }
}

pub fn is_transient_amazon_stream_error(err: Option<&AmazonError>) -> bool {
    let Some(err) = err else {
        return false;
    };
    if err.cancelled {
        return false;
    }
    if err.stalled || err.deadline || err.timeout {
        return true;
    }
    let msg = err.message.to_lowercase();
    msg.contains("timeout")
        || msg.contains("deadline exceeded")
        || msg.contains("connection reset")
        || msg.contains("broken pipe")
        || msg.contains("tls handshake timeout")
        || msg.contains("stalled")
}

#[derive(Debug)]
pub struct AmazonDownloader {
    config: AmazonConfig,
}

impl Default for AmazonDownloader {
    fn default() -> Self {
        Self::new()
    }
}

impl AmazonDownloader {
    pub fn new() -> Self {
        Self {
            config: AmazonConfig::default(),
        }
    }

    pub fn with_config(config: AmazonConfig) -> Self {
        Self { config }
    }

    pub async fn fetch_amazon_community_response(
        &self,
        payload: &[u8],
    ) -> Result<AmazonCommunityResponse, AmazonError> {
        let resp = self.do_community_request(payload).await?;
        if resp.status != 200 {
            return Err(AmazonError::plain(format!(
                "Amazon API returned status {}",
                resp.status
            )));
        }
        serde_json::from_slice(&resp.body)
            .map_err(|err| AmazonError::plain(format!("failed to decode response: {err}")))
    }

    pub async fn download_from_community(
        &self,
        amazon_url: &str,
        output_dir: &Path,
        quality: &str,
    ) -> Result<String, AmazonError> {
        self.download_from_gateway(amazon_url, output_dir, quality, "")
            .await
    }

    pub async fn download_from_gateway(
        &self,
        amazon_url: &str,
        output_dir: &Path,
        quality: &str,
        source: &str,
    ) -> Result<String, AmazonError> {
        let asin = extract_amazon_gateway_asin(amazon_url).ok_or_else(|| {
            AmazonError::plain(format!("failed to extract ASIN from URL: {amazon_url}"))
        })?;
        let mut payload_map = Map::new();
        payload_map.insert("id".to_string(), Value::String(asin.clone()));
        payload_map.insert(
            "quality".to_string(),
            Value::String(normalize_amazon_quality(quality).to_string()),
        );
        payload_map.insert("country".to_string(), Value::String("US".to_string()));
        let payload =
            serde_json::to_vec(&payload_map).map_err(|err| AmazonError::plain(err.to_string()))?;

        println!("Fetching from Amazon API (ASIN: {asin})...");
        let api_resp = if source == "zarz" {
            self.get_amazon_zarz_stream(&asin, quality).await?
        } else {
            match self.fetch_amazon_community_response(&payload).await {
                Ok(resp) => resp,
                Err(err) if source.is_empty() => {
                    println!("Community Amazon API failed, trying Zarz: {err}");
                    match self.get_amazon_zarz_stream(&asin, quality).await {
                        Ok(resp) => resp,
                        Err(zarz_err) if amazon_should_fallback_to_flac(quality) => {
                            println!(
                                "Zarz Amazon {} failed, falling back to flac: {zarz_err}",
                                map_amazon_quality_to_zarz_codec(quality)
                            );
                            self.get_amazon_zarz_stream(&asin, "16").await?
                        }
                        Err(zarz_err) => return Err(zarz_err),
                    }
                }
                Err(err) => return Err(err),
            }
        };

        let mut stream_url = api_resp.stream_url.trim().to_string();
        if stream_url.is_empty() {
            stream_url = api_resp.url.trim().to_string();
        }
        if stream_url.is_empty() {
            return Err(AmazonError::plain("no stream URL found in response"));
        }
        let mut key_specs = api_resp.key_specs.clone();
        if key_specs.is_empty() {
            let key = api_resp.key.trim();
            if !key.is_empty() {
                key_specs.push(key.to_string());
            }
        }
        let captcha = api_resp.captcha.trim().to_string();

        let encrypted_path = output_dir.join(format!("{asin}.encrypted.mp4"));
        let _encrypted_tmp = TempRemove::new(encrypted_path.clone());
        {
            let mut out = OpenOptions::new()
                .write(true)
                .create(true)
                .truncate(true)
                .open(&encrypted_path)
                .map_err(AmazonError::io)?;
            println!("Downloading track: {asin}");
            let client = self.stream_client()?;
            let mut downloaded = false;
            for attempt in 1..=self.config.stream_retries {
                out.seek(SeekFrom::Start(0)).map_err(AmazonError::io)?;
                out.set_len(0).map_err(AmazonError::io)?;
                match self
                    .transfer_once(&client, &stream_url, &captcha, &mut out)
                    .await
                {
                    Ok(total) => {
                        let mb = total as f64 / (1024.0 * 1024.0);
                        println!("\rDownloaded: {mb:.2} MB (Complete)");
                        downloaded = true;
                        break;
                    }
                    Err(fail) => {
                        let (err, from_request) = match fail {
                            TransferError::Request(err) => (err, true),
                            TransferError::Copy(err) => (err, false),
                        };
                        if is_transient_amazon_stream_error(Some(&err))
                            && attempt < self.config.stream_retries
                        {
                            if from_request {
                                println!(
                                    "Amazon stream timed out, retrying ({attempt}/{})...",
                                    self.config.stream_retries
                                );
                            } else {
                                println!(
                                    "Amazon stream download interrupted, retrying ({attempt}/{})...",
                                    self.config.stream_retries
                                );
                            }
                            tokio::time::sleep(self.config.retry_wait).await;
                            continue;
                        }
                        return Err(err);
                    }
                }
            }
            if !downloaded {
                return Err(AmazonError::plain("download stalled"));
            }
            out.flush().map_err(AmazonError::io)?;
        }

        let mut remux_input = encrypted_path.clone();
        let _decrypted_tmp = if key_specs.is_empty() {
            None
        } else {
            println!("Decrypting file...");
            let decrypted_path = output_dir.join(format!("{asin}.decrypted.mp4"));
            crate::mp4ff::decrypt_with_mp4ff(&key_specs, &encrypted_path, &decrypted_path)
                .map_err(|err| AmazonError::plain(err.to_string()))?;
            remux_input = decrypted_path.clone();
            println!("Decryption successful");
            Some(TempRemove::new(decrypted_path))
        };

        let target_ext = amazon_target_ext(quality, &api_resp.codec);
        let final_path = output_dir.join(format!("{asin}{target_ext}"));
        if let Err(err) = amazon_remux_with_ffmpeg(&remux_input, &final_path, target_ext) {
            let _ = std::fs::remove_file(&final_path);
            return Err(err);
        }
        match std::fs::metadata(&final_path) {
            Ok(info) if info.len() > 0 => Ok(final_path.to_string_lossy().into_owned()),
            _ => {
                let _ = std::fs::remove_file(&final_path);
                Err(AmazonError::plain("remuxed file missing or empty"))
            }
        }
    }

    async fn get_amazon_zarz_stream(
        &self,
        asin: &str,
        quality: &str,
    ) -> Result<AmazonCommunityResponse, AmazonError> {
        let codec = map_amazon_quality_to_zarz_codec(quality);
        println!("Trying Zarz Amazon API (ASIN {asin}, {codec})...");
        let mut payload_map = Map::new();
        payload_map.insert("asin".to_string(), Value::String(asin.to_string()));
        payload_map.insert("codec".to_string(), Value::String(codec.to_string()));
        let payload =
            serde_json::to_vec(&payload_map).map_err(|err| AmazonError::plain(err.to_string()))?;
        let client = self.zarz_client()?;
        let response = send_following(
            &client,
            "POST",
            &self.config.zarz_url,
            &[
                ("Accept".to_string(), "application/json".to_string()),
                ("Content-Type".to_string(), "application/json".to_string()),
                ("User-Agent".to_string(), ZARZ_AMAZON_USER_AGENT.to_string()),
            ],
            Some(payload),
            true,
            None,
        )
        .await?;
        let status = response.status().as_u16();
        let body = read_capped(response, ZARZ_BODY_LIMIT).await?;
        if status != 200 {
            return Err(zarz_http_error(status, &body));
        }
        let parsed = parse_zarz_amazon(&body, asin, codec)?;
        println!("Zarz Amazon stream found");
        Ok(parsed)
    }

    async fn do_community_request(&self, payload: &[u8]) -> Result<CommunityHttp, AmazonError> {
        let client = self.api_client()?;
        let max_retries = self.config.community_max_retries;
        let mut last_err = AmazonError::plain("Amazon community API rate limited (429)");
        let mut attempt = 0u32;
        loop {
            let response = self.send_community(&client, payload).await?;
            let status = response.status().as_u16();
            if status == 503 {
                return Err(self.community_cooldown("Amazon", response).await);
            }
            if status != 429 && status != 502 && status != 504 {
                let body = if status == 200 {
                    read_bounded(response, MAX_PROVIDER_JSON_BYTES).await?
                } else {
                    Vec::new()
                };
                return Ok(CommunityHttp { status, body });
            }
            let wait = if status == 429 {
                community_retry_after(&response, &self.config)
            } else {
                last_err = AmazonError::plain(format!("Amazon community API returned {status}"));
                self.config
                    .gateway_retry_step
                    .saturating_mul(attempt.saturating_add(1))
            };
            drop(response);
            if attempt == max_retries {
                break;
            }
            let secs = wait.as_secs_f64();
            println!(
                "Amazon transient error, waiting {secs:.0}s before retry ({}/{max_retries})...",
                attempt + 1
            );
            tokio::time::sleep(wait).await;
            attempt += 1;
        }
        Err(last_err)
    }

    async fn send_community(
        &self,
        client: &reqwest::Client,
        payload: &[u8],
    ) -> Result<reqwest::Response, AmazonError> {
        send_following(
            client,
            "POST",
            &self.config.community_url,
            &[
                ("Content-Type".to_string(), "application/json".to_string()),
                ("Accept".to_string(), "application/json".to_string()),
                (
                    "User-Agent".to_string(),
                    community_user_agent(&self.config.app_version),
                ),
            ],
            Some(payload.to_vec()),
            true,
            None,
        )
        .await
    }

    async fn community_cooldown(&self, service: &str, response: reqwest::Response) -> AmazonError {
        let mut seconds: i64 = 0;
        if let Some(raw) = header_str(&response, "retry-after") {
            if let Ok(secs) = raw.trim().parse::<i64>() {
                if secs > 0 {
                    seconds = secs;
                }
            }
        }
        let body = read_capped(response, COMMUNITY_PREVIEW_LIMIT)
            .await
            .unwrap_or_default();
        let mut message = serde_json::from_slice::<Value>(&body)
            .ok()
            .and_then(|value| {
                value
                    .get("error")?
                    .as_str()
                    .map(str::trim)
                    .map(str::to_string)
            })
            .unwrap_or_default();
        if seconds <= 0 {
            seconds = i64::try_from(self.config.rate_limit_fallback.as_secs()).unwrap_or(30);
            if seconds <= 0 {
                seconds = 30;
            }
        }
        if message.is_empty() {
            let minutes = ((seconds + 59) / 60).max(1);
            message = format!(
                "The server is taking a scheduled short break. Please try again in about {minutes} minute(s)."
            );
        }
        if !message.starts_with(COMMUNITY_COOLDOWN_NOTICE) {
            message = format!("{COMMUNITY_COOLDOWN_NOTICE}{message}");
        }
        println!("{service} community API on scheduled cooldown (503), back in ~{seconds}s");
        AmazonError::plain(message)
    }

    async fn transfer_once(
        &self,
        client: &reqwest::Client,
        stream_url: &str,
        captcha: &str,
        out: &mut std::fs::File,
    ) -> Result<u64, TransferError> {
        let mut headers = vec![
            (
                "User-Agent".to_string(),
                DEFAULT_DOWNLOADER_USER_AGENT.to_string(),
            ),
            (
                "Accept".to_string(),
                "application/json, text/plain, */*".to_string(),
            ),
        ];
        if !captcha.is_empty() {
            headers.push(("x-captcha-token".to_string(), captcha.to_string()));
        }
        let response = match send_following(
            client,
            "GET",
            stream_url,
            &headers,
            None,
            false,
            Some(self.config.response_header_timeout),
        )
        .await
        {
            Err(err) => return Err(TransferError::Request(err)),
            Ok(response) => response,
        };
        let status = response.status().as_u16();
        if status != 200 {
            return Err(TransferError::Copy(AmazonError::plain(format!(
                "Amazon stream returned status {status}"
            ))));
        }
        copy_body(response, out, self.config.idle_timeout)
            .await
            .map_err(TransferError::Copy)
    }

    fn api_client(&self) -> Result<reqwest::Client, AmazonError> {
        self.http_client(self.config.api_timeout)
    }

    fn stream_client(&self) -> Result<reqwest::Client, AmazonError> {
        self.http_client(self.config.stream_timeout)
    }

    fn zarz_client(&self) -> Result<reqwest::Client, AmazonError> {
        self.http_client(self.config.zarz_timeout)
    }

    fn http_client(&self, timeout: Duration) -> Result<reqwest::Client, AmazonError> {
        let mut builder = reqwest::Client::builder()
            .timeout(timeout)
            .connect_timeout(self.config.connect_timeout)
            .pool_idle_timeout(Duration::from_secs(30))
            .pool_max_idle_per_host(8)
            .tcp_keepalive(Duration::from_secs(30))
            .redirect(reqwest::redirect::Policy::none());
        if !self.config.use_env_proxy {
            builder = builder.no_proxy();
        }
        builder
            .build()
            .map_err(|err| AmazonError::plain(err.to_string()))
    }
}

struct CommunityHttp {
    status: u16,
    body: Vec<u8>,
}

enum TransferError {
    Request(AmazonError),
    Copy(AmazonError),
}

struct TempRemove {
    path: PathBuf,
}

impl TempRemove {
    fn new(path: PathBuf) -> Self {
        Self { path }
    }
}

impl Drop for TempRemove {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.path);
    }
}

async fn send_following(
    client: &reqwest::Client,
    method: &str,
    url: &str,
    headers: &[(String, String)],
    mut body: Option<Vec<u8>>,
    same_host_only: bool,
    header_timeout: Option<Duration>,
) -> Result<reqwest::Response, AmazonError> {
    let op = go_op(method);
    let mut current = reqwest::Url::parse(url)
        .map_err(|err| AmazonError::plain(format!("{op} \"{url}\": {err}")))?;
    let origin = current.clone();
    let mut current_method = method.to_string();
    let mut sent = 0u32;
    loop {
        let response = dispatch(
            client,
            &current_method,
            &current,
            headers,
            body.as_deref(),
            header_timeout,
            &op,
        )
        .await?;
        let status = response.status().as_u16();
        let loc = header_str(&response, "location").unwrap_or_default();
        let Some((next_method, keep_body)) = redirect_behavior(&current_method, status) else {
            return Ok(response);
        };
        if loc.is_empty() {
            return Ok(response);
        }
        sent += 1;
        if sent >= 10 {
            return Err(AmazonError::plain(format!(
                "{op} \"{loc}\": stopped after 10 redirects"
            )));
        }
        let next = match current.join(&loc) {
            Ok(parsed) => parsed,
            Err(err) => {
                return Err(AmazonError::plain(format!(
                    "{op} \"{current}\": failed to parse Location header \"{loc}\": {err}"
                )));
            }
        };
        if same_host_only && !same_go_origin(&origin, &next) {
            return Ok(response);
        }
        if !keep_body {
            body = None;
        }
        current_method = next_method;
        current = next;
    }
}

async fn dispatch(
    client: &reqwest::Client,
    method: &str,
    url: &reqwest::Url,
    headers: &[(String, String)],
    body: Option<&[u8]>,
    header_timeout: Option<Duration>,
    op: &str,
) -> Result<reqwest::Response, AmazonError> {
    let mut request = client.request(http_method(method), url.clone());
    for (key, value) in headers {
        if body.is_none()
            && (key.eq_ignore_ascii_case("content-type")
                || key.eq_ignore_ascii_case("content-length"))
        {
            continue;
        }
        request = request.header(key, value);
    }
    if let Some(bytes) = body {
        request = request.body(bytes.to_vec());
    }
    let pending = request.send();
    if let Some(limit) = header_timeout {
        match tokio::time::timeout(limit, pending).await {
            Err(_) => Err(AmazonError::header_timeout()),
            Ok(result) => result.map_err(|err| map_reqwest(err, op)),
        }
    } else {
        pending.await.map_err(|err| map_reqwest(err, op))
    }
}

fn http_method(method: &str) -> reqwest::Method {
    match method {
        "POST" => reqwest::Method::POST,
        "HEAD" => reqwest::Method::HEAD,
        _ => reqwest::Method::GET,
    }
}

fn go_op(method: &str) -> String {
    let mut chars = method.chars();
    let Some(first) = chars.next() else {
        return "Get".to_string();
    };
    let mut out = first.to_uppercase().to_string();
    for ch in chars {
        out.extend(ch.to_lowercase());
    }
    out
}

fn redirect_behavior(method: &str, status: u16) -> Option<(String, bool)> {
    match status {
        301..=303 => {
            let next = if method == "GET" || method == "HEAD" {
                method.to_string()
            } else {
                "GET".to_string()
            };
            Some((next, false))
        }
        307 | 308 => Some((method.to_string(), true)),
        _ => None,
    }
}

fn same_go_origin(left: &reqwest::Url, right: &reqwest::Url) -> bool {
    left.scheme().eq_ignore_ascii_case(right.scheme())
        && go_host(left).eq_ignore_ascii_case(&go_host(right))
}

fn go_host(url: &reqwest::Url) -> String {
    let host = match url.host() {
        Some(url::Host::Ipv6(addr)) => format!("[{addr}]"),
        Some(_) => url.host_str().unwrap_or("").to_string(),
        None => String::new(),
    };
    match url.port() {
        Some(port) => format!("{host}:{port}"),
        None => host,
    }
}

fn community_user_agent(version: &str) -> String {
    let version = version.trim();
    if version.is_empty() || version == "Unknown" {
        "Auralis".to_string()
    } else {
        format!("Auralis/{version}")
    }
}

fn community_retry_after(response: &reqwest::Response, config: &AmazonConfig) -> Duration {
    if let Some(raw) = header_str(response, "retry-after") {
        if let Ok(secs) = raw.trim().parse::<i64>() {
            if secs >= 0 {
                return capped_wait(Duration::from_secs(secs_u64(secs)), config);
            }
        }
    }
    if let Some(raw) = header_str(response, "x-ratelimit-reset") {
        if let Ok(epoch) = raw.trim().parse::<i64>() {
            if epoch >= 0 {
                if let Some(wait) = until_epoch(epoch as u64) {
                    if !wait.is_zero() {
                        return capped_wait(wait, config);
                    }
                }
            }
        }
    }
    config.rate_limit_fallback
}

fn capped_wait(wait: Duration, config: &AmazonConfig) -> Duration {
    wait.saturating_add(config.rate_limit_pad)
        .min(config.rate_limit_max_wait)
}

fn secs_u64(secs: i64) -> u64 {
    u64::try_from(secs).unwrap_or(0).min(86_400)
}

fn until_epoch(epoch: u64) -> Option<Duration> {
    let now = SystemTime::now().duration_since(UNIX_EPOCH).ok()?;
    let capped = epoch.min(now.as_secs().saturating_add(86_400));
    if capped <= now.as_secs() {
        return Some(Duration::ZERO);
    }
    Some(Duration::from_secs(capped - now.as_secs()))
}

fn header_str(response: &reqwest::Response, name: &str) -> Option<String> {
    response
        .headers()
        .get(name)
        .and_then(|value| value.to_str().ok())
        .map(str::to_string)
}

fn map_reqwest(err: reqwest::Error, method: &str) -> AmazonError {
    let raw = err.to_string();
    if raw.contains("stopped after 10 redirects") {
        let url = err.url().map(reqwest::Url::as_str).unwrap_or("");
        return AmazonError::plain(format!("{method} \"{url}\": stopped after 10 redirects"));
    }
    let mut out = AmazonError::plain(raw);
    if err.is_timeout() {
        out.timeout = true;
    }
    out
}

async fn read_bounded(response: reqwest::Response, limit: u64) -> Result<Vec<u8>, AmazonError> {
    let bytes = read_capped(response, limit.saturating_add(1)).await?;
    if bytes.len() as u64 > limit {
        return Err(AmazonError::plain(format!(
            "response exceeds {limit}-byte limit"
        )));
    }
    Ok(bytes)
}

async fn read_capped(mut response: reqwest::Response, limit: u64) -> Result<Vec<u8>, AmazonError> {
    let limit = usize::try_from(limit).unwrap_or(usize::MAX);
    let mut out = Vec::new();
    while out.len() < limit {
        let next = response
            .chunk()
            .await
            .map_err(|err| map_reqwest(err, "Post"))?;
        let Some(chunk) = next else {
            break;
        };
        let room = limit - out.len();
        if chunk.len() > room {
            out.extend_from_slice(&chunk[..room]);
            break;
        }
        out.extend_from_slice(&chunk);
    }
    Ok(out)
}

async fn copy_body(
    mut response: reqwest::Response,
    out: &mut std::fs::File,
    idle: Duration,
) -> Result<u64, AmazonError> {
    let mut total = 0u64;
    loop {
        let next = match tokio::time::timeout(idle, response.chunk()).await {
            Err(_) => return Err(AmazonError::stalled()),
            Ok(Err(err)) => return Err(map_reqwest(err, "Get")),
            Ok(Ok(chunk)) => chunk,
        };
        let Some(chunk) = next else {
            break;
        };
        out.write_all(&chunk).map_err(AmazonError::io)?;
        total += u64::try_from(chunk.len()).unwrap_or(0);
    }
    Ok(total)
}

#[derive(Debug, Default, Deserialize)]
struct ZarzAmazonAudio {
    #[serde(default)]
    url: String,
    #[serde(default)]
    key: String,
    #[serde(default)]
    codec: String,
    #[serde(default)]
    captcha: String,
    #[serde(default)]
    key_specs: Vec<String>,
}

#[derive(Debug, Deserialize)]
struct ZarzAmazonItem {
    #[serde(default)]
    audio: ZarzAmazonAudio,
    #[serde(default)]
    url: String,
    #[serde(default)]
    key: String,
    #[serde(default)]
    codec: String,
}

fn parse_zarz_amazon(
    body: &[u8],
    asin: &str,
    requested_codec: &str,
) -> Result<AmazonCommunityResponse, AmazonError> {
    let items = match serde_json::from_slice::<Vec<ZarzAmazonItem>>(body) {
        Ok(items) => items,
        Err(array_err) => match serde_json::from_slice::<ZarzAmazonItem>(body) {
            Ok(single) => vec![single],
            Err(_) => {
                return Err(AmazonError::plain(format!(
                    "invalid zarz amazon response: {array_err}"
                )));
            }
        },
    };
    if items.is_empty() {
        return Err(AmazonError::plain("zarz amazon returned an empty response"));
    }
    let item = &items[0];
    let mut stream_url = item.audio.url.trim().to_string();
    if stream_url.is_empty() {
        stream_url = item.url.trim().to_string();
    }
    if stream_url.is_empty() {
        return Err(AmazonError::plain("no stream URL in zarz amazon response"));
    }
    let mut key = item.audio.key.trim().to_string();
    if key.is_empty() {
        key = item.key.trim().to_string();
    }
    let mut key_specs = item.audio.key_specs.clone();
    if key_specs.is_empty() && !key.is_empty() {
        key_specs.push(key.clone());
    }
    let mut codec = item.audio.codec.trim().to_string();
    if codec.is_empty() {
        codec = item.codec.trim().to_string();
    }
    if codec.is_empty() {
        codec = requested_codec.to_string();
    }
    Ok(AmazonCommunityResponse {
        asin: asin.to_string(),
        codec,
        bit_depth: 0,
        url: stream_url.clone(),
        stream_url,
        key,
        key_specs,
        captcha: item.audio.captcha.clone(),
    })
}

fn zarz_http_error(status: u16, body: &[u8]) -> AmazonError {
    let parsed = serde_json::from_slice::<Value>(body).ok();
    let message = parsed
        .as_ref()
        .and_then(|value| value.get("error"))
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|text| !text.is_empty())
        .map(str::to_string);
    let text = message.unwrap_or_else(|| zarz_preview(body, 180));
    AmazonError::plain(format!("zarz HTTP {status}: {text}"))
}

fn zarz_preview(body: &[u8], max_len: usize) -> String {
    let preview = String::from_utf8_lossy(body).trim().to_string();
    if max_len > 0 && preview.len() > max_len {
        let mut end = max_len;
        while end > 0 && !preview.is_char_boundary(end) {
            end -= 1;
        }
        format!("{}...", &preview[..end])
    } else {
        preview
    }
}

pub fn amazon_remux_with_ffmpeg(
    input_path: &Path,
    output_path: &Path,
    target_ext: &str,
) -> Result<(), AmazonError> {
    let ffmpeg = get_ffmpeg_path()
        .map_err(|err| AmazonError::plain(format!("ffmpeg not found for remux: {err}")))?;
    validate_executable(&ffmpeg)
        .map_err(|err| AmazonError::plain(format!("invalid ffmpeg executable: {err}")))?;
    let input = input_path.to_string_lossy().into_owned();
    let output = output_path.to_string_lossy().into_owned();
    let mut args = vec![
        "-y".to_string(),
        "-i".to_string(),
        input.clone(),
        "-map".to_string(),
        "0:a:0".to_string(),
        "-vn".to_string(),
        "-c:a".to_string(),
        "copy".to_string(),
    ];
    if target_ext == ".m4a" {
        args.push("-f".to_string());
        args.push("mp4".to_string());
    }
    args.push(output.clone());
    if let Err((mut output_bytes, mut status)) = run_ffmpeg(&ffmpeg, &args) {
        if target_ext == ".flac" {
            let retry = vec![
                "-y".to_string(),
                "-i".to_string(),
                input,
                "-map".to_string(),
                "0:a:0".to_string(),
                "-vn".to_string(),
                "-c:a".to_string(),
                "flac".to_string(),
                output,
            ];
            match run_ffmpeg(&ffmpeg, &retry) {
                Ok(()) => return Ok(()),
                Err((bytes, retry_status)) => {
                    output_bytes = bytes;
                    status = retry_status;
                }
            }
        }
        let tail = tail_bytes(&output_bytes);
        return Err(AmazonError::plain(format!(
            "ffmpeg remux failed: {status}\nTail Output: {tail}"
        )));
    }
    Ok(())
}

fn run_ffmpeg(path: &Path, args: &[String]) -> Result<(), (Vec<u8>, String)> {
    let mut cmd = std::process::Command::new(path);
    cmd.args(args);
    configure_command(&mut cmd);
    let output = cmd.output().map_err(|err| (Vec::new(), err.to_string()))?;
    if output.status.success() {
        return Ok(());
    }
    let mut combined = output.stdout;
    combined.extend_from_slice(&output.stderr);
    let status = match output.status.code() {
        Some(code) => format!("exit status {code}"),
        None => "signal: killed".to_string(),
    };
    Err((combined, status))
}

fn tail_bytes(output: &[u8]) -> String {
    let start = output.len().saturating_sub(500);
    String::from_utf8_lossy(&output[start..]).into_owned()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU32, Ordering};
    use std::sync::Arc;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::{TcpListener, TcpStream};

    #[test]
    fn test_extract_amazon_track_asin() {
        assert_eq!(
            extract_amazon_track_asin("https://music.amazon.com/tracks/B09ABC1234?foo=bar"),
            Some("B09ABC1234".into())
        );
        assert_eq!(
            extract_amazon_track_asin("https://music.amazon.com/albums/B09ABC1234"),
            None
        );
    }

    #[test]
    fn test_normalize_amazon_quality() {
        assert_eq!(normalize_amazon_quality("CD"), "16");
        assert_eq!(normalize_amazon_quality("Lossless"), "16");
        assert_eq!(normalize_amazon_quality("Atmos"), "atmos");
        assert_eq!(normalize_amazon_quality("HiRes"), "24");
        assert_eq!(normalize_amazon_quality("anything-else"), "24");
    }

    #[test]
    fn test_is_transient_amazon_stream_error() {
        assert!(!is_transient_amazon_stream_error(None));
        assert!(is_transient_amazon_stream_error(Some(
            &AmazonError::deadline()
        )));
        let client_timeout = AmazonError::plain(
            "Get https://cdn.example: context deadline exceeded (Client.Timeout exceeded while awaiting headers)",
        );
        assert!(is_transient_amazon_stream_error(Some(&client_timeout)));
        let dns = AmazonError::net_timeout("lookup : timeout");
        assert!(is_transient_amazon_stream_error(Some(&dns)));
        let missing = AmazonError::plain("Amazon API returned status 404");
        assert!(!is_transient_amazon_stream_error(Some(&missing)));
        assert!(!is_transient_amazon_stream_error(Some(
            &AmazonError::plain("timed out")
        )));
        assert!(is_transient_amazon_stream_error(Some(&AmazonError::plain(
            "connection reset by peer"
        ))));
        assert!(is_transient_amazon_stream_error(Some(
            &AmazonError::stalled()
        )));
        let cancelled = AmazonError::cancelled("timeout");
        assert!(!is_transient_amazon_stream_error(Some(&cancelled)));
    }

    #[test]
    fn test_amazon_stream_client_timeout() {
        let client = amazon_stream_http_client();
        assert_eq!(client.timeout, MEDIA_DOWNLOAD_TIMEOUT);
        assert_eq!(client.response_header_timeout, MEDIA_HEADER_TIMEOUT);
        assert_eq!(MEDIA_DOWNLOAD_TIMEOUT, Duration::from_secs(30 * 60));
        assert_eq!(MEDIA_HEADER_TIMEOUT, Duration::from_secs(30));
        let downloader = AmazonDownloader::new();
        assert_eq!(
            downloader.config.community_url,
            "https://amz-oss.spotbye.qzz.io/api/dl"
        );
        assert_eq!(
            downloader.config.zarz_url,
            "https://api.zarz.moe/v2/dl/amazeamazeamaze"
        );
        assert_eq!(downloader.config.api_timeout, Duration::from_secs(120));
        assert_eq!(downloader.config.retry_wait, Duration::from_secs(2));
        assert_eq!(downloader.config.stream_retries, 3);
        assert_eq!(downloader.config.stream_timeout, MEDIA_DOWNLOAD_TIMEOUT);
        assert_eq!(downloader.config.idle_timeout, Duration::from_secs(25));
        assert_eq!(downloader.config.connect_timeout, Duration::from_secs(15));
        assert_eq!(downloader.config.app_version, "Unknown");
        assert_eq!(AMAZON_STREAM_TIMEOUT, Duration::from_secs(5 * 60));
        assert_eq!(amazon_music_api_base_url(), "https://amazon.spotbye.qzz.io");
        assert_eq!(
            amazon_community_download_url(),
            "https://amz-oss.spotbye.qzz.io/api/dl"
        );
        assert_eq!(map_amazon_quality_to_zarz_codec("Atmos"), "eac3");
        assert_eq!(map_amazon_quality_to_zarz_codec("24"), "flac");
        assert!(amazon_should_fallback_to_flac("atmos"));
        assert!(!amazon_should_fallback_to_flac("16"));
        assert_eq!(amazon_target_ext("atmos", "flac"), ".m4a");
        assert_eq!(amazon_target_ext("24", "ec-3"), ".m4a");
        assert_eq!(amazon_target_ext("24", "flac"), ".flac");
        assert_eq!(
            extract_amazon_gateway_asin("https://music.amazon.com/albums/b0test1234"),
            None
        );
        assert_eq!(
            extract_amazon_gateway_asin("https://music.amazon.com/albums/B0TEST1234"),
            Some("B0TEST1234".to_string())
        );
    }

    fn test_downloader(community_url: &str, zarz_url: &str) -> AmazonDownloader {
        AmazonDownloader::with_config(AmazonConfig {
            community_url: community_url.to_string(),
            zarz_url: zarz_url.to_string(),
            api_timeout: Duration::from_secs(2),
            stream_timeout: Duration::from_secs(2),
            response_header_timeout: Duration::from_millis(300),
            idle_timeout: Duration::from_millis(200),
            connect_timeout: Duration::from_millis(400),
            retry_wait: Duration::from_millis(1),
            rate_limit_pad: Duration::from_millis(1),
            gateway_retry_step: Duration::from_millis(1),
            zarz_timeout: Duration::from_secs(2),
            use_env_proxy: false,
            ..AmazonConfig::default()
        })
    }

    struct Captured {
        method: String,
        path: String,
        headers: std::collections::HashMap<String, String>,
        body: Vec<u8>,
    }

    enum Reply {
        Bytes {
            status: u16,
            headers: Vec<(String, String)>,
            body: Vec<u8>,
        },
        DelayHeaders {
            delay: Duration,
            body: Vec<u8>,
        },
        Stall,
    }

    async fn serve<F>(handler: F) -> std::net::SocketAddr
    where
        F: Fn(Captured) -> Reply + Send + Sync + 'static,
    {
        let listener = TcpListener::bind("127.0.0.1:0").await.expect("bind");
        let addr = listener.local_addr().expect("addr");
        tokio::spawn(async move {
            let handler = Arc::new(handler);
            loop {
                let Ok((sock, _)) = listener.accept().await else {
                    break;
                };
                let handler = handler.clone();
                tokio::spawn(async move {
                    let _ = handle_conn(sock, handler).await;
                });
            }
        });
        addr
    }

    async fn handle_conn<F>(mut sock: TcpStream, handler: Arc<F>) -> std::io::Result<()>
    where
        F: Fn(Captured) -> Reply + Send + Sync,
    {
        let Some(captured) = read_http(&mut sock).await? else {
            return Ok(());
        };
        match handler(captured) {
            Reply::Bytes {
                status,
                headers,
                body,
            } => write_http(&mut sock, status, &headers, &body).await?,
            Reply::DelayHeaders { delay, body } => {
                tokio::time::sleep(delay).await;
                write_http(&mut sock, 200, &[], &body).await?;
            }
            Reply::Stall => {
                sock.write_all(
                    b"HTTP/1.1 200 OK\r\nContent-Length: 64\r\nConnection: close\r\n\r\n",
                )
                .await?;
                tokio::time::sleep(Duration::from_secs(30)).await;
            }
        }
        Ok(())
    }

    async fn read_http(sock: &mut TcpStream) -> std::io::Result<Option<Captured>> {
        let mut buf = Vec::new();
        let mut tmp = [0u8; 2048];
        let header_end = loop {
            let n = sock.read(&mut tmp).await?;
            if n == 0 {
                return Ok(None);
            }
            buf.extend_from_slice(&tmp[..n]);
            if let Some(pos) = buf.windows(4).position(|w| w == b"\r\n\r\n") {
                break pos;
            }
            if buf.len() > 1_048_576 {
                return Ok(None);
            }
        };
        let head = String::from_utf8_lossy(&buf[..header_end]).into_owned();
        let mut lines = head.split("\r\n");
        let Some(req_line) = lines.next() else {
            return Ok(None);
        };
        let mut parts = req_line.split_whitespace();
        let Some(method) = parts.next() else {
            return Ok(None);
        };
        let Some(path) = parts.next() else {
            return Ok(None);
        };
        let mut headers = std::collections::HashMap::new();
        for line in lines {
            if let Some((key, value)) = line.split_once(':') {
                headers.insert(key.trim().to_ascii_lowercase(), value.trim().to_string());
            }
        }
        let length = headers
            .get("content-length")
            .and_then(|value| value.parse::<usize>().ok())
            .unwrap_or(0);
        let mut body = buf[header_end + 4..].to_vec();
        while body.len() < length {
            let n = sock.read(&mut tmp).await?;
            if n == 0 {
                break;
            }
            body.extend_from_slice(&tmp[..n]);
        }
        body.truncate(length);
        Ok(Some(Captured {
            method: method.to_string(),
            path: path.to_string(),
            headers,
            body,
        }))
    }

    async fn write_http(
        sock: &mut TcpStream,
        status: u16,
        headers: &[(String, String)],
        body: &[u8],
    ) -> std::io::Result<()> {
        let reason = match status {
            200 => "OK",
            302 => "Found",
            401 => "Unauthorized",
            404 => "Not Found",
            429 => "Too Many Requests",
            500 => "Internal Server Error",
            502 => "Bad Gateway",
            503 => "Service Unavailable",
            504 => "Gateway Timeout",
            _ => "Status",
        };
        let mut head = format!(
            "HTTP/1.1 {status} {reason}\r\nContent-Length: {}\r\nConnection: close\r\n",
            body.len()
        );
        for (key, value) in headers {
            head.push_str(key);
            head.push_str(": ");
            head.push_str(value);
            head.push_str("\r\n");
        }
        head.push_str("\r\n");
        sock.write_all(head.as_bytes()).await?;
        sock.write_all(body).await?;
        Ok(())
    }

    fn json_reply(status: u16, body: &str) -> Reply {
        Reply::Bytes {
            status,
            headers: vec![("Content-Type".to_string(), "application/json".to_string())],
            body: body.as_bytes().to_vec(),
        }
    }

    const ASIN_URL: &str = "https://music.amazon.com/albums/B0TEST1234";

    #[tokio::test]
    async fn test_download_from_gateway_rejects_missing_asin() {
        let dl = test_downloader("http://127.0.0.1:1", "http://127.0.0.1:1");
        let err = dl
            .download_from_gateway(
                "https://music.amazon.com/albums/not-an-asin",
                Path::new("."),
                "24",
                "",
            )
            .await
            .expect_err("asin");
        assert_eq!(
            err.to_string(),
            "failed to extract ASIN from URL: https://music.amazon.com/albums/not-an-asin"
        );
    }

    #[tokio::test]
    async fn test_fetch_amazon_community_response_headers_and_status() {
        let seen = Arc::new(std::sync::Mutex::new(Vec::new()));
        let seen_h = seen.clone();
        let addr = serve(move |req| {
            seen_h.lock().expect("lock").push(req);
            json_reply(404, "missing")
        })
        .await;
        let mut dl = test_downloader(&format!("http://{addr}/api/dl"), "http://127.0.0.1:1");
        dl.config.app_version = "1.2.3".to_string();
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "HiRes", "community")
            .await
            .expect_err("404");
        assert_eq!(err.to_string(), "Amazon API returned status 404");
        let reqs = seen.lock().expect("lock");
        assert_eq!(reqs.len(), 1);
        assert_eq!(reqs[0].method, "POST");
        assert_eq!(
            reqs[0].headers.get("user-agent").map(String::as_str),
            Some("Auralis/1.2.3")
        );
        assert_eq!(
            reqs[0].headers.get("accept").map(String::as_str),
            Some("application/json")
        );
        assert_eq!(
            reqs[0].headers.get("content-type").map(String::as_str),
            Some("application/json")
        );
        let parsed: Value = serde_json::from_slice(&reqs[0].body).expect("json");
        assert_eq!(parsed["id"], "B0TEST1234");
        assert_eq!(parsed["quality"], "24");
        assert_eq!(parsed["country"], "US");
        assert_eq!(
            std::str::from_utf8(&reqs[0].body).expect("utf8"),
            "{\"country\":\"US\",\"id\":\"B0TEST1234\",\"quality\":\"24\"}"
        );
    }

    #[tokio::test]
    async fn test_community_rate_limit_then_no_stream() {
        let hits = Arc::new(AtomicU32::new(0));
        let hits_h = hits.clone();
        let addr = serve(move |_req| {
            let n = hits_h.fetch_add(1, Ordering::SeqCst);
            if n == 0 {
                Reply::Bytes {
                    status: 429,
                    headers: vec![("Retry-After".to_string(), "0".to_string())],
                    body: Vec::new(),
                }
            } else {
                json_reply(200, "{}")
            }
        })
        .await;
        let dl = test_downloader(&format!("http://{addr}/api/dl"), "http://127.0.0.1:1");
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("url");
        assert_eq!(err.to_string(), "no stream URL found in response");
        assert!(hits.load(Ordering::SeqCst) >= 2);
    }

    #[tokio::test]
    async fn test_community_502_returns_last_error() {
        let hits = Arc::new(AtomicU32::new(0));
        let hits_h = hits.clone();
        let addr = serve(move |_req| {
            hits_h.fetch_add(1, Ordering::SeqCst);
            json_reply(502, "bad gateway")
        })
        .await;
        let mut dl = test_downloader(&format!("http://{addr}/api/dl"), "http://127.0.0.1:1");
        dl.config.community_max_retries = 1;
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("502");
        assert_eq!(err.to_string(), "Amazon community API returned 502");
        assert_eq!(hits.load(Ordering::SeqCst), 2);
    }

    #[tokio::test]
    async fn test_community_503_cooldown_message() {
        let addr = serve(|req| {
            if req.path.contains("empty") {
                Reply::Bytes {
                    status: 503,
                    headers: vec![],
                    body: b"{}".to_vec(),
                }
            } else if req.path.contains("minutes") {
                Reply::Bytes {
                    status: 503,
                    headers: vec![("Retry-After".to_string(), "61".to_string())],
                    body: b"{}".to_vec(),
                }
            } else if req.path.contains("prefixed") {
                json_reply(503, "{\"error\":\"This is normal and not a bug. already\"}")
            } else {
                json_reply(503, "{\"error\":\"maintenance\"}")
            }
        })
        .await;
        let dl = test_downloader(&format!("http://{addr}/api/dl"), "http://127.0.0.1:1");
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("503");
        assert_eq!(err.to_string(), "This is normal and not a bug. maintenance");
        let mut empty = test_downloader(&format!("http://{addr}/empty"), "http://127.0.0.1:1");
        empty.config.rate_limit_fallback = Duration::from_secs(30);
        let err = empty
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("empty");
        assert_eq!(
            err.to_string(),
            "This is normal and not a bug. The server is taking a scheduled short break. Please try again in about 1 minute(s)."
        );
        let minutes = test_downloader(&format!("http://{addr}/minutes"), "http://127.0.0.1:1");
        let err = minutes
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("minutes");
        assert_eq!(
            err.to_string(),
            "This is normal and not a bug. The server is taking a scheduled short break. Please try again in about 2 minute(s)."
        );
        let prefixed = test_downloader(&format!("http://{addr}/prefixed"), "http://127.0.0.1:1");
        let err = prefixed
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("prefixed");
        assert_eq!(err.to_string(), "This is normal and not a bug. already");
    }

    #[tokio::test]
    async fn test_community_401_is_status_error() {
        let hits = Arc::new(AtomicU32::new(0));
        let hits_h = hits.clone();
        let addr = serve(move |_req| {
            hits_h.fetch_add(1, Ordering::SeqCst);
            json_reply(401, "nope")
        })
        .await;
        let dl = test_downloader(&format!("http://{addr}/api/dl"), "http://127.0.0.1:1");
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("401");
        assert_eq!(err.to_string(), "Amazon API returned status 401");
        assert_eq!(hits.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn test_community_same_host_redirect_and_cross_host_stop() {
        let addr = serve(|req| {
            if req.path == "/api/dl" {
                Reply::Bytes {
                    status: 302,
                    headers: vec![(
                        "Location".to_string(),
                        format!(
                            "http://{}/done",
                            req.headers.get("host").cloned().unwrap_or_default()
                        ),
                    )],
                    body: Vec::new(),
                }
            } else {
                json_reply(200, "{}")
            }
        })
        .await;
        let dl = test_downloader(&format!("http://{addr}/api/dl"), "http://127.0.0.1:1");
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("followed");
        assert_eq!(err.to_string(), "no stream URL found in response");

        let other = serve(|_| json_reply(200, "{\"stream_url\":\"http://127.0.0.1:1/x\"}")).await;
        let cross = serve(move |_req| Reply::Bytes {
            status: 302,
            headers: vec![("Location".to_string(), format!("http://{other}/api/dl"))],
            body: Vec::new(),
        })
        .await;
        let dl = test_downloader(&format!("http://{cross}/api/dl"), "http://127.0.0.1:1");
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("cross");
        assert_eq!(err.to_string(), "Amazon API returned status 302");
    }

    #[tokio::test]
    async fn test_community_stops_after_10_redirects() {
        let hits = Arc::new(AtomicU32::new(0));
        let hits_h = hits.clone();
        let addr = serve(move |req| {
            hits_h.fetch_add(1, Ordering::SeqCst);
            let host = req
                .headers
                .get("host")
                .cloned()
                .unwrap_or_else(|| "127.0.0.1".to_string());
            let n = req
                .path
                .split('/')
                .next_back()
                .and_then(|v| v.parse::<u32>().ok())
                .unwrap_or(0);
            Reply::Bytes {
                status: 302,
                headers: vec![("Location".to_string(), format!("http://{host}/r/{}", n + 1))],
                body: Vec::new(),
            }
        })
        .await;
        let dl = test_downloader(&format!("http://{addr}/r/0"), "http://127.0.0.1:1");
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("redirects");
        let msg = err.to_string();
        assert!(msg.starts_with("Post \""), "{msg}");
        assert!(msg.contains("stopped after 10 redirects"), "{msg}");
        assert_eq!(hits.load(Ordering::SeqCst), 10);
    }

    #[tokio::test]
    async fn test_community_body_limit_and_decode_error() {
        let addr = serve(|req| {
            if req.path.contains("big") {
                Reply::Bytes {
                    status: 200,
                    headers: vec![],
                    body: vec![b'x'; (2 << 20) + 1],
                }
            } else {
                json_reply(200, "not-json")
            }
        })
        .await;
        let big = test_downloader(&format!("http://{addr}/big"), "http://127.0.0.1:1");
        let err = big
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("big");
        assert_eq!(err.to_string(), "response exceeds 2097152-byte limit");
        let bad = test_downloader(&format!("http://{addr}/bad"), "http://127.0.0.1:1");
        let err = bad
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("decode");
        assert!(
            err.to_string().starts_with("failed to decode response: "),
            "{}",
            err
        );
    }

    #[tokio::test]
    async fn test_source_community_does_not_call_zarz() {
        let zarz_hits = Arc::new(AtomicU32::new(0));
        let zarz_h = zarz_hits.clone();
        let zarz = serve(move |_req| {
            zarz_h.fetch_add(1, Ordering::SeqCst);
            json_reply(200, "[]")
        })
        .await;
        let community = serve(|_| json_reply(500, "no")).await;
        let dl = test_downloader(
            &format!("http://{community}/api/dl"),
            &format!("http://{zarz}/dl"),
        );
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "atmos", "community")
            .await
            .expect_err("community");
        assert_eq!(err.to_string(), "Amazon API returned status 500");
        assert_eq!(zarz_hits.load(Ordering::SeqCst), 0);
    }

    #[tokio::test]
    async fn test_empty_source_falls_back_to_zarz_then_flac() {
        let codecs = Arc::new(std::sync::Mutex::new(Vec::new()));
        let codecs_h = codecs.clone();
        let zarz = serve(move |req| {
            let parsed: Value = serde_json::from_slice(&req.body).unwrap_or(Value::Null);
            let codec = parsed["codec"].as_str().unwrap_or("").to_string();
            codecs_h.lock().expect("lock").push(codec.clone());
            if codec == "eac3" {
                json_reply(503, "{\"error\":\"busy\"}")
            } else {
                json_reply(200, "[]")
            }
        })
        .await;
        let community = serve(|_| json_reply(500, "down")).await;
        let dl = test_downloader(
            &format!("http://{community}/api/dl"),
            &format!("http://{zarz}/dl"),
        );
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "atmos", "")
            .await
            .expect_err("fallback");
        assert_eq!(err.to_string(), "zarz amazon returned an empty response");
        assert_eq!(
            codecs.lock().expect("lock").as_slice(),
            ["eac3".to_string(), "flac".to_string()]
        );
    }

    #[tokio::test]
    async fn test_zarz_source_does_not_flac_fallback() {
        let hits = Arc::new(AtomicU32::new(0));
        let hits_h = hits.clone();
        let zarz = serve(move |_req| {
            hits_h.fetch_add(1, Ordering::SeqCst);
            json_reply(500, "{\"error\":\"nope\"}")
        })
        .await;
        let dl = test_downloader("http://127.0.0.1:1", &format!("http://{zarz}/dl"));
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "atmos", "zarz")
            .await
            .expect_err("zarz");
        assert_eq!(err.to_string(), "zarz HTTP 500: nope");
        assert_eq!(hits.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn test_zarz_object_invalid_and_missing_url() {
        let zarz = serve(|req| {
            if req.path.ends_with("/object") {
                json_reply(200, "{\"url\":\"\",\"audio\":{\"codec\":\"flac\"}}")
            } else if req.path.ends_with("/bad") {
                json_reply(200, "not-json")
            } else if req.path.ends_with("/preview") {
                Reply::Bytes {
                    status: 404,
                    headers: vec![],
                    body: b"plain body".to_vec(),
                }
            } else {
                json_reply(200, "{\"audio\":{}}")
            }
        })
        .await;
        let object = test_downloader("http://127.0.0.1:1", &format!("http://{zarz}/object"));
        let err = object
            .download_from_gateway(ASIN_URL, Path::new("."), "16", "zarz")
            .await
            .expect_err("object");
        assert_eq!(err.to_string(), "no stream URL in zarz amazon response");
        let bad = test_downloader("http://127.0.0.1:1", &format!("http://{zarz}/bad"));
        let err = bad
            .download_from_gateway(ASIN_URL, Path::new("."), "16", "zarz")
            .await
            .expect_err("bad");
        assert!(
            err.to_string()
                .starts_with("invalid zarz amazon response: "),
            "{err}"
        );
        let preview = test_downloader("http://127.0.0.1:1", &format!("http://{zarz}/preview"));
        let err = preview
            .download_from_gateway(ASIN_URL, Path::new("."), "16", "zarz")
            .await
            .expect_err("preview");
        assert_eq!(err.to_string(), "zarz HTTP 404: plain body");
    }

    #[tokio::test]
    async fn test_stream_status_is_not_retried_and_captcha_is_sent() {
        let hits = Arc::new(AtomicU32::new(0));
        let hits_h = hits.clone();
        let saw_captcha = Arc::new(std::sync::Mutex::new(String::new()));
        let saw_h = saw_captcha.clone();
        let stream = serve(move |req| {
            hits_h.fetch_add(1, Ordering::SeqCst);
            *saw_h.lock().expect("lock") = req
                .headers
                .get("x-captcha-token")
                .cloned()
                .unwrap_or_default();
            assert_eq!(
                req.headers.get("user-agent").map(String::as_str),
                Some(DEFAULT_DOWNLOADER_USER_AGENT)
            );
            json_reply(404, "gone")
        })
        .await;
        let community = serve(move |_req| {
            json_reply(
                200,
                &format!("{{\"stream_url\":\"http://{stream}/file\",\"captcha\":\" secret \"}}"),
            )
        })
        .await;
        let dl = test_downloader(&format!("http://{community}/api/dl"), "http://127.0.0.1:1");
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("stream");
        assert_eq!(err.to_string(), "Amazon stream returned status 404");
        assert!(!is_transient_amazon_stream_error(Some(&err)));
        assert_eq!(hits.load(Ordering::SeqCst), 1);
        assert_eq!(saw_captcha.lock().expect("lock").as_str(), "secret");
    }

    #[tokio::test]
    async fn test_stream_stall_and_header_timeout_retry() {
        let stalls = Arc::new(AtomicU32::new(0));
        let stalls_h = stalls.clone();
        let stall = serve(move |_req| {
            stalls_h.fetch_add(1, Ordering::SeqCst);
            Reply::Stall
        })
        .await;
        let community =
            serve(move |_req| json_reply(200, &format!("{{\"url\":\"http://{stall}/a\"}}"))).await;
        let mut dl = test_downloader(&format!("http://{community}/api/dl"), "http://127.0.0.1:1");
        dl.config.idle_timeout = Duration::from_millis(40);
        dl.config.stream_retries = 2;
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("stall");
        assert_eq!(err.to_string(), "download stalled");
        assert!(is_transient_amazon_stream_error(Some(&err)));
        assert_eq!(stalls.load(Ordering::SeqCst), 2);

        let delays = Arc::new(AtomicU32::new(0));
        let delays_h = delays.clone();
        let slow = serve(move |_req| {
            delays_h.fetch_add(1, Ordering::SeqCst);
            Reply::DelayHeaders {
                delay: Duration::from_millis(800),
                body: b"late".to_vec(),
            }
        })
        .await;
        let community =
            serve(move |_req| json_reply(200, &format!("{{\"url\":\"http://{slow}/a\"}}"))).await;
        let mut dl = test_downloader(&format!("http://{community}/api/dl"), "http://127.0.0.1:1");
        dl.config.response_header_timeout = Duration::from_millis(50);
        dl.config.stream_retries = 2;
        let err = dl
            .download_from_gateway(ASIN_URL, Path::new("."), "24", "community")
            .await
            .expect_err("header");
        assert_eq!(
            err.to_string(),
            "net/http: timeout awaiting response headers"
        );
        assert!(is_transient_amazon_stream_error(Some(&err)));
        assert_eq!(delays.load(Ordering::SeqCst), 2);
    }

    #[tokio::test]
    async fn test_gateway_decrypts_and_removes_temps() {
        let bytes = std::fs::read(
            Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/amazon/cenc16.enc.mp4"),
        )
        .expect("fixture");
        let stream = serve(move |_req| Reply::Bytes {
            status: 200,
            headers: vec![],
            body: bytes.clone(),
        })
        .await;
        let community = serve(move |_req| {
            json_reply(
                200,
                &format!(
                    "{{\"stream_url\":\"http://{stream}/a\",\"key\":\"not-a-key\",\"key_specs\":[\"00112233445566778899aabbccddeeff\"],\"codec\":\"flac\"}}"
                ),
            )
        })
        .await;
        let dl = test_downloader(&format!("http://{community}/api/dl"), "http://127.0.0.1:1");
        let dir = tempfile::tempdir().expect("temp");
        let result = dl
            .download_from_gateway(ASIN_URL, dir.path(), "24", "community")
            .await;
        assert!(!dir.path().join("B0TEST1234.encrypted.mp4").exists());
        assert!(!dir.path().join("B0TEST1234.decrypted.mp4").exists());
        match result {
            Ok(path) => {
                let meta = std::fs::metadata(&path).expect("final");
                assert!(meta.len() > 0);
            }
            Err(err) => {
                let msg = err.to_string();
                assert!(!msg.contains("mp4ff decryption failed"), "{msg}");
                assert!(
                    msg.starts_with("ffmpeg not found for remux:")
                        || msg.starts_with("invalid ffmpeg executable:")
                        || msg.starts_with("ffmpeg remux failed:"),
                    "{msg}"
                );
            }
        }
    }

    #[tokio::test]
    async fn test_stream_recovers_after_one_stall() {
        let hits = Arc::new(AtomicU32::new(0));
        let hits_h = hits.clone();
        let stream = serve(move |_req| {
            let n = hits_h.fetch_add(1, Ordering::SeqCst);
            if n == 0 {
                Reply::Stall
            } else {
                Reply::Bytes {
                    status: 200,
                    headers: vec![],
                    body: b"not-audio".to_vec(),
                }
            }
        })
        .await;
        let community = serve(move |_req| {
            json_reply(
                200,
                &format!("{{\"url\":\"http://{stream}/a\",\"codec\":\"flac\"}}"),
            )
        })
        .await;
        let mut dl = test_downloader(&format!("http://{community}/api/dl"), "http://127.0.0.1:1");
        dl.config.idle_timeout = Duration::from_millis(40);
        let dir = tempfile::tempdir().expect("temp");
        let err = dl
            .download_from_gateway(ASIN_URL, dir.path(), "24", "community")
            .await
            .expect_err("remux");
        let msg = err.to_string();
        assert!(!msg.contains("download stalled"), "{msg}");
        assert!(hits.load(Ordering::SeqCst) >= 2);
        assert!(
            msg.starts_with("ffmpeg not found for remux:")
                || msg.starts_with("invalid ffmpeg executable:")
                || msg.starts_with("ffmpeg remux failed:"),
            "{msg}"
        );
        assert!(!dir.path().join("B0TEST1234.encrypted.mp4").exists());
    }
}

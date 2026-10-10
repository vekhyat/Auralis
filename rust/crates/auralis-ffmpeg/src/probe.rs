//! FFprobe audio metadata and stream inspection.

use crate::error::{FfmpegError, Result};
use crate::locator::get_ffprobe_path;
use crate::process::configure_command;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::Path;
use std::process::Command;

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct FormatInfo {
    pub filename: Option<String>,
    pub nb_streams: Option<u32>,
    pub format_name: Option<String>,
    pub format_long_name: Option<String>,
    pub duration: Option<String>,
    pub size: Option<String>,
    pub bit_rate: Option<String>,
    #[serde(default)]
    pub tags: HashMap<String, String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct StreamInfo {
    pub index: u32,
    pub codec_name: Option<String>,
    pub codec_long_name: Option<String>,
    pub codec_type: Option<String>,
    pub sample_rate: Option<String>,
    pub channels: Option<u32>,
    pub channel_layout: Option<String>,
    pub bits_per_raw_sample: Option<String>,
    pub duration: Option<String>,
    pub bit_rate: Option<String>,
    #[serde(default)]
    pub tags: HashMap<String, String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct ProbeOutput {
    #[serde(default)]
    pub streams: Vec<StreamInfo>,
    pub format: Option<FormatInfo>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MediaProbe {
    pub file_path: String,
    pub format_name: String,
    pub duration_secs: f64,
    pub size_bytes: u64,
    pub bit_rate_bps: u64,
    pub audio_codec: String,
    pub sample_rate: u32,
    pub channels: u32,
    pub bit_depth: u32,
    pub tags: HashMap<String, String>,
}

pub fn probe_file(path: &Path) -> Result<MediaProbe> {
    let ffprobe = get_ffprobe_path()?;

    let mut cmd = Command::new(ffprobe);
    cmd.args([
        "-v",
        "quiet",
        "-print_format",
        "json",
        "-show_format",
        "-show_streams",
    ]);
    cmd.arg(path);
    configure_command(&mut cmd);

    let output = cmd
        .output()
        .map_err(|e| FfmpegError::ExecutionFailed(format!("Failed to invoke ffprobe: {}", e)))?;

    if !output.status.success() {
        let err = String::from_utf8_lossy(&output.stderr);
        return Err(FfmpegError::ProbeFailed(format!(
            "ffprobe exited with error: {}",
            err
        )));
    }

    let parsed: ProbeOutput = serde_json::from_slice(&output.stdout)?;

    let format = parsed.format.unwrap_or_default();
    let audio_stream = parsed
        .streams
        .into_iter()
        .find(|s| s.codec_type.as_deref() == Some("audio"))
        .unwrap_or_default();

    let duration_secs = format
        .duration
        .as_deref()
        .or(audio_stream.duration.as_deref())
        .and_then(|d| d.parse::<f64>().ok())
        .unwrap_or(0.0);

    let size_bytes = format
        .size
        .as_deref()
        .and_then(|s| s.parse::<u64>().ok())
        .unwrap_or(0);

    let bit_rate_bps = format
        .bit_rate
        .as_deref()
        .or(audio_stream.bit_rate.as_deref())
        .and_then(|b| b.parse::<u64>().ok())
        .unwrap_or(0);

    let sample_rate = audio_stream
        .sample_rate
        .as_deref()
        .and_then(|s| s.parse::<u32>().ok())
        .unwrap_or(0);

    let bit_depth = audio_stream
        .bits_per_raw_sample
        .as_deref()
        .and_then(|b| b.parse::<u32>().ok())
        .unwrap_or(0);

    Ok(MediaProbe {
        file_path: path.display().to_string(),
        format_name: format.format_name.unwrap_or_default(),
        duration_secs,
        size_bytes,
        bit_rate_bps,
        audio_codec: audio_stream.codec_name.unwrap_or_default(),
        sample_rate,
        channels: audio_stream.channels.unwrap_or(0),
        bit_depth,
        tags: format.tags,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_probe_json() {
        let json_str = r#"{
            "streams": [
                {
                    "index": 0,
                    "codec_name": "flac",
                    "codec_type": "audio",
                    "sample_rate": "44100",
                    "channels": 2,
                    "bits_per_raw_sample": "16",
                    "duration": "180.5"
                }
            ],
            "format": {
                "format_name": "flac",
                "duration": "180.5",
                "size": "25000000",
                "bit_rate": "1107000",
                "tags": {
                    "TITLE": "Song Title",
                    "ARTIST": "Artist Name"
                }
            }
        }"#;

        let parsed: ProbeOutput = serde_json::from_str(json_str).unwrap();
        assert_eq!(parsed.streams.len(), 1);
        assert_eq!(parsed.streams[0].codec_name.as_deref(), Some("flac"));
        assert_eq!(parsed.format.unwrap().tags.get("TITLE").unwrap(), "Song Title");
    }
}

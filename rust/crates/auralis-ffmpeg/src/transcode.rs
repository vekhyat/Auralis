//! FFmpeg transcoding and ReplayGain measurement execution.

use crate::error::{FfmpegError, Result};
use crate::locator::get_ffmpeg_path;
use crate::process::configure_command;
use serde::{Deserialize, Serialize};
use std::path::Path;
use std::process::Command;

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct ReplayGainLoudnormStats {
    #[serde(rename = "input_i")]
    pub input_i: String,
    #[serde(rename = "input_tp")]
    pub input_tp: String,
    #[serde(rename = "input_lra")]
    pub input_lra: String,
    #[serde(rename = "input_thresh")]
    pub input_thresh: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct ReplayGainRawResult {
    pub integrated_loudness: f64,
    pub true_peak: f64,
    pub sample_peak: f64,
    pub loudness_range: f64,
    pub threshold: f64,
}

pub fn run_ffmpeg(args: &[&str]) -> Result<String> {
    let ffmpeg = get_ffmpeg_path()?;

    let mut cmd = Command::new(ffmpeg);
    cmd.args(args);
    configure_command(&mut cmd);

    let output = cmd
        .output()
        .map_err(|e| FfmpegError::ExecutionFailed(format!("Failed to invoke ffmpeg: {}", e)))?;

    if !output.status.success() {
        let err = String::from_utf8_lossy(&output.stderr);
        return Err(FfmpegError::ExecutionFailed(format!(
            "ffmpeg failed: {}",
            err
        )));
    }

    Ok(String::from_utf8_lossy(&output.stdout).to_string())
}

pub fn transcode_audio(input_path: &Path, output_path: &Path, codec_args: &[&str]) -> Result<()> {
    let ffmpeg = get_ffmpeg_path()?;

    let mut cmd = Command::new(ffmpeg);
    cmd.args(["-hide_banner", "-nostats", "-loglevel", "error", "-y", "-i"]);
    cmd.arg(input_path);
    cmd.args(codec_args);
    cmd.arg(output_path);
    configure_command(&mut cmd);

    let output = cmd
        .output()
        .map_err(|e| FfmpegError::ExecutionFailed(format!("Failed to run transcode: {}", e)))?;

    if !output.status.success() {
        let err = String::from_utf8_lossy(&output.stderr);
        return Err(FfmpegError::ExecutionFailed(format!(
            "Transcoding failed: {}",
            err
        )));
    }

    Ok(())
}

pub fn measure_replaygain(input_path: &Path) -> Result<ReplayGainRawResult> {
    let ffmpeg = get_ffmpeg_path()?;

    let filter = "asplit=2[rg][sample];[rg]loudnorm=I=-18:TP=-1:LRA=11:dual_mono=true:print_format=json[rgout];[sample]astats=metadata=0:reset=0:measure_perchannel=none:measure_overall=Peak_level[sampleout]";

    let mut cmd = Command::new(ffmpeg);
    cmd.args(["-hide_banner", "-nostats", "-i"]);
    cmd.arg(input_path);
    cmd.args([
        "-filter_complex",
        filter,
        "-map",
        "[rgout]",
        "-map",
        "[sampleout]",
        "-f",
        "null",
        "-",
    ]);
    configure_command(&mut cmd);

    let output = cmd.output().map_err(|e| {
        FfmpegError::ExecutionFailed(format!("Failed to run replaygain measurement: {}", e))
    })?;

    // loudnorm and astats print to stderr
    let stderr = String::from_utf8_lossy(&output.stderr);

    parse_replaygain_stderr(&stderr)
}

pub fn parse_replaygain_stderr(stderr: &str) -> Result<ReplayGainRawResult> {
    // Find loudnorm json object
    let start = stderr.rfind('{').ok_or_else(|| {
        FfmpegError::ExecutionFailed("Loudness statistics not found in ffmpeg output".into())
    })?;
    let end = stderr.rfind('}').ok_or_else(|| {
        FfmpegError::ExecutionFailed("Malformed loudness statistics in ffmpeg output".into())
    })?;

    if end <= start {
        return Err(FfmpegError::ExecutionFailed(
            "Invalid loudness statistics range".into(),
        ));
    }

    let json_slice = &stderr[start..=end];
    let stats: ReplayGainLoudnormStats = serde_json::from_str(json_slice)?;

    let integrated = stats.input_i.parse::<f64>().map_err(|e| {
        FfmpegError::ExecutionFailed(format!("Failed to parse input_i: {}", e))
    })?;
    let true_peak = stats.input_tp.parse::<f64>().map_err(|e| {
        FfmpegError::ExecutionFailed(format!("Failed to parse input_tp: {}", e))
    })?;
    let lra = stats.input_lra.parse::<f64>().map_err(|e| {
        FfmpegError::ExecutionFailed(format!("Failed to parse input_lra: {}", e))
    })?;
    let thresh = stats.input_thresh.parse::<f64>().map_err(|e| {
        FfmpegError::ExecutionFailed(format!("Failed to parse input_thresh: {}", e))
    })?;

    // Parse Peak level dB from astats: "Peak level dB: -0.1234"
    let re = regex::Regex::new(r"(?m)Peak level dB:\s+(-?(?:\d+(?:\.\d*)?|\.\d+)|-inf)")
        .map_err(|e| FfmpegError::ExecutionFailed(e.to_string()))?;

    let sample_peak = if let Some(caps) = re.captures_iter(stderr).last() {
        let val_str = caps.get(1).map(|m| m.as_str()).unwrap_or("-inf");
        if val_str.eq_ignore_ascii_case("-inf") {
            0.0
        } else {
            let peak_db = val_str.parse::<f64>().unwrap_or(0.0);
            10.0_f64.powf(peak_db / 20.0)
        }
    } else {
        10.0_f64.powf(true_peak / 20.0)
    };

    Ok(ReplayGainRawResult {
        integrated_loudness: integrated,
        true_peak,
        sample_peak,
        loudness_range: lra,
        threshold: thresh,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_replaygain_stderr() {
        let stderr = r#"
[Parsed_astats_2 @ 000001f] Channel: 1
[Parsed_astats_2 @ 000001f] Peak level dB: -0.500000
[Parsed_loudnorm_1 @ 000002f]
{
    "input_i" : "-16.50",
    "input_tp" : "-0.30",
    "input_lra" : "6.20",
    "input_thresh" : "-27.10"
}
"#;

        let result = parse_replaygain_stderr(stderr).unwrap();
        assert_eq!(result.integrated_loudness, -16.50);
        assert_eq!(result.true_peak, -0.30);
        assert_eq!(result.loudness_range, 6.20);
        assert_eq!(result.threshold, -27.10);
        assert!((result.sample_peak - 10.0_f64.powf(-0.5 / 20.0)).abs() < 1e-5);
    }
}

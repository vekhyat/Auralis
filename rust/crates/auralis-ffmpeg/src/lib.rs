//! FFmpeg and FFprobe binary management, probing, transcoding, and ReplayGain analysis.

pub mod download;
pub mod error;
pub mod locator;
pub mod process;
pub mod probe;
pub mod transcode;

pub use download::{download_and_install_binaries, extract_executable_from_zip, verify_archive_sha256};
pub use error::{FfmpegError, Result};
pub use locator::{
    get_ffmpeg_path, get_ffprobe_path, is_ffmpeg_installed, is_ffprobe_installed,
    run_version_check, validate_executable,
};
pub use probe::{probe_file, FormatInfo, MediaProbe, StreamInfo};
pub use transcode::{
    measure_replaygain, parse_replaygain_stderr, run_ffmpeg, transcode_audio,
    ReplayGainRawResult,
};

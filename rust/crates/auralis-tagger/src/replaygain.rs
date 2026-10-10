//! ReplayGain loudness calculation and tag formatting.

use crate::error::Result;
use auralis_ffmpeg::transcode::measure_replaygain;
use serde::{Deserialize, Serialize};
use std::path::Path;

pub const REPLAYGAIN_REFERENCE_LUFS: f64 = -18.0;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TrackReplayGain {
    pub file_path: String,
    pub track_gain_db: f64,
    pub track_peak: f64,
    pub integrated_loudness: f64,
    pub true_peak: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AlbumReplayGain {
    pub album_gain_db: f64,
    pub album_peak: f64,
    pub tracks: Vec<TrackReplayGain>,
}

pub fn analyze_track_replaygain(path: &Path) -> Result<TrackReplayGain> {
    let raw = measure_replaygain(path)?;

    // Gain relative to reference (-18 LUFS)
    let track_gain_db = REPLAYGAIN_REFERENCE_LUFS - raw.integrated_loudness;

    Ok(TrackReplayGain {
        file_path: path.display().to_string(),
        track_gain_db,
        track_peak: raw.sample_peak,
        integrated_loudness: raw.integrated_loudness,
        true_peak: raw.true_peak,
    })
}

pub fn calculate_album_replaygain(tracks: Vec<TrackReplayGain>) -> AlbumReplayGain {
    if tracks.is_empty() {
        return AlbumReplayGain {
            album_gain_db: 0.0,
            album_peak: 0.0,
            tracks,
        };
    }

    // Energy mean of integrated loudness
    let mut total_power = 0.0;
    let mut max_peak = 0.0_f64;

    for t in &tracks {
        total_power += 10.0_f64.powf(t.integrated_loudness / 10.0);
        if t.track_peak > max_peak {
            max_peak = t.track_peak;
        }
    }

    let mean_power = total_power / (tracks.len() as f64);
    let album_loudness = 10.0 * mean_power.log10();
    let album_gain_db = REPLAYGAIN_REFERENCE_LUFS - album_loudness;

    AlbumReplayGain {
        album_gain_db,
        album_peak: max_peak,
        tracks,
    }
}

pub fn format_gain_db(gain_db: f64) -> String {
    format!("{:+0.2} dB", gain_db)
}

pub fn format_peak(peak: f64) -> String {
    format!("{:0.6}", peak)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_format_gain_and_peak() {
        assert_eq!(format_gain_db(-2.345), "-2.35 dB");
        assert_eq!(format_gain_db(1.5), "+1.50 dB");
        assert_eq!(format_peak(0.9881234), "0.988123");
    }

    #[test]
    fn test_calculate_album_replaygain() {
        let t1 = TrackReplayGain {
            file_path: "track1.flac".into(),
            track_gain_db: -1.0,
            track_peak: 0.8,
            integrated_loudness: -17.0,
            true_peak: -0.2,
        };
        let t2 = TrackReplayGain {
            file_path: "track2.flac".into(),
            track_gain_db: -3.0,
            track_peak: 0.95,
            integrated_loudness: -15.0,
            true_peak: 0.0,
        };

        let album = calculate_album_replaygain(vec![t1, t2]);
        assert_eq!(album.album_peak, 0.95);
        assert!(album.album_gain_db < -1.0 && album.album_gain_db > -3.0);
    }
}

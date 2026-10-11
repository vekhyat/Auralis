use std::path::Path;
use std::process::Command;

pub fn make_audio_file(path: &Path, metadata: &[(&str, &str)]) {
    let mut cmd = Command::new("ffmpeg");
    cmd.args([
        "-hide_banner",
        "-loglevel",
        "error",
        "-f",
        "lavfi",
        "-i",
        "sine=frequency=440:duration=1:sample_rate=44100",
        "-y",
    ]);
    for (k, v) in metadata {
        cmd.args(["-metadata", &format!("{k}={v}")]);
    }
    cmd.arg(path);
    let output = cmd.output().expect("failed to execute ffmpeg");
    assert!(
        output.status.success(),
        "ffmpeg fixture failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    assert!(path.exists(), "fixture missing: {:?}", path);
}

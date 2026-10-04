package syncengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vekhyat/Auralis/backend"
)

// TranscodeCache converts lossless sources per FormatPolicy and caches the
// result keyed by (source hash, settings). Re-syncs hit the cache.
type TranscodeCache struct {
	Dir string
}

func NewTranscodeCache(dir string) *TranscodeCache { return &TranscodeCache{Dir: dir} }

// Key returns the cache filename for a track under a policy.
func (c *TranscodeCache) Key(track *SourceTrack, policy FormatPolicy) string {
	sum := sha256.Sum256([]byte(track.Hash + "|" + policy.SettingsKey()))
	return hex.EncodeToString(sum[:16]) + policy.RemoteExt(track.Ext)
}

// CachedPath returns the cached output path if present and newer than the source.
func (c *TranscodeCache) CachedPath(track *SourceTrack, policy FormatPolicy) (string, bool) {
	p := filepath.Join(c.Dir, c.Key(track, policy))
	info, err := os.Stat(p)
	if err != nil || info.Size() == 0 {
		return "", false
	}
	return p, true
}

// Materialize returns the file to upload for a track: the cached transcode
// when one exists, otherwise runs FFmpeg (or copies the original when the
// policy keeps it) and caches the result.
func (c *TranscodeCache) Materialize(ctx context.Context, track *SourceTrack, policy FormatPolicy) (string, error) {
	if !policy.NeedsTranscode(track.Ext) {
		return track.Path, nil
	}
	if p, ok := c.CachedPath(track, policy); ok {
		return p, nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(c.Dir, ".transcode-*"+policy.RemoteExt(track.Ext))
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	if err := transcodeTo(ctx, track.Path, tmpName, policy); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	final := filepath.Join(c.Dir, c.Key(track, policy))
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return final, nil
}

// transcodeTo runs FFmpeg with the right codec for the policy and carries
// tags, cover art, and lyrics over from the source.
func transcodeTo(ctx context.Context, src, dst string, policy FormatPolicy) error {
	ffmpeg, err := backend.GetFFmpegPath()
	if err != nil {
		return err
	}
	if err := backend.ValidateExecutable(ffmpeg); err != nil {
		return err
	}
	args := []string{"-hide_banner", "-nostats", "-i", src, "-map", "0:a", "-map", "0:v?", "-map_metadata", "0", "-y"}
	switch policy.Mode {
	case "aac":
		args = append(args, "-c:a", "aac", "-b:a", "256k", "-c:v", "copy", "-f", "ipod", dst)
	case "opus":
		args = append(args, "-c:a", "libopus", "-b:a", "160k", "-c:v", "copy", dst)
	case "mp3":
		args = append(args, "-c:a", "libmp3lame", "-q:a", "0", "-id3v2_version", "3", "-c:v", "copy", dst)
	default:
		return fmt.Errorf("unsupported transcode mode %q", policy.Mode)
	}
	cmd := execCmd(ctx, ffmpeg, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(dst)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		text := strings.TrimSpace(string(out))
		if len(text) > 400 {
			text = text[len(text)-400:]
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, text)
	}
	return nil
}

// execCmd binds the command to ctx so cancellation kills FFmpeg.
func execCmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

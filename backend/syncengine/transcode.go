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
	"go.senan.xyz/taglib"
)

// TranscodeCache converts lossless sources per FormatPolicy and caches the
// result keyed by (source hash, settings). Re-syncs hit the cache.
type TranscodeCache struct {
	Dir string
}

func NewTranscodeCache(dir string) *TranscodeCache { return &TranscodeCache{Dir: dir} }

// Key returns the cache filename for a track under a policy. The key binds
// the source content hash, so a re-tag or re-encode (new hash) never hits a
// stale cache entry. The extension is track-aware so ALAC-in-".m4a" keys
// under the transcoded name.
func (c *TranscodeCache) Key(track *SourceTrack, policy FormatPolicy) string {
	sum := sha256.Sum256([]byte(track.Hash + "|" + policy.SettingsKey()))
	return hex.EncodeToString(sum[:16]) + policy.RemoteExtForTrack(track)
}

// CachedPath returns the cached output path when a previous transcode for
// the same (hash, settings) exists. Freshness needs no mtime check: the
// key already contains the source hash, so changed sources map to
// different keys.
func (c *TranscodeCache) CachedPath(track *SourceTrack, policy FormatPolicy) (string, bool) {
	if track.Hash == "" {
		return "", false
	}
	p := filepath.Join(c.Dir, c.Key(track, policy))
	info, err := os.Stat(p)
	if err != nil || info.Size() == 0 {
		return "", false
	}
	return p, true
}

// Materialize returns the file to upload for a track: the cached transcode
// when one exists, otherwise runs FFmpeg (or copies the original when the
// policy keeps it) and caches the result. It honours ctx cancellation,
// which kills the FFmpeg child. Tracks without a content hash are rejected
// so two different files can never share one cache key. Losslessness is
// decided per track (flag/codec before extension) so ALAC-in-".m4a" is
// converted like any other lossless source.
func (c *TranscodeCache) Materialize(ctx context.Context, track *SourceTrack, policy FormatPolicy) (string, error) {
	if !policy.NeedsTranscodeTrack(track) {
		return track.Path, nil
	}
	if track.Hash == "" {
		return "", fmt.Errorf("transcode %s: missing content hash", track.Path)
	}
	if p, ok := c.CachedPath(track, policy); ok {
		return p, nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(c.Dir, ".transcode-*"+policy.RemoteExtForTrack(track))
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
	// Cover art is dropped here and copied with taglib afterwards: the Ogg
	// muxer rejects an attached image stream, and MP4 needs it flagged as
	// cover art, so one path that works for every format is simpler.
	args := []string{"-hide_banner", "-nostats", "-i", src, "-map", "0:a", "-vn", "-map_metadata", "0", "-y"}
	switch policy.Mode {
	case "aac":
		args = append(args, "-c:a", "aac", "-b:a", "256k", "-f", "ipod", dst)
	case "opus":
		args = append(args, "-c:a", "libopus", "-b:a", "160k", "-f", "ogg", dst)
	case "mp3":
		args = append(args, "-c:a", "libmp3lame", "-q:a", "0", "-id3v2_version", "3", "-f", "mp3", dst)
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
	// A missing source image is not worth failing the sync over, but a
	// failed WriteImage must fail the transcode: silently continuing
	// would publish a file that pretends to carry preserved cover art.
	if cover, err := taglib.ReadImage(src); err == nil && len(cover) > 0 {
		if err := taglib.WriteImage(dst, cover); err != nil {
			_ = os.Remove(dst)
			return fmt.Errorf("preserve cover art: %w", err)
		}
	}
	return nil
}

// execCmd binds the command to ctx so cancellation kills FFmpeg, and hides
// the console window Windows would otherwise flash for every track.
func execCmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	return cmd
}

package syncengine

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vekhyat/Auralis/backend"
	"go.senan.xyz/taglib"
)

// TestTranscodeKeepsTagsAndCover runs the real FFmpeg when one is installed.
func TestTranscodeKeepsTagsAndCover(t *testing.T) {
	ffmpeg, err := backend.GetFFmpegPath()
	if err != nil || backend.ValidateExecutable(ffmpeg) != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src.flac")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "flac", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("cannot generate fixture: %v %s", err, out)
	}
	if err := taglib.WriteTags(src, map[string][]string{"TITLE": {"Tone"}, "ARTIST": {"Synth"}}, 0); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var cover bytes.Buffer
	if err := jpeg.Encode(&cover, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := taglib.WriteImage(src, cover.Bytes()); err != nil {
		t.Fatal(err)
	}

	for _, mode := range []string{"aac", "opus", "mp3"} {
		t.Run(mode, func(t *testing.T) {
			policy := FormatPolicy{Mode: mode}
			dst := filepath.Join(dir, "out"+policy.RemoteExt(".flac"))
			if err := transcodeTo(context.Background(), src, dst, policy); err != nil {
				t.Fatal(err)
			}
			if info, err := os.Stat(dst); err != nil || info.Size() == 0 {
				t.Fatalf("no output: %v", err)
			}
			tags, err := taglib.ReadTags(dst)
			if err != nil {
				t.Fatal(err)
			}
			if len(tags[taglib.Title]) == 0 || tags[taglib.Title][0] != "Tone" {
				t.Fatalf("title lost: %v", tags)
			}
			if got, err := taglib.ReadImage(dst); err != nil || len(got) == 0 {
				t.Fatalf("cover lost: %v", err)
			}
		})
	}
}

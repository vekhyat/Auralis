package library

import (
	"os"
	"os/exec"
	"testing"

	"github.com/vekhyat/Auralis/backend"
)

// TestMain isolates the app data dir so MoveLibraryIndexFile and friends
// never touch the real user profile.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "auralis-library-test-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	os.Setenv("AURALIS_APP_DIR", dir)
	os.Exit(m.Run())
}

// makeAudioFile generates a tiny real audio file with ffmpeg so taglib can
// read and write it. Tests skip when ffmpeg is unavailable.
func makeAudioFile(t *testing.T, path string, metadata map[string]string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		if p, pathErr := backend.GetFFmpegPath(); pathErr == nil {
			ffmpeg = p
		} else {
			t.Skip("ffmpeg unavailable")
		}
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1:sample_rate=44100", "-y"}
	for key, value := range metadata {
		args = append(args, "-metadata", key+"="+value)
	}
	args = append(args, path)
	cmd := exec.Command(ffmpeg, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture: %v %s", err, output)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture missing: %v", err)
	}
}

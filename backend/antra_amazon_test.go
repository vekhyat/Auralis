package backend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAntraAmazonResolvePassesGrantedKey(t *testing.T) {
	root := t.TempDir()
	t.Setenv(appDataDirEnv, filepath.Join(root, "app"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))

	const (
		spotify    = "https://open.spotify.com/track/2EqlS6tkEnglzr7tkKAAYD"
		asin       = "B07FSSGBJV"
		grantedKey = "00112233445566778899aabbccddeeff"
		apiKey     = "contract-test-key"
	)

	var srv *httptest.Server
	var sawResolve atomic.Bool
	var sawStream atomic.Bool
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/resolve":
			if r.Header.Get("X-API-Key") != apiKey {
				http.Error(w, "missing key", http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("url") != spotify {
				http.Error(w, "unexpected url", http.StatusBadRequest)
				return
			}
			sawResolve.Store(true)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"streamUrl":     srv.URL + "/audio",
				"decryptionKey": grantedKey,
				"asin":          asin,
				"codec":         "flac",
			})
		case "/audio":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte{
				0, 0, 0, 0x18,
				'f', 't', 'y', 'p',
				'i', 's', 'o', 'm',
				0, 0, 0, 0,
				'i', 's', 'o', 'm',
				'm', 'p', '4', '1',
			})
			_, _ = w.Write([]byte("not-decodable-ciphertext"))
		default:
			if strings.HasPrefix(r.URL.Path, "/api/stream") {
				sawStream.Store(true)
			}
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	manifest := antraEndpointManifest{APIKey: apiKey}
	manifest.Mirrors.Amazon = srv.URL
	defer seedAntraManifest(manifest)()

	t.Run("decrypt failure does not publish ciphertext", func(t *testing.T) {
		restoreDecrypt := swapAntraAmazonDecrypt(func(keySpecs []string, _, _ string) error {
			if len(keySpecs) != 1 || keySpecs[0] != grantedKey {
				return errUnexpectedKeySpec(len(keySpecs))
			}
			return errStubDecrypt
		})
		defer restoreDecrypt()

		dir := isolatedDownloadDir(t)
		_, err := NewAmazonDownloader().downloadFromAntraMirror(spotify, dir, "16")
		if err == nil {
			t.Fatal("expected decrypt failure")
		}
		if strings.Contains(err.Error(), grantedKey) {
			t.Fatal("error included the granted key")
		}
		if !strings.Contains(err.Error(), "protected stream was not readable audio") {
			t.Fatalf("error: %v", err)
		}
		assertNoAudioLeft(t, dir)
	})

	t.Run("granted key is remuxed to decodable flac", func(t *testing.T) {
		ffmpegPath, err := GetFFmpegPath()
		if err != nil {
			t.Fatalf("ffmpeg required: %v", err)
		}
		var got []string
		restoreDecrypt := swapAntraAmazonDecrypt(func(keySpecs []string, _, outputPath string) error {
			got = append([]string{}, keySpecs...)
			if len(keySpecs) != 1 || keySpecs[0] != grantedKey {
				return errUnexpectedKeySpec(len(keySpecs))
			}
			return writeToneFLAC(ffmpegPath, outputPath)
		})
		defer restoreDecrypt()

		dir := isolatedDownloadDir(t)
		path, err := NewAmazonDownloader().downloadFromAntraMirror(spotify, dir, "16")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0] != grantedKey {
			t.Fatalf("decrypt key specs = %d items", len(got))
		}
		if filepath.Base(path) != asin+".flac" {
			t.Fatalf("final name: %s", filepath.Base(path))
		}
		if _, statErr := os.Stat(filepath.Join(dir, asin+".m4a")); statErr == nil {
			t.Fatal("ciphertext m4a was published")
		}
		assertFullAudioDecode(t, ffmpegPath, path)
		leftovers, _ := filepath.Glob(filepath.Join(dir, "*.decrypted.mp4"))
		if len(leftovers) != 0 {
			t.Fatalf("decrypted temp left behind: %d", len(leftovers))
		}
	})

	if !sawResolve.Load() {
		t.Fatal("resolve was not called")
	}
	if sawStream.Load() {
		t.Fatal("amazon mirror has no /api/stream")
	}
}

func TestAntraAmazonMirrorFullDecode(t *testing.T) {
	if os.Getenv("AURALIS_ANTRA_AMAZON_DECODE") != "1" {
		t.Skip("set AURALIS_ANTRA_AMAZON_DECODE=1 to decode one granted-key Amazon mirror file")
	}
	root := t.TempDir()
	t.Setenv(appDataDirEnv, filepath.Join(root, "app"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	outDir := filepath.Join(root, "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}
	path, err := NewAmazonDownloader().downloadFromAntraMirror(
		"https://open.spotify.com/track/2EqlS6tkEnglzr7tkKAAYD",
		outDir,
		"16",
	)
	if err != nil {
		t.Fatalf("amazon mirror: %s", antraRedactDetail(err.Error()))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 1024*1024 {
		t.Fatalf("output too small: %d bytes", info.Size())
	}
	ffmpegPath, err := GetFFmpegPath()
	if err != nil {
		t.Fatalf("ffmpeg required: %v", err)
	}
	assertFullAudioDecode(t, ffmpegPath, path)
	t.Logf("decoded %s (%d bytes)", filepath.Base(path), info.Size())
}

func seedAntraManifest(manifest antraEndpointManifest) func() {
	antraManifestMu.Lock()
	prevCache := antraManifestCache
	prevFetched := antraManifestFetched
	prevOK := antraManifestOK
	antraManifestCache = manifest
	antraManifestFetched = time.Now()
	antraManifestOK = true
	antraManifestMu.Unlock()
	return func() {
		antraManifestMu.Lock()
		antraManifestCache = prevCache
		antraManifestFetched = prevFetched
		antraManifestOK = prevOK
		antraManifestMu.Unlock()
	}
}

func swapAntraAmazonDecrypt(fn func(keySpecs []string, inputPath, outputPath string) error) func() {
	prev := antraAmazonDecrypt
	antraAmazonDecrypt = fn
	return func() { antraAmazonDecrypt = prev }
}

type stubError string

func (e stubError) Error() string { return string(e) }

const errStubDecrypt = stubError("mp4ff decryption failed: stub")

func errUnexpectedKeySpec(n int) error {
	return stubError(fmt.Sprintf("unexpected key spec count %d", n))
}

func isolatedDownloadDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(appDataDirEnv, filepath.Join(root, "app"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	dir := filepath.Join(root, "out")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeToneFLAC(ffmpegPath, outputPath string) error {
	cmd := exec.Command(ffmpegPath, "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.25:sample_rate=48000", "-c:a", "flac", outputPath)
	setHideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 300 {
			out = out[len(out)-300:]
		}
		return stubError("tone flac: " + string(out))
	}
	return nil
}

func assertFullAudioDecode(t *testing.T, ffmpegPath, path string) {
	t.Helper()
	cmd := exec.Command(ffmpegPath, "-v", "error", "-i", path, "-f", "null", "-")
	setHideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("full decode failed: %v %s", err, antraRedactDetail(string(out)))
	}
	if len(strings.TrimSpace(string(out))) != 0 {
		t.Fatalf("decode stderr: %s", antraRedactDetail(string(out)))
	}
}

func assertNoAudioLeft(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		switch filepath.Ext(name) {
		case ".m4a", ".flac", ".mp4", ".part":
			t.Fatalf("published leftover %s", entry.Name())
		}
		if strings.Contains(name, "decrypted") || strings.HasSuffix(name, ".part") {
			t.Fatalf("published leftover %s", entry.Name())
		}
	}
}

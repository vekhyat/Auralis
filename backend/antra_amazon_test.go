package backend

import (
	"encoding/json"
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

func TestAntraTextSearchContracts(t *testing.T) {
	path, query, err := antraTextSearchRequest("tidal", "Come Together", "The Beatles")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/search/" || query.Get("s") != "Come Together The Beatles" || query.Get("q") != "" {
		t.Fatalf("tidal search: %s %v", path, query)
	}

	path, query, err = antraTextSearchRequest("qobuz", "Come Together", "The Beatles")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/search" || query.Get("title") != "Come Together" || query.Get("artist") != "The Beatles" || query.Get("limit") != "5" || query.Get("q") != "" {
		t.Fatalf("qobuz search: %s %v", path, query)
	}

	path, query, err = antraTextSearchRequest("deezer", "Come Together", "The Beatles")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/search" || query.Get("title") != "Come Together" || query.Get("artist") != "The Beatles" {
		t.Fatalf("deezer search: %s %v", path, query)
	}

	path, query, err = antraTextSearchRequest("apple", "Come Together", "The Beatles")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/search" || query.Get("title") != "Come Together" || query.Get("limit") != "" || query.Get("q") != "" {
		t.Fatalf("apple search: %s %v", path, query)
	}

	if _, _, err = antraTextSearchRequest("amazon", "Come Together", "The Beatles"); err == nil {
		t.Fatal("amazon text search should be rejected")
	}
}

func TestAntraAmazonResolvePassesGrantedKey(t *testing.T) {
	const (
		spotify    = "https://open.spotify.com/track/2EqlS6tkEnglzr7tkKAAYD"
		asin       = "B07FSSGBJV"
		grantedKey = "00112233445566778899aabbccddeeff"
	)
	dir := isolatedDownloadDir(t)
	var sawAPIStream atomic.Bool
	srv := amazonResolveServer(t, spotify, asin, grantedKey, "flac", nil, &sawAPIStream)
	restore := seedAntraAmazon(t, srv.URL)
	defer restore()

	var got []string
	restoreDecrypt := swapAntraAmazonDecrypt(func(keySpecs []string, _, outputPath string) error {
		got = append([]string{}, keySpecs...)
		return os.WriteFile(outputPath, []byte("decoded-audio"), 0644)
	})
	defer restoreDecrypt()
	restoreRemux := swapAntraAmazonRemux(func(_, outputPath, _ string) error {
		return os.WriteFile(outputPath, []byte("remuxed-audio"), 0644)
	})
	defer restoreRemux()

	path, err := NewAmazonDownloader().downloadFromAntraMirror(spotify, dir, "16")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != grantedKey {
		t.Fatalf("decrypt saw %d key specs", len(got))
	}
	if filepath.Base(path) != asin+".flac" {
		t.Fatalf("final name: %s", filepath.Base(path))
	}
	if _, statErr := os.Stat(filepath.Join(dir, asin+".m4a")); statErr == nil {
		t.Fatal("ciphertext m4a was published")
	}
	if sawAPIStream.Load() {
		t.Fatal("amazon mirror has no /api/stream")
	}
}

func TestAntraAmazonRemuxDecodesWhenFFmpegPresent(t *testing.T) {
	isolatedDownloadDir(t)
	ffmpegPath, err := GetFFmpegPath()
	if err != nil {
		t.Skipf("ffmpeg not available: %v", err)
	}
	const (
		spotify    = "https://open.spotify.com/track/2EqlS6tkEnglzr7tkKAAYD"
		asin       = "B07FSSGBJV"
		grantedKey = "00112233445566778899aabbccddeeff"
	)
	dir := isolatedDownloadDir(t)
	srv := amazonResolveServer(t, spotify, asin, grantedKey, "flac", nil, nil)
	restore := seedAntraAmazon(t, srv.URL)
	defer restore()
	restoreDecrypt := swapAntraAmazonDecrypt(func(_ []string, _, outputPath string) error {
		return writeToneFLAC(ffmpegPath, outputPath)
	})
	defer restoreDecrypt()

	path, err := NewAmazonDownloader().downloadFromAntraMirror(spotify, dir, "16")
	if err != nil {
		t.Fatal(err)
	}
	assertFullAudioDecode(t, ffmpegPath, path)
}

func TestAntraAmazonMalformedKeyDoesNotLeak(t *testing.T) {
	const secret = "SUPERSECRETKEYVALUE"
	dir := isolatedDownloadDir(t)
	srv := amazonResolveServer(t, "https://open.spotify.com/track/2EqlS6tkEnglzr7tkKAAYD", "B07FSSGBJV", "zz-not-kid:"+secret, "flac", nil, nil)
	restore := seedAntraAmazon(t, srv.URL)
	defer restore()
	restoreDecrypt := swapAntraAmazonDecrypt(decryptWithMP4FF)
	defer restoreDecrypt()

	_, err := NewAmazonDownloader().downloadFromAntraMirror("https://open.spotify.com/track/2EqlS6tkEnglzr7tkKAAYD", dir, "16")
	if err == nil {
		t.Fatal("expected decrypt failure")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "zz-not-kid") {
		t.Fatal("decrypt error included the granted key")
	}
	if !strings.Contains(err.Error(), "protected stream was not readable audio") {
		t.Fatalf("error: %v", err)
	}
	assertNoAudioLeft(t, dir)
}

func TestAntraAmazonTrackRejectsAudioBody(t *testing.T) {
	ftyp := []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	t.Run("audio content type", func(t *testing.T) {
		dir := isolatedDownloadDir(t)
		srv := amazonAudioTrackServer(t, "audio/mp4", []byte("ftyp-this-is-not-the-full-audio-file"))
		restore := seedAntraAmazon(t, srv.URL)
		defer restore()

		_, err := antraAmazonTrackToFile("B07FSSGBJV", filepath.Join(dir, "B07FSSGBJV.flac"), nil)
		if err == nil {
			t.Fatal("expected audio response to be rejected")
		}
		assertNoAudioLeft(t, dir)
	})

	t.Run("json labeled audio", func(t *testing.T) {
		dir := isolatedDownloadDir(t)
		srv := amazonAudioTrackServer(t, "application/json", ftyp)
		restore := seedAntraAmazon(t, srv.URL)
		defer restore()

		_, err := antraAmazonTrackToFile("B07FSSGBJV", filepath.Join(dir, "B07FSSGBJV.flac"), nil)
		if err == nil {
			t.Fatal("expected labeled audio to be rejected")
		}
		assertNoAudioLeft(t, dir)
	})

	t.Run("asin route", func(t *testing.T) {
		dir := isolatedDownloadDir(t)
		srv := amazonAudioTrackServer(t, "audio/mp4", ftyp)
		restore := seedAntraAmazon(t, srv.URL)
		defer restore()

		_, err := NewAmazonDownloader().downloadFromAntraMirror("https://music.amazon.com/tracks/B07FSSGBJV", dir, "16")
		if err == nil {
			t.Fatal("expected asin audio response to be rejected")
		}
		assertNoAudioLeft(t, dir)
	})
}

func amazonAudioTrackServer(t *testing.T, contentType string, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/track/B07FSSGBJV" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-API-Key") == "" {
			http.Error(w, "missing key", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAntraAmazonAtmosRejectsNonSpatialCodec(t *testing.T) {
	for _, codec := range []string{"flac", "alac", "stereo"} {
		t.Run(codec, func(t *testing.T) {
			dir := isolatedDownloadDir(t)
			var sawCDN atomic.Bool
			srv := amazonResolveServer(t, "https://open.spotify.com/track/2EqlS6tkEnglzr7tkKAAYD", "B07FSSGBJV", "00112233445566778899aabbccddeeff", codec, &sawCDN, nil)
			restore := seedAntraAmazon(t, srv.URL)
			defer restore()

			_, err := NewAmazonDownloader().downloadFromAntraMirror("https://open.spotify.com/track/2EqlS6tkEnglzr7tkKAAYD", dir, "atmos")
			if err == nil || !strings.Contains(err.Error(), "did not return atmos") {
				t.Fatalf("error: %v", err)
			}
			if sawCDN.Load() {
				t.Fatal("downloaded a non-atmos stream")
			}
			assertNoAudioLeft(t, dir)
		})
	}
}

func amazonResolveServer(t *testing.T, spotify, asin, grantedKey, codec string, sawCDN, sawAPIStream *atomic.Bool) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/resolve":
			if r.Header.Get("X-API-Key") == "" {
				http.Error(w, "missing key", http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("url") != spotify {
				http.Error(w, "unexpected url", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"streamUrl":     srv.URL + "/cdn/audio",
				"decryptionKey": grantedKey,
				"asin":          asin,
				"codec":         codec,
			})
		case "/cdn/audio":
			if sawCDN != nil {
				sawCDN.Store(true)
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte{
				0, 0, 0, 0x18,
				'f', 't', 'y', 'p',
				'i', 's', 'o', 'm',
				0, 0, 0, 0,
				'i', 's', 'o', 'm',
				'm', 'p', '4', '1',
			})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/stream") && sawAPIStream != nil {
				sawAPIStream.Store(true)
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func seedAntraAmazon(t *testing.T, amazonBase string) func() {
	t.Helper()
	manifest := antraEndpointManifest{APIKey: "contract-test-key"}
	manifest.Mirrors.Amazon = amazonBase
	return seedAntraManifest(manifest)
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

func swapAntraAmazonRemux(fn func(inputPath, outputPath, targetExt string) error) func() {
	prev := antraAmazonRemux
	antraAmazonRemux = fn
	return func() { antraAmazonRemux = prev }
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
		return os.ErrInvalid
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

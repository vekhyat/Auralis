package backend

import (
	"crypto/cipher"
	"crypto/des"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/vekhyat/Auralis/backend/blowfish"
)

func TestParseAntraSearchHit(t *testing.T) {
	hit, err := parseAntraSearchHit([]byte(`{"track_id":"12345","title":"Come Together","artist":"The Beatles","duration_ms":259000,"isrc":"gbstu1110001"}`))
	if err != nil {
		t.Fatal(err)
	}
	if hit.TrackID != "12345" {
		t.Fatalf("track id: %q", hit.TrackID)
	}
	if hit.Title != "Come Together" {
		t.Fatalf("title: %q", hit.Title)
	}
	if hit.ISRC != "GBSTU1110001" {
		t.Fatalf("isrc: %q", hit.ISRC)
	}
	if hit.DurationMS != 259000 {
		t.Fatalf("duration: %d", hit.DurationMS)
	}
}

func TestParseAntraSearchHitNumericID(t *testing.T) {
	hit, err := parseAntraSearchHit([]byte(`{"id":9876543210,"title":"Something"}`))
	if err != nil {
		t.Fatal(err)
	}
	if hit.TrackID != "9876543210" {
		t.Fatalf("track id: %q", hit.TrackID)
	}
}

func TestExtractAppleTrackID(t *testing.T) {
	id := extractAppleTrackID("https://music.apple.com/us/song/come-together/1440833098")
	if id != "1440833098" {
		t.Fatalf("song path: %q", id)
	}
	id = extractAppleTrackID("https://music.apple.com/us/album/abbey-road/1440833098?i=1440833489")
	if id != "1440833489" {
		t.Fatalf("i= query: %q", id)
	}
}

func TestDeezerBlowfishRoundTrip(t *testing.T) {
	key := []byte("g4el58wc0zvf9na1")
	plain := make([]byte, deezerEncryptedChunk)
	for i := range plain {
		plain[i] = byte(i)
	}
	block, err := blowfish.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	iv := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	encrypted := append([]byte(nil), plain...)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, encrypted)
	payload := append(encrypted, []byte("plain-tail")...)
	out, err := decryptDeezerStream(payload, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(out[:len(plain)]) != string(plain) {
		t.Fatal("decrypted chunk mismatch")
	}
	if string(out[len(plain):]) != "plain-tail" {
		t.Fatalf("tail: %q", out[len(plain):])
	}
}

func TestDecryptJioSaavnURL(t *testing.T) {
	plain := []byte("https://aac.saavncdn.com/example_320.mp4")
	for len(plain)%des.BlockSize != 0 {
		plain = append(plain, byte(des.BlockSize-len(plain)%des.BlockSize))
	}
	block, err := des.NewCipher([]byte("38346591"))
	if err != nil {
		t.Fatal(err)
	}
	enc := make([]byte, len(plain))
	for i := 0; i < len(plain); i += des.BlockSize {
		block.Encrypt(enc[i:i+des.BlockSize], plain[i:i+des.BlockSize])
	}
	got, err := decryptJioSaavnURL(base64.StdEncoding.EncodeToString(enc))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "https://aac.saavncdn.com/example_320.mp4") {
		t.Fatalf("got %q", got)
	}
}

func TestJioSaavnQueriesPreferCleanTitle(t *testing.T) {
	queries := jioSaavnQueries("On Time (with John Legend)", "Huntrix, EJAE")
	if len(queries) == 0 {
		t.Fatal("no queries")
	}
	joined := strings.Join(queries, " | ")
	if !strings.Contains(strings.ToLower(joined), "on time") {
		t.Fatalf("missing clean title: %s", joined)
	}
}

func TestReplaceAudioExtension(t *testing.T) {
	got := replaceAudioExtension(`C:\Music\track.flac`, ".m4a")
	if !strings.HasSuffix(got, ".m4a") {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeDownloaderIncludesNewSources(t *testing.T) {
	if sanitizeDownloaderValue("deezer") != "deezer" {
		t.Fatal("deezer")
	}
	if sanitizeDownloaderValue("apple") != "apple" {
		t.Fatal("apple")
	}
	if sanitizeDownloaderValue("jiosaavn") != "jiosaavn" {
		t.Fatal("jiosaavn")
	}
}

func TestSanitizeAutoOrderKeepsExtraSources(t *testing.T) {
	got := sanitizeAutoOrderValue("tidal-qobuz-amazon-deezer")
	if got != "tidal-qobuz-amazon-deezer" {
		t.Fatalf("got %q", got)
	}
}

func TestAntraQualityQuery(t *testing.T) {
	tidalLossless := antraQualityQuery("tidal", "LOSSLESS")
	if tidalLossless.Get("prefer_16") != "1" || tidalLossless.Get("quality") != "" {
		t.Fatalf("tidal LOSSLESS: %v", tidalLossless)
	}
	tidalHiRes := antraQualityQuery("tidal", "HI_RES_LOSSLESS")
	if tidalHiRes.Get("strict_24") != "1" {
		t.Fatalf("tidal HI_RES_LOSSLESS: %v", tidalHiRes)
	}
	tidalAtmos := antraQualityQuery("tidal", "ATMOS")
	if tidalAtmos.Get("format") != "atmos" {
		t.Fatalf("tidal ATMOS: %v", tidalAtmos)
	}

	qobuz := antraQualityQuery("qobuz", "27")
	if qobuz.Get("strict_24") != "1" || qobuz.Get("quality") != "" {
		t.Fatalf("qobuz 24-bit: %v", qobuz)
	}
	if antraQualityQuery("qobuz", "6").Get("strict_24") != "" {
		t.Fatal("qobuz 16-bit must not force strict_24")
	}

	amazon := antraQualityQuery("amazon", "atmos")
	if amazon.Get("format") != "atmos" {
		t.Fatalf("amazon atmos format: %q", amazon.Get("format"))
	}
}

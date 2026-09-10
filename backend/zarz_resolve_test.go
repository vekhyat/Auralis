package backend

import (
	"strings"
	"testing"
)

func TestParseZarzResolveLinks(t *testing.T) {
	body := []byte(`{
		"success": true,
		"isrc": "USSM19902991",
		"songUrls": {
			"Tidal": "https://tidal.com/browse/track/1781887",
			"AmazonMusic": ["https://music.amazon.com/tracks/B00137QVBC"],
			"Qobuz": "https://open.qobuz.com/track/57854192",
			"Deezer": "https://deezer.com/track/13129199"
		}
	}`)
	links, err := parseZarzResolveBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if links.ISRC != "USSM19902991" {
		t.Fatalf("isrc=%q", links.ISRC)
	}
	if links.TidalURL == "" || links.AmazonURL == "" || links.QobuzURL == "" || links.DeezerURL == "" {
		t.Fatalf("missing platform urls: %+v", links)
	}
}

func TestParseZarzResolveRejectsFailure(t *testing.T) {
	_, err := parseZarzResolveBody([]byte(`{"success":false,"songUrls":{}}`))
	if err == nil {
		t.Fatal("expected error for success=false")
	}
}

func TestMergeZarzResolveDoesNotOverwriteExisting(t *testing.T) {
	dst := &resolvedTrackLinks{TidalURL: "https://listen.tidal.com/track/1"}
	src := &resolvedTrackLinks{
		TidalURL:  "https://listen.tidal.com/track/2",
		AmazonURL: "https://music.amazon.com/tracks/B00137QVBC",
		ISRC:      "USSM19902991",
	}
	mergeResolvedTrackLinks(dst, src)
	if dst.TidalURL != "https://listen.tidal.com/track/1" {
		t.Fatalf("tidal url overwritten: %s", dst.TidalURL)
	}
	if dst.AmazonURL == "" || dst.ISRC != "USSM19902991" {
		t.Fatalf("failed to fill missing fields: %+v", dst)
	}
}

func TestTidalPublicSearchPathUsesUSCatalog(t *testing.T) {
	path := tidalPublicSearchPath("USSM19902991", 8)
	if !strings.Contains(path, "https://tidal.com/v1/search/tracks") ||
		!strings.Contains(path, "query=USSM19902991") ||
		!strings.Contains(path, "countryCode=US") {
		t.Fatalf("unexpected tidal search path %q", path)
	}
}

func TestAmazonZarzFallsBackToFlacCodec(t *testing.T) {
	if mapAmazonQualityToZarzCodec("atmos") != "eac3" {
		t.Fatal("atmos should map to eac3")
	}
	if mapAmazonQualityToZarzCodec("16") != "flac" {
		t.Fatal("16-bit should map to flac")
	}
	if !amazonShouldFallbackToFlac("atmos") {
		t.Fatal("atmos should fallback to flac")
	}
	if amazonShouldFallbackToFlac("16") {
		t.Fatal("flac quality should not fallback again")
	}
}

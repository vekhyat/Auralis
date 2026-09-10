package backend

import (
	"strings"
	"testing"
)

func TestQobuzTitleSearchVariantsStripsMixYear(t *testing.T) {
	variants := qobuzTitleSearchVariants("Come Together - 2019 Mix")
	joined := strings.Join(variants, " | ")
	if len(variants) < 2 {
		t.Fatalf("expected stripped title variant, got %q", joined)
	}
	found := false
	for _, variant := range variants {
		if strings.EqualFold(variant, "Come Together") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing clean title, got %q", joined)
	}
}

func TestQobuzSearchQueriesPreferCleanTitle(t *testing.T) {
	queries := qobuzSearchQueries("", "Something - 2019 Mix", "The Beatles", "Abbey Road (2019 Mix)")
	found := false
	for _, query := range queries {
		if strings.EqualFold(query, "Something The Beatles") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected clean artist query, got %#v", queries)
	}
}

func TestLooksLikeQobuzReleaseVersion(t *testing.T) {
	if !looksLikeQobuzReleaseVersion("2019 Mix") {
		t.Fatal("2019 Mix should be treated as a version suffix")
	}
	if looksLikeQobuzReleaseVersion("Maxwell's Silver Hammer") {
		t.Fatal("a normal title should not look like a version suffix")
	}
}

func TestParseQobuzTrackIDFromURL(t *testing.T) {
	cases := map[string]int64{
		"https://open.qobuz.com/track/243937":                                                 243937,
		"https://www.qobuz.com/us-en/track/243937":                                            243937,
		"https://play.qobuz.com/track/580609?source=share":                                    580609,
		"https://www.qobuz.com/gb-en/album/toxicity/download-streaming-hi-res/00612427384027": 0,
	}
	for raw, want := range cases {
		got, err := parseQobuzTrackID(raw)
		if want == 0 {
			if err == nil {
				t.Fatalf("expected error for %s, got id=%d", raw, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		if got != want {
			t.Fatalf("parse %s: got %d want %d", raw, got, want)
		}
	}
}

func TestQobuzZarzSearchPathEncodesQuery(t *testing.T) {
	path := qobuzZarzSearchPath("track/search", "Prison Song", 10)
	if !strings.Contains(path, "/qbz/track/search?") {
		t.Fatalf("unexpected path %q", path)
	}
	if !strings.Contains(path, "query=Prison") || !strings.Contains(path, "limit=10") {
		t.Fatalf("missing query params in %q", path)
	}
}

func TestQobuzTrackFromIDUsesSpotifyMetadata(t *testing.T) {
	track := qobuzTrackFromID(12345, "Prison Song", "System Of A Down", "Toxicity", "USAB10102701")
	if track.ID != 12345 || track.Title != "Prison Song" || qobuzTrackDisplayArtist(*track) != "System Of A Down" {
		t.Fatalf("unexpected track: %+v", track)
	}
	if track.Album.Title != "Toxicity" || track.ISRC != "USAB10102701" {
		t.Fatalf("unexpected album/isrc: %+v", track)
	}
}

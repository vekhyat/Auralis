package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestJioSaavnOriginalArtistBeatsExactTitleCover(t *testing.T) {
	items := jioSaavnComeTogetherFixture()
	got, score := jioSaavnPickBest(items, jioSaavnWant{Title: "Come Together", Artist: "The Beatles"})
	if score <= 0 || got == nil {
		t.Fatal("expected a Beatles match, got no selection")
	}
	if jioSaavnString(got, "id") != "buPhYncP" {
		t.Fatalf("picked %q (%s), want Beatles id buPhYncP", jioSaavnString(got, "id"), jioSaavnItemTitle(got))
	}
}

func TestJioSaavnTitleOnlyFallbackStillUsesRequestedArtist(t *testing.T) {
	id, _, err := jioSaavnResultFromList(jioSaavnComeTogetherFixture(), jioSaavnWant{Title: "Come Together", Artist: "The Beatles"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "buPhYncP" {
		t.Fatalf("title-only request selected %s, want buPhYncP", id)
	}
	got, score := jioSaavnPickBest([]map[string]any{jioSaavnComeTogetherFixture()[0]}, jioSaavnWant{Title: "Come Together", Artist: "The Beatles"})
	if score != 0 || got != nil {
		t.Fatalf("title-only result list still selected cover %#v score %d", got, score)
	}
}

func TestJioSaavnWrongArtistOnlyIsNoMatch(t *testing.T) {
	items := []map[string]any{jioSaavnComeTogetherFixture()[0], jioSaavnComeTogetherFixture()[1]}
	got, score := jioSaavnPickBest(items, jioSaavnWant{Title: "Come Together", Artist: "The Beatles"})
	if score != 0 || got != nil {
		t.Fatalf("selected %#v score %d, want no match", got, score)
	}
	if _, _, err := jioSaavnResultFromList(items, jioSaavnWant{Title: "Come Together", Artist: "The Beatles"}); err == nil {
		t.Fatal("expected no close match error")
	}
}

func TestJioSaavnUnrelatedTitleIsNoMatch(t *testing.T) {
	items := []map[string]any{
		{
			"id":          "sun",
			"title":       "Here Comes the Sun (Remastered 2009)",
			"description": "The Beatles · Abbey Road (Remastered)",
			"more_info":   map[string]any{"primary_artists": "The Beatles", "singers": ""},
		},
		{
			"id":          "now",
			"title":       "Come Together Now",
			"description": "The Beatles · Let It Be",
			"more_info":   map[string]any{"primary_artists": "The Beatles"},
		},
		{
			"id":        "karaoke",
			"title":     "Come Together (Karaoke Version)",
			"more_info": map[string]any{"primary_artists": "The Beatles"},
		},
	}
	got, score := jioSaavnPickBest(items, jioSaavnWant{Title: "Come Together", Artist: "The Beatles"})
	if score != 0 || got != nil {
		t.Fatalf("selected %q score %d, want no match", jioSaavnString(got, "id"), score)
	}
}

func TestJioSaavnVersionFallbackMatchesEitherSide(t *testing.T) {
	remaster := map[string]any{
		"id":          "remaster",
		"title":       "Come Together (Remastered 2009)",
		"description": "The Beatles · Abbey Road (Remastered)",
		"more_info":   map[string]any{"primary_artists": "The Beatles", "singers": ""},
	}
	plain := map[string]any{
		"id":          "plain",
		"title":       "Come Together",
		"description": "The Beatles · Abbey Road",
		"more_info":   map[string]any{"primary_artists": "The Beatles"},
	}
	if _, score := jioSaavnPickBest([]map[string]any{remaster}, jioSaavnWant{Title: "Come Together", Artist: "The Beatles"}); score <= 0 {
		t.Fatal("versioned title should match the plain request")
	}
	if _, score := jioSaavnPickBest([]map[string]any{plain}, jioSaavnWant{Title: "Come Together (Remastered 2009)", Artist: "Beatles"}); score <= 0 {
		t.Fatal("plain title should match a versioned request, including a leading The")
	}
	got, _ := jioSaavnPickBest([]map[string]any{plain, remaster}, jioSaavnWant{Title: "Come Together (Remastered 2009)", Artist: "The Beatles"})
	if jioSaavnString(got, "id") != "remaster" {
		t.Fatalf("exact version lost to %q", jioSaavnString(got, "id"))
	}
}

func TestJioSaavnUnicodeCollaborationArtists(t *testing.T) {
	items := []map[string]any{
		{
			"id":          "cover",
			"title":       "Crazy in Love",
			"description": "Someone Else · Tributes",
			"more_info":   map[string]any{"primary_artists": "Someone Else", "singers": "Someone Else"},
		},
		{
			"id":    "duet",
			"title": "Crazy in Love",
			"artists": map[string]any{
				"primary":  []any{map[string]any{"id": "1", "name": "Beyoncé", "role": "primary_artists"}},
				"featured": []any{map[string]any{"id": "2", "name": "JAY-Z", "role": "featured_artists"}},
			},
		},
	}
	for _, artist := range []string{"Beyonce & Jay-Z", "Beyoncé feat. JAY-Z", "Beyoncé"} {
		got, score := jioSaavnPickBest(items, jioSaavnWant{Title: "Crazy in Love", Artist: artist})
		if score <= 0 || jioSaavnString(got, "id") != "duet" {
			t.Fatalf("artist %q picked %q score %d, want duet", artist, jioSaavnString(got, "id"), score)
		}
	}
	got, score := jioSaavnPickBest(items, jioSaavnWant{Title: "Crazy in Love", Artist: "Someone Else"})
	if jioSaavnString(got, "id") != "cover" || score <= 0 {
		t.Fatalf("collaboration track should not steal a different requested artist: %#v", got)
	}
}

func TestJioSaavnCommunityNestedArtists(t *testing.T) {
	items := []map[string]any{
		{
			"id":    "cover",
			"name":  "Come Together",
			"album": map[string]any{"name": "Live At the Iron Horse"},
			"artists": map[string]any{
				"primary": []any{map[string]any{"name": "Milovan"}, map[string]any{"name": "Anna"}},
				"all":     []any{map[string]any{"name": "Milovan"}, map[string]any{"name": "Anna"}},
			},
		},
		{
			"id":       "nested",
			"name":     "Come Together (Remastered 2009)",
			"duration": "258",
			"album":    map[string]any{"id": "9", "name": "Abbey Road (Remastered)"},
			"artists": map[string]any{
				"primary": []any{map[string]any{"id": "624737", "name": "The Beatles", "role": "primary_artists", "type": "artist"}},
				"all":     []any{map[string]any{"name": "The Beatles"}},
			},
			"artistMap": map[string]any{
				"primary_artists": []any{map[string]any{"name": "The Beatles"}},
				"artists":         []any{map[string]any{"name": "The Beatles"}},
			},
		},
	}
	got, score := jioSaavnPickBest(items, jioSaavnWant{Title: "Come Together", Artist: "The Beatles", Album: "Abbey Road", Duration: 259})
	if score <= 0 || jioSaavnString(got, "id") != "nested" {
		t.Fatalf("picked %q score %d, want nested Beatles recording", jioSaavnString(got, "id"), score)
	}
}

func TestJioSaavnDescriptionOnlyArtistStillMatches(t *testing.T) {
	items := []map[string]any{
		{
			"id":          "cover",
			"title":       "Come Together",
			"description": "Milovan, Anna · Live At the Iron Horse",
		},
		{
			"id":          "desc",
			"title":       "Come Together (2019 Mix)",
			"subtitle":    "",
			"description": "The Beatles · The Beatles 1967 - 1970 (2023 Edition)",
		},
	}
	got, score := jioSaavnPickBest(items, jioSaavnWant{Title: "Come Together", Artist: "The Beatles"})
	if score <= 0 || jioSaavnString(got, "id") != "desc" {
		t.Fatalf("description credit was ignored, picked %q score %d", jioSaavnString(got, "id"), score)
	}
}

func TestJioSaavnDurationAndISRCAreEvidence(t *testing.T) {
	remaster := map[string]any{
		"id":          "buPhYncP",
		"title":       "Come Together (Remastered 2009)",
		"album":       "Abbey Road (Remastered)",
		"duration":    "258",
		"isrc":        "GBAYE0601698",
		"description": "The Beatles · Abbey Road (Remastered)",
		"more_info":   map[string]any{"primary_artists": "The Beatles"},
	}
	mix := map[string]any{
		"id":          "VaTL2kp-",
		"title":       "Come Together (2019 Mix)",
		"album":       "The Beatles 1967 - 1970 (2023 Edition)",
		"duration":    249,
		"isrc":        "OTHERISRC0001",
		"description": "The Beatles · The Beatles 1967 - 1970 (2023 Edition)",
		"more_info":   map[string]any{"primary_artists": "The Beatles"},
	}
	cover := jioSaavnComeTogetherFixture()[0]
	cover["duration"] = "187"

	got, score := jioSaavnPickBest([]map[string]any{mix, cover, remaster}, jioSaavnWant{
		Title: "Come Together", Artist: "The Beatles", Duration: 259, ISRC: "gbaye0601698", Album: "Abbey Road",
	})
	if score <= 0 || jioSaavnString(got, "id") != "buPhYncP" {
		t.Fatalf("duration/ISRC/album evidence picked %q score %d", jioSaavnString(got, "id"), score)
	}

	onlyMix, mixScore := jioSaavnPickBest([]map[string]any{mix}, jioSaavnWant{Title: "Come Together", Artist: "The Beatles", ISRC: "GBAYE0601698"})
	if mixScore <= 0 || jioSaavnString(onlyMix, "id") != "VaTL2kp-" {
		t.Fatal("a conflicting ISRC must not erase the only title and artist match")
	}
	loose, looseScore := jioSaavnPickBest([]map[string]any{mix, remaster}, jioSaavnWant{Title: "Come Together", Artist: "The Beatles"})
	if looseScore <= 0 || jioSaavnString(loose, "id") == "" {
		t.Fatal("title and artist alone should still match a version when duration and ISRC are absent")
	}
}

func TestJioSaavnLiveComeTogetherSelection(t *testing.T) {
	if os.Getenv("AURALIS_JIOSAAVN_LIVE") != "1" {
		t.Skip("set AURALIS_JIOSAAVN_LIVE=1 to download the Come Together fixture")
	}
	root := os.Getenv("AURALIS_LIVE_RESULTS_DIR")
	if root == "" || !filepath.IsAbs(root) {
		t.Fatal("AURALIS_LIVE_RESULTS_DIR must be an absolute isolated directory")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(appDataDirEnv, filepath.Join(root, "app-data"))
	t.Setenv("USERPROFILE", filepath.Join(root, "profile"))
	t.Setenv("HOME", filepath.Join(root, "profile"))
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatal("ffprobe is required")
	}
	decoder, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal("ffmpeg is required")
	}
	redact := func(err error) string {
		if err == nil {
			return ""
		}
		return regexp.MustCompile(`https?://\S+`).ReplaceAllString(err.Error(), "<redacted-url>")
	}
	_, release := BeginDownloadCancellationScope()
	defer release()
	want := ExtraDownloadParams{TrackName: "Come Together", ArtistName: "The Beatles"}
	id, _, searchErr := searchJioSaavn(want)
	if searchErr != nil {
		t.Fatal(redact(searchErr))
	}
	params := url.Values{"__call": {"song.getDetails"}, "cc": {"in"}, "_format": {"json"}, "_marker": {"0"}, "pids": {id}}
	req, err := NewRequestWithDefaultHeaders(http.MethodGet, jioSaavnOfficialAPI+"?"+params.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(WithDownloadContext(req))
	if err != nil {
		t.Fatal(redact(err))
	}
	var details map[string]any
	decodeErr := json.NewDecoder(resp.Body).Decode(&details)
	resp.Body.Close()
	if decodeErr != nil {
		t.Fatal("song details were not valid JSON")
	}
	song := jioSaavnExtractSong(details, id)
	title := jioSaavnString(song, "song")
	artist := jioSaavnString(song, "primary_artists")
	var catalogDuration int
	fmt.Sscanf(jioSaavnString(song, "duration"), "%d", &catalogDuration)
	if !strings.Contains(strings.ToLower(title), "come together") || !strings.Contains(strings.ToLower(artist), "the beatles") {
		t.Fatalf("selected id %s title %q artist %q", id, title, artist)
	}
	if catalogDuration < 200 {
		t.Fatalf("selected id %s catalog duration %ds, want the full Beatles recording", id, catalogDuration)
	}
	path, _, downloadErr := downloadJioSaavnTrack(want, filepath.Join(root, "come-together.audio"))
	if downloadErr != nil {
		t.Fatal(redact(downloadErr))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	body, probeErr := exec.CommandContext(ctx, probe, "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,sample_rate:format=duration", "-of", "json", path).Output()
	var media struct {
		Streams []struct {
			Codec string `json:"codec_name"`
			Rate  string `json:"sample_rate"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if probeErr != nil || json.Unmarshal(body, &media) != nil || len(media.Streams) == 0 {
		t.Fatal("download is not readable audio")
	}
	var decoded float64
	fmt.Sscanf(media.Format.Duration, "%f", &decoded)
	if output, decodeErr := exec.CommandContext(ctx, decoder, "-v", "error", "-xerror", "-i", path, "-map", "0:a:0", "-f", "null", "-").CombinedOutput(); decodeErr != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 180 {
			message = message[:180]
		}
		t.Fatalf("full decode failed: %s", message)
	}
	if math.Abs(decoded-float64(catalogDuration)) > 15 {
		t.Fatalf("decoded %.3fs codec %s, catalog %ds", decoded, media.Streams[0].Codec, catalogDuration)
	}
	t.Logf("id=%s title=%q artist=%q catalog_seconds=%d decoded_seconds=%.3f codec=%s sample_rate=%s full_decode=ok", id, title, artist, catalogDuration, decoded, media.Streams[0].Codec, media.Streams[0].Rate)
}

func jioSaavnComeTogetherFixture() []map[string]any {
	return []map[string]any{
		{
			"id":          "WYD9hPrJ",
			"title":       "Come Together",
			"subtitle":    "",
			"description": "Milovan, Anna · Live At the Iron Horse",
			"more_info": map[string]any{
				"primary_artists": "Milovan, Anna",
				"singers":         "Milovan, Anna",
			},
		},
		{
			"id":          "bs5iaXrI",
			"title":       "Come Together",
			"description": "The Shiny Darks · 50 Years Of Aerosmith - A 2020s Tribute",
			"more_info": map[string]any{
				"primary_artists": "The Shiny Darks",
				"singers":         "The Shiny Darks",
			},
		},
		{
			"id":          "buPhYncP",
			"title":       "Come Together (Remastered 2009)",
			"subtitle":    "",
			"album":       "Abbey Road (Remastered)",
			"description": "The Beatles · Abbey Road (Remastered)",
			"more_info": map[string]any{
				"primary_artists": "The Beatles",
				"singers":         "",
			},
		},
		{
			"id":          "VaTL2kp-",
			"title":       "Come Together (2019 Mix)",
			"description": "The Beatles · The Beatles 1967 - 1970 (2023 Edition)",
			"more_info": map[string]any{
				"primary_artists": "The Beatles",
				"singers":         "",
			},
		},
	}
}

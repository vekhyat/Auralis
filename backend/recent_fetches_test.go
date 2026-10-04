package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveRecentFetchesIsAtomic(t *testing.T) {
	dir := isolateAppData(t)
	items := []RecentFetchItem{{
		ID:        "1",
		URL:       "https://open.spotify.com/track/abc",
		Type:      "track",
		Name:      "Song",
		Artist:    "Artist",
		Timestamp: 10,
	}}
	if err := SaveRecentFetches(items); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRecentFetches()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Name != "Song" || loaded[0].URL != items[0].URL {
		t.Fatalf("loaded = %#v", loaded)
	}

	if err := SaveRecentFetches(nil); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadRecentFetches()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded = %#v", loaded)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".auralis-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
	if _, err := os.Stat(filepath.Join(dir, recentFetchesFileName)); err != nil {
		t.Fatal(err)
	}
}

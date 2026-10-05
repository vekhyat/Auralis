package taste

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLikedNotDownloadedShelf(t *testing.T) {
	now := time.Now()
	events := []TasteEvent{
		{
			Kind:      KindSaveTrack,
			SpotifyID: "track1",
			Artist:    "Daft Punk",
			Title:     "One More Time",
			Timestamp: now,
		},
		{
			Kind:      KindSaveTrack,
			SpotifyID: "track2",
			Artist:    "Daft Punk",
			Title:     "Digital Love",
			Timestamp: now,
		},
		{
			Kind:      KindSaveTrack,
			SpotifyID: "track3",
			Artist:    "Justice",
			Title:     "Genesis",
			Timestamp: now,
		},
	}

	fb := Feedback{
		DismissedItems: map[string]bool{
			"track:track2": true, // dismissed
		},
	}

	// Mock library filter: track1 is already owned
	libraryFilter := func(lookup LibraryLookup) bool {
		return lookup.SpotifyID == "track1"
	}

	profile := BuildProfile(events, fb, now)
	rawItems := likedNotDownloaded(events, profile)
	shelfItems := finalise(rawItems, fb, ownership{tracks: libraryFilter})

	// track1 is owned -> excluded
	// track2 is dismissed -> excluded
	// track3 (Justice - Genesis) should remain
	if len(shelfItems) != 1 {
		t.Fatalf("expected 1 item on liked-not-downloaded shelf, got %d", len(shelfItems))
	}
	if shelfItems[0].SpotifyID != "track3" || shelfItems[0].Artist != "Justice" {
		t.Fatalf("unexpected shelf item: %+v", shelfItems[0])
	}
}

func TestFinishAlbumsShelf(t *testing.T) {
	now := time.Now()
	// Album 1 has 3 liked tracks, 1 owned in library -> eligible!
	// Album 2 has 3 liked tracks, ALL 3 owned in library -> excluded!
	// Album 3 has 2 liked tracks -> excluded (< 3 tracks)
	events := []TasteEvent{
		{Kind: KindSaveTrack, SpotifyID: "t1", Artist: "ArtistA", Album: "Album1", Title: "Track1", Timestamp: now},
		{Kind: KindSaveTrack, SpotifyID: "t2", Artist: "ArtistA", Album: "Album1", Title: "Track2", Timestamp: now},
		{Kind: KindSaveTrack, SpotifyID: "t3", Artist: "ArtistA", Album: "Album1", Title: "Track3", Timestamp: now},

		{Kind: KindSaveTrack, SpotifyID: "t4", Artist: "ArtistB", Album: "Album2", Title: "Track4", Timestamp: now},
		{Kind: KindSaveTrack, SpotifyID: "t5", Artist: "ArtistB", Album: "Album2", Title: "Track5", Timestamp: now},
		{Kind: KindSaveTrack, SpotifyID: "t6", Artist: "ArtistB", Album: "Album2", Title: "Track6", Timestamp: now},

		{Kind: KindSaveTrack, SpotifyID: "t7", Artist: "ArtistC", Album: "Album3", Title: "Track7", Timestamp: now},
		{Kind: KindSaveTrack, SpotifyID: "t8", Artist: "ArtistC", Album: "Album3", Title: "Track8", Timestamp: now},
	}

	fb := Feedback{}
	libraryFilter := func(lookup LibraryLookup) bool {
		// All tracks of Album2 are in library
		return lookup.SpotifyID == "t1" || lookup.SpotifyID == "t4" || lookup.SpotifyID == "t5" || lookup.SpotifyID == "t6"
	}

	profile := BuildProfile(events, fb, now)
	albumItems := finishAlbums(events, profile, libraryFilter)
	items := finalise(albumItems, fb, ownership{})

	if len(items) != 1 {
		t.Fatalf("expected 1 album item to finish, got %d: %+v", len(items), items)
	}
	if items[0].Title != "Album1" || items[0].Artist != "ArtistA" {
		t.Fatalf("expected Album1 by ArtistA, got: %+v", items[0])
	}
	if !strings.Contains(items[0].Reason, "3 tracks") {
		t.Fatalf("unexpected reason: %s", items[0].Reason)
	}
}

func TestSimilarArtistCandidates(t *testing.T) {
	events := []TasteEvent{
		{Kind: KindSaveTrack, Artist: "Aphex Twin", Title: "Xtal", Timestamp: time.Now()},
		{Kind: KindSaveTrack, Artist: "AlreadyKnownArtist", Title: "Song", Timestamp: time.Now()},
	}

	fb := Feedback{
		BannedArtists: []string{"BannedArtist"},
		DismissedItems: map[string]bool{
			"artist:" + Normalise("DismissedArtist"): true,
		},
	}

	candidates := []SimilarArtist{
		{Name: "Boards of Canada", Match: 0.9},
		{Name: "AlreadyKnownArtist", Match: 0.85}, // should be excluded (already known)
		{Name: "BannedArtist", Match: 0.8},        // should be excluded (banned)
		{Name: "DismissedArtist", Match: 0.75},   // should be excluded (dismissed)
		{Name: "Squarepusher", Match: 0.7},
	}

	items := similarArtistCandidates(candidates, "Aphex Twin", events, fb)
	if len(items) != 2 {
		t.Fatalf("expected 2 similar artist items, got %d: %+v", len(items), items)
	}
	if items[0].Artist != "Boards of Canada" || items[1].Artist != "Squarepusher" {
		t.Fatalf("unexpected similar items: %+v", items)
	}
}

func TestCappingAndDiversity(t *testing.T) {
	// Artist 1 has 5 items
	// Artist 2 has 2 items
	items := []Item{
		{ID: "1", Artist: "Artist1", Title: "Track1", Score: 100},
		{ID: "2", Artist: "Artist1", Title: "Track2", Score: 95},
		{ID: "3", Artist: "Artist1", Title: "Track3", Score: 90},
		{ID: "4", Artist: "Artist1", Title: "Track4", Score: 85},
		{ID: "5", Artist: "Artist1", Title: "Track5", Score: 80},
		{ID: "6", Artist: "Artist2", Title: "Track6", Score: 70},
		{ID: "7", Artist: "Artist2", Title: "Track7", Score: 65},
	}

	capped := capPerArtist(items)
	// maxItemsPerArtistPerShelf = 3, so Artist1 should have 3, Artist2 has 2 -> total 5
	if len(capped) != 5 {
		t.Fatalf("expected 5 items after capping, got %d", len(capped))
	}

	div := diversified(capped)
	// Diversity interleaves artists: should alternate between Artist1 and Artist2
	if len(div) != 5 {
		t.Fatalf("expected 5 items after diversity, got %d", len(div))
	}
	if div[0].Artist != "Artist1" || div[1].Artist != "Artist2" || div[2].Artist != "Artist1" || div[3].Artist != "Artist2" || div[4].Artist != "Artist1" {
		t.Fatalf("unexpected diversity ordering: %+v", div)
	}
}

func TestOwnedAlbumsAreFilteredAndGapsKeepFullReleases(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"Radiohead/OK Computer", "Weezer"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	index := &FolderAlbumIndex{Root: root}
	if !index.Owned("Radiohead", "OK Computer (Remastered)") {
		t.Fatal("expected the album folder to count as owned")
	}
	// "Weezer" at the top level is the artist folder (no audio inside), so
	// the self-titled album is not owned.
	if index.Owned("Weezer", "Weezer") {
		t.Fatal("artist folder mistaken for a self-titled album")
	}

	items := []Item{
		{ID: "album:1", Kind: "album", Title: "OK Computer", Artist: "Radiohead"},
		{ID: "album:2", Kind: "album", Title: "Kid A", Artist: "Radiohead"},
	}
	kept := finalise(items, Feedback{}, ownership{albums: index.Owned})
	if len(kept) != 1 || kept[0].Title != "Kid A" {
		t.Fatalf("kept = %+v", kept)
	}

	gaps := discographyGapItems("Radiohead", []GapAlbum{
		{ID: "a", Name: "In Rainbows", AlbumType: "album"},
		{ID: "b", Name: "Creep", AlbumType: "single", TotalTracks: 2},
		{ID: "c", Name: "My Iron Lung", AlbumType: "single", TotalTracks: 8},
		{ID: "d", Name: "Hits", AlbumType: "compilation"},
	}, Profile{})
	var names []string
	for _, g := range gaps {
		names = append(names, g.Title)
	}
	if len(names) != 2 || names[0] != "In Rainbows" || names[1] != "My Iron Lung" {
		t.Fatalf("gap releases = %v", names)
	}
}

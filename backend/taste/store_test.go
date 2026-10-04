package taste

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreIdempotenceAndFeedback(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "auralis-taste-store-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "taste.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	now := time.Now()
	events := []TasteEvent{
		{
			ID:        "ev-1",
			Kind:      KindPlay,
			Artist:    "Daft Punk",
			Title:     "One More Time",
			MsPlayed:  200000,
			Timestamp: now,
			Source:    "test",
		},
		{
			ID:        "ev-2",
			Kind:      KindSaveTrack,
			Artist:    "Daft Punk",
			Title:     "Aerodynamic",
			Timestamp: now,
			Source:    "test",
		},
	}

	// 1. First record should insert 2 events
	added, err := store.AddEvents(events)
	if err != nil {
		t.Fatalf("AddEvents failed: %v", err)
	}
	if added != 2 {
		t.Fatalf("expected 2 events added, got %d", added)
	}

	// 2. Second record of exact same events must be idempotent (0 newly added)
	addedAgain, err := store.AddEvents(events)
	if err != nil {
		t.Fatalf("second AddEvents failed: %v", err)
	}
	if addedAgain != 0 {
		t.Fatalf("expected 0 events added on duplicate import, got %d", addedAgain)
	}

	allEvents, err := store.AllEvents()
	if err != nil {
		t.Fatalf("AllEvents failed: %v", err)
	}
	if len(allEvents) != 2 {
		t.Fatalf("expected 2 total events, got %d", len(allEvents))
	}

	// 3. Feedback operations
	err = store.UpdateFeedback(func(fb *Feedback) {
		fb.PinnedArtists = append(fb.PinnedArtists, "Daft Punk")
	})
	if err != nil {
		t.Fatalf("UpdateFeedback failed: %v", err)
	}
	fb, err := store.GetFeedback()
	if err != nil {
		t.Fatalf("GetFeedback failed: %v", err)
	}
	if len(fb.PinnedArtists) != 1 || fb.PinnedArtists[0] != "Daft Punk" {
		t.Fatalf("expected Daft Punk pinned, got: %+v", fb.PinnedArtists)
	}

	// Unpin
	err = store.UpdateFeedback(func(fb *Feedback) {
		fb.PinnedArtists = nil
	})
	if err != nil {
		t.Fatalf("UpdateFeedback unpin failed: %v", err)
	}
	fb, _ = store.GetFeedback()
	if len(fb.PinnedArtists) != 0 {
		t.Fatalf("expected 0 pinned artists after unpin, got: %+v", fb.PinnedArtists)
	}

	// Ban and Unban
	err = store.UpdateFeedback(func(fb *Feedback) {
		fb.BannedArtists = append(fb.BannedArtists, "Kraftwerk")
	})
	if err != nil {
		t.Fatalf("UpdateFeedback ban failed: %v", err)
	}
	fb, _ = store.GetFeedback()
	if len(fb.BannedArtists) != 1 || fb.BannedArtists[0] != "Kraftwerk" {
		t.Fatalf("expected Kraftwerk banned, got: %+v", fb.BannedArtists)
	}
	err = store.UpdateFeedback(func(fb *Feedback) {
		fb.BannedArtists = nil
	})
	if err != nil {
		t.Fatalf("UpdateFeedback unban failed: %v", err)
	}
	fb, _ = store.GetFeedback()
	if len(fb.BannedArtists) != 0 {
		t.Fatalf("expected 0 banned artists after unban, got: %+v", fb.BannedArtists)
	}

	// Dismiss item
	err = store.UpdateFeedback(func(fb *Feedback) {
		if fb.DismissedItems == nil {
			fb.DismissedItems = map[string]bool{}
		}
		fb.DismissedItems["track:123"] = true
	})
	if err != nil {
		t.Fatalf("UpdateFeedback dismiss failed: %v", err)
	}
	fb, _ = store.GetFeedback()
	if !fb.DismissedItems["track:123"] {
		t.Fatalf("expected track:123 to be dismissed")
	}

	// 4. Gap Albums Cache
	gaps := []GapAlbum{
		{ID: "album-1", Name: "Homework", AlbumType: "album", ReleaseDate: "1997", TotalTracks: 16},
	}
	if err := store.SetGaps("artist-dp", gaps, time.Now()); err != nil {
		t.Fatalf("SetGaps failed: %v", err)
	}
	cachedGaps, fetchedAt, ok := store.GetGaps("artist-dp")
	if !ok || time.Since(fetchedAt) > time.Minute {
		t.Fatalf("GetGaps failed or not found: ok=%v", ok)
	}
	if len(cachedGaps) != 1 || cachedGaps[0].Name != "Homework" {
		t.Fatalf("unexpected cached gaps: %+v", cachedGaps)
	}
}

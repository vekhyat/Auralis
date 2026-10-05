package taste

import (
	"math"
	"testing"
	"time"
)

func TestProfileAffinityAndDecay(t *testing.T) {
	now := time.Now()

	// 1. Check weight of play under 30s (skip) vs normal play
	shortPlay := TasteEvent{Kind: KindPlay, Artist: "ArtistSkip", Title: "SkipSong", MsPlayed: 15000, Timestamp: now}
	if shortPlay.Weight() >= 0 {
		t.Fatalf("expected skip under 30s to have negative weight, got %f", shortPlay.Weight())
	}
	if shortPlay.Weight() != -0.5 {
		t.Fatalf("expected -0.5 weight for skip, got %f", shortPlay.Weight())
	}

	fullPlay := TasteEvent{Kind: KindPlay, Artist: "ArtistGood", Title: "GoodSong", MsPlayed: 180000, Timestamp: now}
	if fullPlay.Weight() != 1.0 {
		t.Fatalf("expected 1.0 weight for full play, got %f", fullPlay.Weight())
	}

	// 2. Check time decay: an event from 60 days ago
	sixtyDaysAgo := now.Add(-60 * 24 * time.Hour)
	oldEvent := TasteEvent{Kind: KindPlay, Artist: "OldFavorite", Title: "OldSong", MsPlayed: 200000, Timestamp: sixtyDaysAgo}
	recentEvent := TasteEvent{Kind: KindPlay, Artist: "NewFavorite", Title: "NewSong", MsPlayed: 200000, Timestamp: now}

	events := []TasteEvent{oldEvent, recentEvent, shortPlay}
	fb := Feedback{}
	p := BuildProfile(events, fb, now)

	oldKey := Normalise("OldFavorite")
	newKey := Normalise("NewFavorite")
	skipKey := Normalise("ArtistSkip")

	// In current profile (tau = 28 days), 60 days ago has decayed: e^(-60/28) ≈ 0.117
	// In core profile (tau = 365 days), 60 days ago has decayed: e^(-60/365) ≈ 0.848
	expectedCurrentDecay := math.Exp(-60.0 / 28.0)
	expectedCoreDecay := math.Exp(-60.0 / 365.0)

	if math.Abs(p.CurrentArtists[oldKey]-expectedCurrentDecay) > 0.05 {
		t.Fatalf("expected current affinity ~%f, got %f", expectedCurrentDecay, p.CurrentArtists[oldKey])
	}
	if math.Abs(p.CoreArtists[oldKey]-expectedCoreDecay) > 0.05 {
		t.Fatalf("expected core affinity ~%f, got %f", expectedCoreDecay, p.CoreArtists[oldKey])
	}

	// Recent event should have decay ≈ 1.0
	if math.Abs(p.CurrentArtists[newKey]-1.0) > 0.01 {
		t.Fatalf("expected recent event current affinity ~1.0, got %f", p.CurrentArtists[newKey])
	}

	// Skip key should have negative score
	if p.CoreArtists[skipKey] >= 0 {
		t.Fatalf("expected skip artist to have negative affinity, got %f", p.CoreArtists[skipKey])
	}
}

func TestProfilePinAndBan(t *testing.T) {
	now := time.Now()
	events := []TasteEvent{
		{Kind: KindPlay, Artist: "LovedArtist", Title: "Track1", MsPlayed: 200000, Timestamp: now},
		{Kind: KindPlay, Artist: "BannedArtist", Title: "Track2", MsPlayed: 200000, Timestamp: now},
		{Kind: KindPlay, Artist: "PinnedArtist", Title: "Track3", MsPlayed: 200000, Timestamp: now},
	}

	fb := Feedback{
		PinnedArtists: []string{"PinnedArtist"},
		BannedArtists: []string{"BannedArtist"},
	}

	p := BuildProfile(events, fb, now)

	bannedKey := Normalise("BannedArtist")
	if _, exists := p.CoreArtists[bannedKey]; exists {
		t.Fatalf("banned artist must be deleted from CoreArtists")
	}
	if _, exists := p.CurrentArtists[bannedKey]; exists {
		t.Fatalf("banned artist must be deleted from CurrentArtists")
	}
	if _, exists := p.ArtistNames[bannedKey]; exists {
		t.Fatalf("banned artist must be deleted from ArtistNames")
	}

	pinnedKey := Normalise("PinnedArtist")
	lovedKey := Normalise("LovedArtist")
	// Pinned artist should receive a boost above loved artist
	if p.CoreArtists[pinnedKey] <= p.CoreArtists[lovedKey] {
		t.Fatalf("pinned artist should have higher affinity than loved artist due to boost (pinned=%f, loved=%f)",
			p.CoreArtists[pinnedKey], p.CoreArtists[lovedKey])
	}
}

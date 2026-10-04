package taste

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseExtended(t *testing.T) {
	validRow := map[string]interface{}{
		"ts":                              "2023-08-15T10:30:00Z",
		"ms_played":                       float64(185000),
		"master_metadata_album_artist_name": "Daft Punk",
		"master_metadata_album_album_name":  "Discovery",
		"master_metadata_track_name":        "One More Time",
		"spotify_track_uri":               "spotify:track:0DiWol3AO6WpXZgp0gwoAV",
	}

	ev, err := parseExtended(validRow)
	if err != nil {
		t.Fatalf("parseExtended failed on valid row: %v", err)
	}
	if ev.Artist != "Daft Punk" || ev.Album != "Discovery" || ev.Title != "One More Time" {
		t.Fatalf("unexpected metadata parsed: %+v", ev)
	}
	if ev.SpotifyID != "0DiWol3AO6WpXZgp0gwoAV" {
		t.Fatalf("unexpected Spotify ID: %s", ev.SpotifyID)
	}
	if ev.MsPlayed != 185000 {
		t.Fatalf("unexpected ms played: %d", ev.MsPlayed)
	}
	expectedTime, _ := time.Parse(time.RFC3339, "2023-08-15T10:30:00Z")
	if !ev.Timestamp.Equal(expectedTime) {
		t.Fatalf("unexpected timestamp: %v", ev.Timestamp)
	}

	// Missing artist or title (e.g. podcast episode)
	podcastRow := map[string]interface{}{
		"ts":                              "2023-08-15T10:30:00Z",
		"ms_played":                       float64(300000),
		"master_metadata_album_artist_name": "",
		"master_metadata_album_album_name":  "",
		"master_metadata_track_name":        "",
		"episode_name":                    "Daily News",
	}
	if _, err := parseExtended(podcastRow); err == nil {
		t.Fatalf("expected error for row missing artist/title")
	}

	// Invalid timestamp
	badTimeRow := map[string]interface{}{
		"ts":                              "not-a-timestamp",
		"master_metadata_album_artist_name": "Artist",
		"master_metadata_track_name":        "Title",
	}
	if _, err := parseExtended(badTimeRow); err == nil {
		t.Fatalf("expected error for bad timestamp")
	}
}

func TestParseLegacy(t *testing.T) {
	validRow := map[string]interface{}{
		"endTime":    "2023-08-15 10:30",
		"artistName": "Radiohead",
		"trackName":  "Karma Police",
		"msPlayed":   float64(260000),
	}

	ev, err := parseLegacy(validRow)
	if err != nil {
		t.Fatalf("parseLegacy failed on valid row: %v", err)
	}
	if ev.Artist != "Radiohead" || ev.Title != "Karma Police" {
		t.Fatalf("unexpected metadata parsed: %+v", ev)
	}
	if ev.MsPlayed != 260000 {
		t.Fatalf("unexpected ms played: %d", ev.MsPlayed)
	}
	expectedTime, _ := time.Parse("2006-01-02 15:04", "2023-08-15 10:30")
	if !ev.Timestamp.Equal(expectedTime) {
		t.Fatalf("unexpected timestamp: %v", ev.Timestamp)
	}

	// Missing track name
	badRow := map[string]interface{}{
		"endTime":    "2023-08-15 10:30",
		"artistName": "Radiohead",
		"trackName":  "",
		"msPlayed":   float64(10000),
	}
	if _, err := parseLegacy(badRow); err == nil {
		t.Fatalf("expected error for missing track name")
	}
}

func TestImportDirAndZip(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "auralis-taste-export-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	extendedJSON := `[
		{
			"ts": "2023-01-01T12:00:00Z",
			"ms_played": 200000,
			"master_metadata_album_artist_name": "Daft Punk",
			"master_metadata_album_album_name": "Discovery",
			"master_metadata_track_name": "Aerodynamic",
			"spotify_track_uri": "spotify:track:track1"
		},
		{
			"ts": "2023-01-01T12:05:00Z",
			"ms_played": 15000,
			"master_metadata_album_artist_name": "Daft Punk",
			"master_metadata_album_album_name": "Discovery",
			"master_metadata_track_name": "Digital Love",
			"spotify_track_uri": "spotify:track:track2"
		}
	]`

	legacyJSON := `[
		{
			"endTime": "2023-02-01 14:00",
			"artistName": "Justice",
			"trackName": "Genesis",
			"msPlayed": 230000
		}
	]`

	dirPath := filepath.Join(tempDir, "export_folder")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("failed to create export dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dirPath, "Streaming_History_Audio_2023.json"), []byte(extendedJSON), 0o644); err != nil {
		t.Fatalf("failed to write extended json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "StreamingHistory0.json"), []byte(legacyJSON), 0o644); err != nil {
		t.Fatalf("failed to write legacy json: %v", err)
	}

	importer := &SpotifyExport{}
	ctx := context.Background()

	// 1. Test importing folder
	events, err := importer.Import(ctx, dirPath)
	if err != nil {
		t.Fatalf("Import(dir) failed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// 2. Test importing .zip archive
	zipPath := filepath.Join(tempDir, "export.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("failed to create zip file: %v", err)
	}
	zw := zip.NewWriter(zipFile)

	f1, err := zw.Create("MyData/Streaming_History_Audio_2023.json")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	f1.Write([]byte(extendedJSON))

	f2, err := zw.Create("MyData/StreamingHistory0.json")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	f2.Write([]byte(legacyJSON))

	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}
	zipFile.Close()

	zipEvents, err := importer.Import(ctx, zipPath)
	if err != nil {
		t.Fatalf("Import(zip) failed: %v", err)
	}
	if len(zipEvents) != 3 {
		t.Fatalf("expected 3 events from zip, got %d", len(zipEvents))
	}
}

package taste

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ImportProgress is reported while an export is parsed.
type ImportProgress struct {
	Phase   string // "scan" | "parse" | "done"
	Message string
	Count   int
}

// SpotifyExport imports the user's "Download your data" export. It accepts a
// folder, a single JSON file, or a .zip of the export. Two formats are
// supported: the extended history (Streaming_History_Audio_*.json with `ts`
// and `ms_played`) and the older StreamingHistory*.json (`endTime`,
// `msPlayed`).
type SpotifyExport struct {
	// Progress, when set, receives parsing updates.
	Progress func(ImportProgress)
}

func (s *SpotifyExport) report(p ImportProgress) {
	if s.Progress != nil {
		s.Progress(p)
	}
}

// ImportPath imports everything found at path (file, folder or .zip) and
// returns the deduped set of events (one entry per raw play).
func (s *SpotifyExport) ImportPath(path string) ([]TasteEvent, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return s.importDir(path)
	}
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		tmp, err := os.MkdirTemp("", "auralis-export-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		if err := unzipDir(path, tmp); err != nil {
			return nil, err
		}
		return s.importDir(tmp)
	}
	return s.importFile(path)
}

func (s *SpotifyExport) importDir(dir string) ([]TasteEvent, error) {
	var events []TasteEvent
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if strings.HasSuffix(name, ".json") && (strings.HasPrefix(name, "streaming_history_audio") || strings.HasPrefix(name, "streaminghistory")) {
			ev, err := s.importFile(p)
			if err != nil {
				return err
			}
			events = append(events, ev...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, errors.New("no Spotify streaming history files found")
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Timestamp.Before(events[j].Timestamp) })
	return events, nil
}

func (s *SpotifyExport) importFile(path string) ([]TasteEvent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var probe []map[string]interface{}
	s.report(ImportProgress{Phase: "parse", Message: filepath.Base(path), Count: 0})
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var events []TasteEvent
	for _, row := range probe {
		if _, ok := row["ts"]; ok {
			ev, err := parseExtended(row)
			if err != nil {
				continue
			}
			events = append(events, ev)
			continue
		}
		ev, err := parseLegacy(row)
		if err != nil {
			continue
		}
		events = append(events, ev)
	}
	s.report(ImportProgress{Phase: "parse", Message: filepath.Base(path), Count: len(events)})
	return events, nil
}

func spotifyTrackIDFromURI(uri string) string {
	if strings.HasPrefix(uri, "spotify:track:") {
		return strings.TrimPrefix(uri, "spotify:track:")
	}
	return ""
}

func eventID(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func parseExtended(row map[string]interface{}) (TasteEvent, error) {
	ts, _ := row["ts"].(string)
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return TasteEvent{}, err
	}
	ms, _ := row["ms_played"].(float64)
	artist, _ := row["master_metadata_album_artist_name"].(string)
	album, _ := row["master_metadata_album_album_name"].(string)
	title, _ := row["master_metadata_track_name"].(string)
	uri, _ := row["spotify_track_uri"].(string)
	ev := TasteEvent{
		Kind:      KindPlay,
		SpotifyID: spotifyTrackIDFromURI(uri),
		Artist:    artist,
		Album:     album,
		Title:     title,
		Timestamp: t,
		MsPlayed:  int(ms),
		Source:    "spotify_export",
	}
	ev.ID = eventID("spotify_export", ts, uri, title, artist, strconv.Itoa(int(ms)))
	return ev, nil
}

func parseLegacy(row map[string]interface{}) (TasteEvent, error) {
	endTime, _ := row["endTime"].(string)
	if endTime == "" {
		return TasteEvent{}, errors.New("missing endTime")
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", endTime, time.Local)
	if err != nil {
		return TasteEvent{}, err
	}
	ms, _ := row["msPlayed"].(float64)
	artist, _ := row["artistName"].(string)
	title, _ := row["trackName"].(string)
	ev := TasteEvent{
		Kind:      KindPlay,
		Artist:    artist,
		Title:     title,
		Timestamp: t,
		MsPlayed:  int(ms),
		Source:    "spotify_export",
	}
	ev.ID = eventID("spotify_export", endTime, artist, title, strconv.Itoa(int(ms)))
	return ev, nil
}

func unzipDir(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		target := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal path %s in archive", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(w, rc)
		w.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

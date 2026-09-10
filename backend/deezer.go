package backend

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func downloadDeezerTrack(p ExtraDownloadParams, destPath string) (string, string, error) {
	trackID := ""
	sourceURL := p.ServiceURL
	if sourceURL != "" {
		if id, err := extractDeezerTrackID(sourceURL); err == nil {
			trackID = id
		}
	}
	if trackID == "" && p.SpotifyID != "" {
		if id, err := antraResolveSpotify("deezer", p.SpotifyID); err == nil {
			trackID = id
		}
	}
	if trackID == "" {
		hit, err := resolveISRCOrSearch(p, "deezer")
		if err != nil {
			return "", "", fmt.Errorf("deezer: %w", err)
		}
		trackID = hit.TrackID
	}
	if trackID == "" {
		return "", "", fmt.Errorf("deezer: could not resolve track")
	}
	if sourceURL == "" {
		sourceURL = fmt.Sprintf("https://www.deezer.com/track/%s", trackID)
	}
	fmt.Printf("Downloading Deezer track %s via Antra mirror...\n", trackID)

	info, err := antraGetTrackInfo("deezer", trackID)
	if err != nil {
		return "", sourceURL, err
	}
	key := deezerBlowfishKey(info.DecryptionKey)
	if len(key) == 0 {
		path, streamErr := antraStreamToFile("deezer", trackID, destPath, nil)
		return path, sourceURL, streamErr
	}

	resp, err := antraMirrorGet("deezer", "/api/stream/"+url.PathEscape(trackID), nil, 4*time.Minute)
	if err != nil {
		return "", sourceURL, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", sourceURL, fmt.Errorf("deezer stream HTTP %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	pw := NewProgressWriter(&buf)
	if _, err := io.Copy(pw, io.LimitReader(resp.Body, 200<<20)); err != nil {
		return "", sourceURL, WrapDownloadCancelled(err)
	}
	encrypted := buf.Bytes()
	decrypted, err := decryptDeezerStream(encrypted, key)
	if err != nil {
		return "", sourceURL, err
	}
	ext := ".flac"
	if len(decrypted) >= 4 && string(decrypted[:4]) == "fLaC" {
		ext = ".flac"
	} else if len(decrypted) >= 3 && string(decrypted[:3]) == "ID3" {
		ext = ".mp3"
	} else if len(decrypted) >= 8 && string(decrypted[4:8]) == "ftyp" {
		ext = ".m4a"
	}
	finalPath := replaceAudioExtension(destPath, ext)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil && filepath.Dir(finalPath) != "." {
		return "", sourceURL, err
	}
	if err := os.WriteFile(finalPath, decrypted, 0644); err != nil {
		return "", sourceURL, err
	}
	fmt.Printf("Downloaded Deezer: %s (%.2f MB)\n", filepath.Base(finalPath), float64(len(decrypted))/(1024*1024))
	return finalPath, sourceURL, nil
}

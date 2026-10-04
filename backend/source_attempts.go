package backend

import (
	"errors"
	"fmt"
	"os"
	"time"
)

type sourceDownloadAttempt struct {
	id       string
	download func() (string, error)
}

func runDownloadSources(service, quality string, expectedSeconds int, attempts []sourceDownloadAttempt) (string, error) {
	ids := make([]string, 0, len(attempts))
	byID := map[string]sourceDownloadAttempt{}
	for _, attempt := range attempts {
		ids = append(ids, attempt.id)
		byID[attempt.id] = attempt
	}
	var failures []error
	for _, id := range RankedSourceIDs(ids, quality) {
		if err := CheckDownloadCancelled(); err != nil {
			return "", err
		}
		started := time.Now()
		path, err := byID[id].download()
		validated := false
		if err == nil && expectedSeconds > 0 {
			validated, err = ValidateDownloadedTrackDuration(path, expectedSeconds)
		} else if err == nil {
			duration, probeErr := readAudioDuration(path)
			if probeErr != nil || duration <= 0 {
				err = fmt.Errorf("%s returned unreadable audio", id)
			} else {
				validated = duration > previewMaxSeconds
			}
		}
		if err == nil && validated && sourceQuality(quality) == "24" {
			metadata, probeErr := GetMetadataWithFFprobe(path)
			if probeErr != nil || metadata.BitsPerSample <= 16 {
				err = fmt.Errorf("%s did not deliver readable 24-bit audio", id)
			}
		}
		if err == nil {
			err = CheckDownloadCancelled()
		}
		recordSourceOutcome(id, service, quality, time.Since(started), validated, err)
		if err == nil {
			return path, nil
		}
		if IsDownloadCancelledError(err) {
			return "", err
		}
		if path != "" {
			_ = os.Remove(path)
		}
		failures = append(failures, fmt.Errorf("%s: %w", id, err))
	}
	if len(failures) == 0 {
		return "", fmt.Errorf("no %s download sources", service)
	}
	return "", errors.Join(failures...)
}

func (t *TidalDownloader) downloadRankedTidal(trackID int64, quality, dest string, hints ...SourceTrack) (string, error) {
	viaURL := func(resolve func(int64, string) (string, error)) func() (string, error) {
		return func() (string, error) {
			raw, err := resolve(trackID, quality)
			if err == nil {
				err = t.DownloadFile(raw, dest, quality)
			}
			if err != nil {
				cleanupTidalDownloadArtifacts(dest)
			}
			return dest, err
		}
	}
	track := SourceTrack{ID: fmt.Sprint(trackID)}
	if len(hints) > 0 {
		track = hints[0]
		track.ID = fmt.Sprint(trackID)
	}
	attempts := []sourceDownloadAttempt{
		{"antra-tidal", func() (string, error) {
			return antraStreamToFile("tidal", fmt.Sprint(trackID), dest, antraQualityQuery("tidal", quality))
		}},
	}
	attempts = append(attempts, communitySourceAttempts("tidal", quality, dest, track)...)
	attempts = append(attempts, sourceDownloadAttempt{"community-tidal", viaURL(t.getTidalCommunityDownloadURL)}, sourceDownloadAttempt{"zarz-tidal", viaURL(t.getTidalZarzDownloadURL)})
	return runDownloadSources("tidal", quality, track.Duration, attempts)
}

func (q *QobuzDownloader) downloadRankedQobuz(trackID int64, quality, dest string, expectedSeconds int, allowFallback bool, hints ...SourceTrack) (string, error) {
	if quality == "" || quality == "5" {
		quality = "6"
	}
	viaURL := func(resolve func(int64, string) (string, error), qual string) func() (string, error) {
		return func() (string, error) {
			raw, err := resolve(trackID, qual)
			if err == nil {
				err = q.DownloadFile(raw, dest)
			}
			return dest, err
		}
	}
	if q.customURL != "" {
		path, err := viaURL(q.getQobuzCustomDownloadURL, quality)()
		if err == nil {
			_, err = ValidateDownloadedTrackDuration(path, expectedSeconds)
		}
		if err == nil || IsDownloadCancelledError(err) || !allowFallback {
			return path, err
		}
		_ = os.Remove(path)
	}
	qualities := []string{quality}
	if allowFallback {
		if quality == "27" {
			qualities = append(qualities, "7", "6")
		} else if quality == "7" {
			qualities = append(qualities, "6")
		}
	}
	var lastErr error
	track := SourceTrack{ID: fmt.Sprint(trackID), Duration: expectedSeconds}
	if len(hints) > 0 {
		track = hints[0]
		track.ID = fmt.Sprint(trackID)
		track.Duration = expectedSeconds
	}
	for _, qual := range qualities {
		attempts := []sourceDownloadAttempt{
			{"antra-qobuz", func() (string, error) {
				return antraStreamToFile("qobuz", fmt.Sprint(trackID), dest, antraQualityQuery("qobuz", qual))
			}},
		}
		attempts = append(attempts, communitySourceAttempts("qobuz", qual, dest, track)...)
		attempts = append(attempts, sourceDownloadAttempt{"community-qobuz", viaURL(q.getQobuzCommunityDownloadURL, qual)}, sourceDownloadAttempt{"zarz-qobuz", viaURL(q.getQobuzZarzDownloadURL, qual)})
		path, err := runDownloadSources("qobuz", qual, expectedSeconds, attempts)
		if err == nil || IsDownloadCancelledError(err) {
			return path, err
		}
		lastErr = err
	}
	return "", lastErr
}

package backend

import (
	"fmt"
	"math"
	"os"
	"sync"
)

const (
	previewMaxSeconds         = 35
	previewExpectedMinSeconds = 60
	largeMismatchMinExpected  = 90
	minAllowedDurationDiff    = 15
	durationDiffRatio         = 0.25
)

var (
	audioDurationMu sync.Mutex
	audioDurationFn = GetAudioDuration
)

func readAudioDuration(filePath string) (float64, error) {
	audioDurationMu.Lock()
	fn := audioDurationFn
	audioDurationMu.Unlock()
	return fn(filePath)
}

// SetAudioDurationReaderForTest replaces duration probing for tests that
// should not shell out to ffprobe. Pass nil to restore the real reader.
func SetAudioDurationReaderForTest(fn func(string) (float64, error)) {
	audioDurationMu.Lock()
	defer audioDurationMu.Unlock()
	if fn == nil {
		audioDurationFn = GetAudioDuration
		return
	}
	audioDurationFn = fn
}

// AcceptExistingMedia reports whether an on-disk file is readable audio that
// matches expectedSeconds when that duration is known. It never removes the
// file. A false result means the caller should download rather than skip.
func AcceptExistingMedia(filePath string, expectedSeconds int) bool {
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return false
	}
	if expectedSeconds > 0 {
		ok, validationErr := ValidateDownloadedTrackDuration(filePath, expectedSeconds)
		return validationErr == nil && ok
	}
	duration, err := readAudioDuration(filePath)
	return err == nil && duration > 0
}

func ValidateDownloadedTrackDuration(filePath string, expectedSeconds int) (bool, error) {
	if filePath == "" || expectedSeconds <= 0 {
		return false, nil
	}

	actualDuration, err := readAudioDuration(filePath)
	if err != nil || actualDuration <= 0 {
		if err != nil {
			return true, fmt.Errorf("downloaded file is not readable audio: %w", err)
		}
		return true, fmt.Errorf("downloaded file has no audio duration")
	}

	actualSeconds := int(math.Round(actualDuration))
	if actualSeconds <= 0 {
		return true, fmt.Errorf("downloaded file has no audio duration")
	}

	if expectedSeconds >= previewExpectedMinSeconds && actualSeconds <= previewMaxSeconds {
		return true, fmt.Errorf("detected preview/sample download: file is %ds, expected about %ds. file was removed", actualSeconds, expectedSeconds)
	}

	if expectedSeconds >= largeMismatchMinExpected {
		allowedDiff := int(math.Max(minAllowedDurationDiff, math.Round(float64(expectedSeconds)*durationDiffRatio)))
		diff := int(math.Abs(float64(actualSeconds - expectedSeconds)))
		if diff > allowedDiff {
			return true, fmt.Errorf("downloaded file duration mismatch: file is %ds, expected about %ds. file was removed", actualSeconds, expectedSeconds)
		}
	}

	return true, nil
}

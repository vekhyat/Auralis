package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CommunitySourceCheck struct {
	ID            string  `json:"id"`
	State         string  `json:"state"`
	Message       string  `json:"message"`
	LatencyMS     float64 `json:"latency_ms"`
	AudioVerified bool    `json:"audio_verified"`
	CheckedAt     string  `json:"checked_at"`
	// Action is "verify" for a cookie or hosted browser session and
	// "configure" when the source needs an environment credential.
	Action string `json:"action,omitempty"`
}

var sourceChecksMu sync.Mutex
var redactSourceURL = regexp.MustCompile(`https?://[^\s"<>]+`)

func GetCommunitySourceChecks() []CommunitySourceCheck {
	sourceChecksMu.Lock()
	defer sourceChecksMu.Unlock()
	dir, err := GetAppDir()
	if err != nil {
		return nil
	}
	body, _ := os.ReadFile(filepath.Join(dir, "community-source-checks.json"))
	var rows []CommunitySourceCheck
	_ = json.Unmarshal(body, &rows)
	return rows
}

func saveCommunityCheck(row CommunitySourceCheck) {
	sourceChecksMu.Lock()
	defer sourceChecksMu.Unlock()
	dir, err := GetAppDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, "community-source-checks.json")
	body, _ := os.ReadFile(path)
	var rows []CommunitySourceCheck
	_ = json.Unmarshal(body, &rows)
	found := false
	for i, existing := range rows {
		if existing.ID == row.ID {
			rows[i] = row
			found = true
			break
		}
	}
	if !found {
		rows = append(rows, row)
	}
	body, err = json.MarshalIndent(rows, "", "  ")
	if err == nil {
		_ = WriteFileAtomic(path, body, 0600)
	}
}

// ClearCommunitySourceCheck drops the stored result for a source so a changed
// server or credential configuration cannot keep showing stale verification.
func ClearCommunitySourceCheck(id string) {
	sourceChecksMu.Lock()
	defer sourceChecksMu.Unlock()
	dir, err := GetAppDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, "community-source-checks.json")
	body, _ := os.ReadFile(path)
	var rows []CommunitySourceCheck
	if json.Unmarshal(body, &rows) != nil {
		return
	}
	kept := rows[:0]
	for _, row := range rows {
		if row.ID != id {
			kept = append(kept, row)
		}
	}
	if len(kept) == len(rows) {
		return
	}
	if updated, err := json.MarshalIndent(kept, "", "  "); err == nil {
		_ = WriteFileAtomic(path, updated, 0600)
	}
}

// expectSourceJSON converts an HTML interstitial into an authentication result.
// Several public hosts answer with a browser verification page instead of the
// API, which is an access problem rather than a malformed payload.
func expectSourceJSON(data []byte) error {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return &communityAccessError{Status: 403}
	}
	switch trimmed[0] {
	case '{', '[':
		return nil
	}
	return &communityAccessError{Status: 403}
}

// communityChallengeHTML reports an identifiable login or bot-check page.
// Ordinary parse failures stay parse failures; only a document that looks like
// an interstitial is an authentication result.
func communityChallengeHTML(body []byte) bool {
	if len(body) > 32<<10 {
		body = body[:32<<10]
	}
	trimmed := bytes.TrimLeft(body, " \t\r\n\u0000")
	if len(trimmed) == 0 || trimmed[0] != '<' {
		return false
	}
	text := strings.ToLower(string(trimmed))
	if !strings.HasPrefix(text, "<!doctype html") && !strings.Contains(text, "<html") {
		return false
	}
	for _, marker := range []string{
		"just a moment",
		"cf-challenge",
		"cf-turnstile",
		"challenges.cloudflare.com",
		"challenge-platform",
		"attention required",
		"checking your browser",
		"verify you are human",
		"enable javascript",
		"captcha",
		"sign in",
		"log in",
		"login",
		"access denied",
		"browser verification",
		"cloudflare",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func sourceCheckFixture(source CommunitySource) (SourceTrack, string) {
	track := SourceTrack{Title: "Come Together", Artist: "The Beatles", ISRC: "GBAYE0601690", Duration: 260}
	quality := "16"
	switch source.Service {
	case "tidal":
		track.ID = "55130631"
	case "qobuz":
		track.ID = "30369895"
	case "amazon":
		track.ID = "B07FSSGBJV"
		track.ServiceURL = "https://music.amazon.com/tracks/B07FSSGBJV"
	case "deezer":
		track.ID = "116348128"
	case "apple":
		track.ID = "6818177515"
	}
	return track, quality
}

func findCommunitySource(id string) (CommunitySource, error) {
	rows, err := ListCommunitySources()
	if err != nil {
		return CommunitySource{}, err
	}
	for _, source := range rows {
		if source.ID == id {
			if source.BaseURL == "" {
				return source, fmt.Errorf("configure this server URL first")
			}
			return source, nil
		}
	}
	return CommunitySource{}, fmt.Errorf("unknown community source")
}

func CheckCommunitySource(id string, download bool) (result CommunitySourceCheck, resultErr error) {
	source, err := findCommunitySource(id)
	if err != nil {
		return result, err
	}
	// Acquire the download lease before the deferred save below, so a busy
	// queue is reported to the caller without being stored as a failed check.
	if download {
		if err = TryBeginTrackedDownload(); err != nil {
			return result, err
		}
		defer EndTrackedDownload()
	}
	started := time.Now()
	result = CommunitySourceCheck{ID: id, State: "failed", CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	defer func() {
		result.LatencyMS = float64(time.Since(started)) / float64(time.Millisecond)
		saveCommunityCheck(result)
	}()
	track, quality := sourceCheckFixture(source)
	if !download {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err = probeCommunitySourceAPI(ctx, source)
		if err == nil {
			result.State = "available"
			result.Message = "API responded; audio download has not been verified"
		}
	} else {
		_, finish := BeginDownloadCancellationScope()
		defer finish()
		timer := time.AfterFunc(3*time.Minute, ForceStopActiveDownloads)
		defer timer.Stop()
		dir, makeErr := os.MkdirTemp("", "auralis-community-sample-")
		if makeErr != nil {
			return result, makeErr
		}
		defer os.RemoveAll(dir)
		path, downloadErr := downloadCommunitySource(source, track, map[string]string{"tidal": "LOSSLESS", "qobuz": "6", "amazon": "16", "deezer": "16", "apple": "16"}[source.Service], filepath.Join(dir, "sample.flac"))
		err = downloadErr
		if err == nil {
			err = validateCommunitySample(path, track.Duration, quality)
		}
		if err == nil {
			result.State = "validated"
			result.AudioVerified = true
			result.Message = "Full-track audio and decoding passed"
		}
		recordSourceOutcome(id, source.Service, quality, time.Since(started), err == nil, err)
	}
	if err != nil {
		result.Message = redactCommunityMessage(err.Error(), communitySecretValues(source, nil)...)
		var access *communityAccessError
		if errors.As(err, &access) {
			result.State = "authentication_required"
		} else if IsDownloadCancelledError(err) {
			result.State = "cancelled"
		}
		if !IsDownloadCancelledError(err) {
			communityCircuit.Lock()
			communityCircuit.until[id] = time.Now().Add(10 * time.Minute)
			communityCircuit.Unlock()
		}
	} else {
		communityCircuit.Lock()
		delete(communityCircuit.until, id)
		communityCircuit.Unlock()
	}
	return result, nil
}

// probeCommunitySourceAPI asks the source for metadata or a stream URL.
// A nil error means the API answered. It does not download or decode audio,
// so callers must leave AudioVerified false.
func probeCommunitySourceAPI(ctx context.Context, source CommunitySource) error {
	track, quality := sourceCheckFixture(source)
	switch source.Protocol {
	case "hifi":
		_, err := resolveHiFiSource(ctx, source, track.ID, "LOSSLESS")
		return err
	case "qobuz-rest", "qobuz-dl":
		_, err := resolveQobuzSource(ctx, source, track.ID, "6")
		return err
	case "dab":
		_, err := resolveDABSource(ctx, source, track, "6")
		return err
	case "lucida":
		return probeLucidaMetadata(ctx, source, track)
	case "subsonic":
		_, err := resolveSubsonicSource(ctx, source, track, quality)
		return err
	default:
		return fmt.Errorf("unsupported community adapter")
	}
}

func probeLucidaMetadata(ctx context.Context, source CommunitySource, track SourceTrack) error {
	params := url.Values{"url": {"https://open.qobuz.com/track/" + track.ID}, "country": {"US"}}
	if source.Service == "amazon" {
		params = url.Values{"url": {"https://music.amazon.com/tracks/" + track.ID}}
	}
	body, _, err := sourceResponse(source, http.MethodGet, "/", params, nil, ctx)
	if err != nil {
		return err
	}
	if communityChallengeHTML(body) {
		return &communityAccessError{Status: 403}
	}
	data, err := lucidaPageData(body)
	if err != nil {
		return err
	}
	info, ok := data["info"].(map[string]any)
	if !ok || info["type"] != "track" {
		return fmt.Errorf("Lucida did not return track metadata")
	}
	return nil
}

// communityCodecMatches reports whether a community route delivered the codec
// family that was requested. Servers may transcode silently, so a lossless
// request answered with MP3 or AAC must not be recorded as a success.
func communityCodecMatches(codec, quality string) bool {
	if sourceQuality(quality) == "atmos" {
		return codec == "eac3"
	}
	return codec == "flac" || codec == "alac"
}

// communityCodecReader is replaced in tests that serve placeholder media.
var communityCodecReader = readCommunityCodec

func readCommunityCodec(path string) (string, error) {
	probe, err := GetFFprobePath()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ActiveDownloadContext(), 30*time.Second)
	defer cancel()
	body, err := exec.CommandContext(ctx, probe, "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name", "-of", "json", path).Output()
	if err != nil {
		return "", fmt.Errorf("downloaded file was not readable audio")
	}
	var media struct {
		Streams []struct {
			Codec string `json:"codec_name"`
		} `json:"streams"`
	}
	if json.Unmarshal(body, &media) != nil || len(media.Streams) == 0 {
		return "", fmt.Errorf("downloaded file had no audio stream")
	}
	return media.Streams[0].Codec, nil
}

// requireCommunityCodec probes a downloaded community file before the shared
// duration and bit-depth validation accepts it.
func requireCommunityCodec(path, quality string) error {
	codec, err := communityCodecReader(path)
	if err != nil {
		return err
	}
	if !communityCodecMatches(codec, quality) {
		return fmt.Errorf("source returned %s instead of the requested format", codec)
	}
	return nil
}

func validateCommunitySample(path string, expected int, quality string) error {
	probe, err := GetFFprobePath()
	if err != nil {
		return err
	}
	decoder, err := GetFFmpegPath()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ActiveDownloadContext(), 30*time.Second)
	defer cancel()
	body, err := exec.CommandContext(ctx, probe, "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,bits_per_raw_sample:format=duration", "-of", "json", path).Output()
	if err != nil {
		return fmt.Errorf("downloaded file was not readable audio")
	}
	var media struct {
		Streams []struct {
			Codec string `json:"codec_name"`
			Bits  string `json:"bits_per_raw_sample"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if json.Unmarshal(body, &media) != nil || len(media.Streams) == 0 {
		return fmt.Errorf("downloaded file had no audio stream")
	}
	duration, _ := strconv.ParseFloat(media.Format.Duration, 64)
	if duration <= 35 || math.Abs(duration-float64(expected)) > 15 {
		return fmt.Errorf("source returned a preview or a different duration")
	}
	if !communityCodecMatches(media.Streams[0].Codec, quality) {
		return fmt.Errorf("source did not return lossless audio")
	}
	if sourceQuality(quality) == "24" {
		bits, _ := strconv.Atoi(media.Streams[0].Bits)
		if bits <= 16 {
			return fmt.Errorf("source did not deliver 24-bit audio")
		}
	}
	if _, err := exec.CommandContext(ctx, decoder, "-v", "error", "-xerror", "-i", path, "-map", "0:a:0", "-f", "null", "-").CombinedOutput(); err != nil {
		return fmt.Errorf("full-track audio decoding failed")
	}
	return nil
}

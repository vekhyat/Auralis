package syncengine

import "strings"

// FormatPolicy decides whether a source file is copied or transcoded.
type FormatPolicy struct {
	// Mode is "keep", "aac", "opus" or "mp3".
	Mode string `json:"mode"`
}

// Modes is the fixed set of policy values.
var Modes = []string{"keep", "aac", "opus", "mp3"}

func NormalizePolicy(mode string) FormatPolicy {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "aac", "aac256", "aac 256":
		return FormatPolicy{Mode: "aac"}
	case "opus", "opus160", "opus 160":
		return FormatPolicy{Mode: "opus"}
	case "mp3", "mp3v0", "mp3 v0":
		return FormatPolicy{Mode: "mp3"}
	default:
		return FormatPolicy{Mode: "keep"}
	}
}

// losslessExts are sources a transcode policy converts. Lossy sources are
// always copied unchanged — re-encoding lossy audio only makes it worse.
//
// ".m4a"/".mp4"/".m4b" are deliberately absent: the container holds either
// lossless ALAC or lossy AAC, so the extension alone cannot decide. Use
// IsLossless (flag/codec aware) for per-track decisions; the Ext-only
// helpers below keep treating unknown ".m4a" as lossy (copy) so stale
// tracks never get degraded.
var losslessExts = map[string]bool{
	".flac": true, ".wav": true, ".aiff": true, ".aif": true,
	".alac": true, ".ape": true, ".wv": true,
}

// losslessCodecs are audio codec ids that count as lossless regardless of
// container. The scanner stores these lower-cased in SourceTrack.Codec.
var losslessCodecs = map[string]bool{
	"flac": true, "alac": true, "wav": true, "aiff": true, "aif": true,
	"ape": true, "wv": true, "wvc": true, "pcm": true,
	"pcm_s16le": true, "pcm_s16be": true, "pcm_s24le": true, "pcm_s24be": true,
	"pcm_s32le": true, "pcm_s32be": true, "pcm_f32le": true, "pcm_f64le": true,
	"tta": true, "tak": true, "optimfrog": true, "ofr": true, "shn": true,
}

func normalizeCodec(c string) string {
	return strings.ToLower(strings.TrimSpace(c))
}

func losslessCodec(c string) bool {
	if c == "" {
		return false
	}
	return losslessCodecs[c]
}

// NeedsTranscode reports whether srcExt is converted under this policy.
func (p FormatPolicy) NeedsTranscode(srcExt string) bool {
	if p.Mode != "aac" && p.Mode != "opus" && p.Mode != "mp3" {
		return false
	}
	return losslessExts[strings.ToLower(srcExt)]
}

// NeedsTranscodeTrack reports whether the track is converted under this
// policy, using its Lossless flag and Codec id before the extension so
// ALAC-in-".m4a" is honoured. Tracks without flag or codec fall back to
// the extension-only rule.
func (p FormatPolicy) NeedsTranscodeTrack(tr *SourceTrack) bool {
	if p.Mode != "aac" && p.Mode != "opus" && p.Mode != "mp3" {
		return false
	}
	return IsLossless(tr)
}

// RemoteExt returns the on-device extension for a source extension.
func (p FormatPolicy) RemoteExt(srcExt string) string {
	srcExt = strings.ToLower(srcExt)
	if !p.NeedsTranscode(srcExt) {
		if srcExt == "" {
			return ".audio"
		}
		return srcExt
	}
	switch p.Mode {
	case "aac":
		return ".m4a"
	case "opus":
		return ".opus"
	case "mp3":
		return ".mp3"
	}
	return srcExt
}

// RemoteExtForTrack returns the on-device extension for a source track,
// honouring its Lossless flag and Codec id so ALAC-in-".m4a" is converted
// like any other lossless source. Extension-only callers keep using
// RemoteExt.
func (p FormatPolicy) RemoteExtForTrack(tr *SourceTrack) string {
	ext := ""
	if tr != nil {
		ext = strings.ToLower(tr.Ext)
	}
	if !p.NeedsTranscodeTrack(tr) {
		if ext == "" {
			return ".audio"
		}
		return ext
	}
	switch p.Mode {
	case "aac":
		return ".m4a"
	case "opus":
		return ".opus"
	case "mp3":
		return ".mp3"
	}
	return ext
}

// SettingsKey identifies the transcode settings for cache keys and the
// manifest's transcode field.
func (p FormatPolicy) SettingsKey() string {
	if p.Mode == "keep" {
		return "keep"
	}
	return p.Mode
}

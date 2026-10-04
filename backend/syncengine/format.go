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
var losslessExts = map[string]bool{
	".flac": true, ".wav": true, ".aiff": true, ".aif": true,
	".alac": true, ".ape": true, ".wv": true,
}

// NeedsTranscode reports whether srcExt is converted under this policy.
func (p FormatPolicy) NeedsTranscode(srcExt string) bool {
	if p.Mode != "aac" && p.Mode != "opus" && p.Mode != "mp3" {
		return false
	}
	return losslessExts[strings.ToLower(srcExt)]
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

// SettingsKey identifies the transcode settings for cache keys and the
// manifest's transcode field.
func (p FormatPolicy) SettingsKey() string {
	if p.Mode == "keep" {
		return "keep"
	}
	return p.Mode
}

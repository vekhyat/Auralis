package backend

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const legacyTidalAPICacheFile = "tidal-api-urls.json"

func normalizeCustomTidalAPIValue(value interface{}) string {
	customAPI, _ := value.(string)
	customAPI = strings.TrimRight(strings.TrimSpace(customAPI), "/")
	if strings.HasPrefix(customAPI, "https://") {
		return customAPI
	}
	return ""
}

// A saved single-store choice survives only for services that still pass
// full-audio checks; anything else (including retired TIDAL/Amazon) becomes
// "auto" so an old setting cannot pin downloads to a failing route.
func sanitizeDownloaderValue(value interface{}) string {
	downloader, _ := value.(string)
	downloader = strings.TrimSpace(strings.ToLower(downloader))
	if isSupportedAutoService(downloader) {
		return downloader
	}
	return "auto"
}

func sanitizeAutoOrderValue(value interface{}) string {
	autoOrder, _ := value.(string)
	fallback := strings.Join(defaultAutoServices, "-")

	seen := make(map[string]struct{})
	parts := make([]string, 0, 3)
	for _, rawPart := range strings.Split(strings.TrimSpace(strings.ToLower(autoOrder)), "-") {
		part := strings.TrimSpace(rawPart)
		if part == "" {
			continue
		}
		if !isSupportedAutoService(part) {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		parts = append(parts, part)
	}

	if len(parts) < 2 {
		return fallback
	}

	return strings.Join(parts, "-")
}

func SanitizeSettingsMap(settings map[string]interface{}) map[string]interface{} {
	if settings == nil {
		return nil
	}

	sanitized := make(map[string]interface{}, len(settings))
	for key, value := range settings {
		sanitized[key] = value
	}

	customAPI := normalizeCustomTidalAPIValue(sanitized["customTidalApi"])
	sanitized["customTidalApi"] = customAPI
	sanitized["downloader"] = sanitizeDownloaderValue(sanitized["downloader"])
	sanitized["autoOrder"] = sanitizeAutoOrderValue(sanitized["autoOrder"])
	if replayGainMode, exists := sanitized["autoReplayGainMode"]; exists {
		if value, _ := replayGainMode.(string); strings.EqualFold(strings.TrimSpace(value), "track") {
			sanitized["autoReplayGainMode"] = "track"
		} else {
			sanitized["autoReplayGainMode"] = "album"
		}
	}

	return sanitized
}

func CleanupLegacyTidalPublicAPIState() error {
	appDir, err := EnsureAppDataDir()
	if err != nil {
		return err
	}

	cachePath := filepath.Join(appDir, legacyTidalAPICacheFile)
	if err := os.Remove(cachePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func GetDefaultMusicPath() string {

	homeDir, err := os.UserHomeDir()
	if err != nil {

		return "C:\\Users\\Public\\Music"
	}

	return filepath.Join(homeDir, "Music")
}

func GetConfigPath() (string, error) {
	dir, err := EnsureAppDataDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "config.json"), nil
}

func GetRedownloadWithSuffixSetting() bool {
	settings, err := LoadConfigSettings()
	if err != nil || settings == nil {
		return false
	}

	enabled, ok := settings["redownloadWithSuffix"].(bool)
	if !ok {
		return false
	}
	return enabled
}

func normalizeExistingFileCheckMode(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "isrc", "upc":
		return "isrc"
	case "hybrid":
		return "hybrid"
	default:
		return "filename"
	}
}

func GetExistingFileCheckModeSetting() string {
	settings, err := LoadConfigSettings()
	if err != nil || settings == nil {
		return "filename"
	}

	rawMode, _ := settings["existingFileCheckMode"].(string)
	return normalizeExistingFileCheckMode(rawMode)
}

func GetLinkResolverSetting() string {
	settings, err := LoadConfigSettings()
	if err != nil || settings == nil {
		return linkResolverProviderDeezerSongLink
	}

	resolver, _ := settings["linkResolver"].(string)
	switch strings.TrimSpace(strings.ToLower(resolver)) {
	case "songlink", linkResolverProviderDeezerSongLink:
		return linkResolverProviderDeezerSongLink
	case "songstats":
		return linkResolverProviderSongstats
	case "":
		return linkResolverProviderDeezerSongLink
	default:
		return linkResolverProviderDeezerSongLink
	}
}

func GetLinkResolverAllowFallback() bool {
	settings, err := LoadConfigSettings()
	if err != nil || settings == nil {
		return true
	}

	allowFallback, ok := settings["allowResolverFallback"].(bool)
	if !ok {
		return true
	}

	return allowFallback
}

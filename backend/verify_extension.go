package backend

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "embed"
)

//go:embed assets/verify-extension/content.js
var verifyExtensionContentJS string

func verifyExtensionDir() (string, error) {
	dir, err := EnsureAppDir()
	if err != nil {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "verify_extension")
	if err := os.MkdirAll(path, 0700); err != nil {
		return "", err
	}
	return path, nil
}

func verifyExtensionHosts() []string {
	hosts := []string{
		"https://api.zarz.moe/*",
		"https://verify.spotbye.qzz.io/*",
	}
	seen := map[string]struct{}{
		hosts[0]: {},
		hosts[1]: {},
	}
	if raw := strings.TrimSpace(GetCommunityVerifyURL()); raw != "" {
		if parsed, err := url.Parse(raw); err == nil {
			host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
			if host != "" {
				pattern := "https://" + host + "/*"
				if _, ok := seen[pattern]; !ok {
					hosts = append(hosts, pattern)
					seen[pattern] = struct{}{}
				}
			}
		}
	}
	return hosts
}

func writeVerifyExtension() (string, error) {
	dir, err := verifyExtensionDir()
	if err != nil {
		return "", err
	}
	manifest := map[string]any{
		"manifest_version": 3,
		"name":             "Auralis Verification",
		"version":          "1.0.0",
		"content_scripts": []map[string]any{{
			"matches":    verifyExtensionHosts(),
			"js":         []string{"content.js"},
			"run_at":     "document_start",
			"all_frames": false,
		}},
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifestBytes, 0600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "content.js"), []byte(verifyExtensionContentJS), 0600); err != nil {
		return "", err
	}
	return dir, nil
}

func verifyExtensionLoadArgs(extensionDir string) []string {
	if strings.TrimSpace(extensionDir) == "" {
		return nil
	}
	return []string{
		"--disable-features=DisableLoadExtensionCommandLineSwitch",
		"--load-extension=" + extensionDir,
		"--disable-extensions-except=" + extensionDir,
	}
}

func mustWriteVerifyExtension() string {
	dir, err := writeVerifyExtension()
	if err != nil {
		fmt.Printf("Could not prepare verification branding extension: %v\n", err)
		return ""
	}
	return dir
}

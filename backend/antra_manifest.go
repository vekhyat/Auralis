package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Public Antra endpoint gist. Antra ships this URL to every desktop client
// so the mirror pool can change without an app update.
const antraEndpointManifestURL = "https://gist.githubusercontent.com/anandprtp/fdc2c16b7bfdc2d337fbc86161b79371/raw"

const antraManifestTTL = 30 * time.Minute

type antraEndpointManifest struct {
	Mirrors struct {
		Tidal  string `json:"tidal"`
		Qobuz  string `json:"qobuz"`
		Deezer string `json:"deezer"`
		Amazon string `json:"amazon"`
		Apple  string `json:"apple"`
	} `json:"mirrors"`
	APIKey string `json:"api_key"`
}

func (m antraEndpointManifest) mirrorURL(service string) string {
	var raw string
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "tidal":
		raw = m.Mirrors.Tidal
	case "qobuz":
		raw = m.Mirrors.Qobuz
	case "deezer":
		raw = m.Mirrors.Deezer
	case "amazon":
		raw = m.Mirrors.Amazon
	case "apple":
		raw = m.Mirrors.Apple
	}
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

var (
	antraManifestMu      sync.Mutex
	antraManifestCache   antraEndpointManifest
	antraManifestFetched time.Time
	antraManifestOK      bool
)

func loadAntraEndpointManifest() (antraEndpointManifest, error) {
	antraManifestMu.Lock()
	defer antraManifestMu.Unlock()

	if antraManifestOK && time.Since(antraManifestFetched) < antraManifestTTL {
		return antraManifestCache, nil
	}

	req, err := NewRequestWithDefaultHeaders(http.MethodGet, antraEndpointManifestURL, nil)
	if err != nil {
		if antraManifestOK {
			return antraManifestCache, nil
		}
		return antraEndpointManifest{}, err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if antraManifestOK {
			return antraManifestCache, nil
		}
		return antraEndpointManifest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if antraManifestOK {
			return antraManifestCache, nil
		}
		return antraEndpointManifest{}, fmt.Errorf("antra manifest HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if antraManifestOK {
			return antraManifestCache, nil
		}
		return antraEndpointManifest{}, err
	}
	var parsed antraEndpointManifest
	if err := json.Unmarshal(body, &parsed); err != nil {
		if antraManifestOK {
			return antraManifestCache, nil
		}
		return antraEndpointManifest{}, fmt.Errorf("antra manifest: %w", err)
	}
	parsed.APIKey = strings.TrimSpace(parsed.APIKey)
	antraManifestCache = parsed
	antraManifestFetched = time.Now()
	antraManifestOK = true
	return parsed, nil
}

func antraMirrorBase(service string) (baseURL, apiKey string, err error) {
	manifest, err := loadAntraEndpointManifest()
	if err != nil {
		return "", "", err
	}
	base := manifest.mirrorURL(service)
	if base == "" {
		return "", "", fmt.Errorf("antra %s mirror is not published", service)
	}
	return base, manifest.APIKey, nil
}

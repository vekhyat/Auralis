package backend

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func zarzDownloadJSON(provider, path string, payload any, ticketID string) ([]byte, error) {
	headers := map[string]string{
		"X-Zarz-Ticket": ticketID,
	}
	body, err := zarzSignedJSON(zarzAppVersionForProvider(provider), http.MethodPost, path, payload, headers)
	if err != nil {
		return nil, fmt.Errorf("zarz %s: %w", path, err)
	}
	return body, nil
}

func zarzDownloadWithRetry(provider, path, resourceType, resourceID string, payload any) ([]byte, error) {
	publicName := zarzPublicProviderName(provider)
	if err := skipIfProviderRateLimited(publicName); err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 1; attempt <= zarzMaxProviderTries; attempt++ {
		ticketID, err := mintZarzTicket(provider, resourceType, resourceID)
		if err != nil {
			lastErr = err
			if zarzCanRetry(err, attempt) {
				wait := zarzRetryDelay(err)
				fmt.Printf("Zarz %s ticket unavailable, retrying in %s (%d/%d)...\n", provider, wait, attempt, zarzMaxProviderTries)
				if sleepErr := SleepWithDownloadContext(wait); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			if IsRateLimitedError(err) {
				markProviderRateLimited(publicName, zarzRetryDelay(err))
			}
			return nil, err
		}
		body, err := zarzDownloadJSON(provider, path, payload, ticketID)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if zarzCanRetry(err, attempt) {
			wait := zarzRetryDelay(err)
			fmt.Printf("Zarz %s temporarily unavailable, retrying in %s (%d/%d)...\n", provider, wait, attempt, zarzMaxProviderTries)
			if sleepErr := SleepWithDownloadContext(wait); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}
		if IsRateLimitedError(err) {
			markProviderRateLimited(publicName, zarzRetryDelay(err))
		}
		return nil, err
	}
	if IsRateLimitedError(lastErr) {
		markProviderRateLimited(publicName, zarzRetryDelay(lastErr))
	}
	return nil, lastErr
}

func zarzCanRetry(err error, attempt int) bool {
	if err == nil || attempt >= zarzMaxProviderTries {
		return false
	}
	var apiErr *zarzAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.isRateLimited() {
		return false
	}
	return apiErr.shouldRetry()
}

func mapTidalQualityToZarz(quality string) string {
	switch strings.ToUpper(strings.TrimSpace(quality)) {
	case "ATMOS", "DOLBY", "DOLBY_ATMOS", "EAC3", "EAC3_JOC":
		return "DOLBY_ATMOS"
	case "HI_RES_LOSSLESS", "HI_RES", "24":
		return "HI_RES_LOSSLESS"
	case "HIGH":
		return "HIGH"
	case "LOW":
		return "LOW"
	default:
		return "LOSSLESS"
	}
}

// fetchZarzAtmosManifest downloads an MPD manifest the gateway references by
// HTTPS URL and returns it standard-base64 encoded, matching the inline
// data:application/dash+xml;base64 payloads so DownloadFromManifest can parse
// either shape. Without this, a raw manifest URL would be handed to the file
// downloader and saved as a corrupt track.
func allowGatewayFetchURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("invalid fetch URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("fetch URL must be http(s)")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("refusing private fetch host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isDisallowedGatewayIP(ip) {
			return fmt.Errorf("refusing private fetch host")
		}
		return nil
	}
	_, err = gatewayDialIPs(host)
	return err
}

var allowGatewayFetch = allowGatewayFetchURL

func isDisallowedGatewayIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func gatewayDialIPs(host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	addrs, err := net.LookupIP(host)
	if err != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("refusing unresolved fetch host")
	}
	for _, addr := range addrs {
		if isDisallowedGatewayIP(addr) {
			return nil, fmt.Errorf("refusing private fetch host")
		}
	}
	return addrs, nil
}

func gatewayHTTPClient(dialIPs []net.IP) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if err := SameHostCheckRedirect(req, via); err != nil {
				return err
			}
			if req.URL == nil {
				return http.ErrUseLastResponse
			}
			return allowGatewayFetch(req.URL.String())
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				_, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				var lastErr error
				for _, ip := range dialIPs {
					conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
					if err == nil {
						return conn, nil
					}
					lastErr = err
				}
				if lastErr == nil {
					return nil, fmt.Errorf("no addresses to dial")
				}
				return nil, lastErr
			},
		},
	}
}

func fetchZarzAtmosManifest(manifestURI string) (string, error) {
	manifestURI = strings.TrimSpace(manifestURI)
	if err := allowGatewayFetch(manifestURI); err != nil {
		return "", err
	}
	parsed, err := url.Parse(manifestURI)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("invalid fetch URL")
	}
	dialIPs, err := gatewayDialIPs(parsed.Hostname())
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodGet, manifestURI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/dash+xml,text/xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("User-Agent", zarzUserAgentFor("tidal-web@1.1.0"))
	resp, err := gatewayHTTPClient(dialIPs).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("manifest request returned HTTP %d", resp.StatusCode)
	}
	manifestBytes, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(manifestBytes)) == 0 {
		return "", fmt.Errorf("manifest response is empty")
	}
	return base64.StdEncoding.EncodeToString(manifestBytes), nil
}

func (t *TidalDownloader) getTidalZarzDownloadURL(trackID int64, quality string) (string, error) {
	trackIDStr := fmt.Sprintf("%d", trackID)
	zarzQuality := mapTidalQualityToZarz(quality)
	fmt.Printf("Trying Zarz Tidal API (%s)...\n", zarzQuality)

	var payload any
	if zarzQuality == "DOLBY_ATMOS" {
		payload = map[string]any{
			"id":       trackIDStr,
			"endpoint": "manifests",
			"formats":  []string{"EAC3_JOC"},
		}
	} else {
		payload = map[string]string{
			"id":      trackIDStr,
			"quality": zarzQuality,
		}
	}

	body, err := zarzDownloadWithRetry("tid", "/dl/tid", "track", trackIDStr, payload)
	if err != nil {
		return "", err
	}

	if zarzQuality == "DOLBY_ATMOS" {
		var manifestResponse TidalManifestAPIResponse
		if err := json.Unmarshal(body, &manifestResponse); err != nil {
			return "", fmt.Errorf("failed to decode zarz tidal atmos response: %w", err)
		}
		attributes := manifestResponse.Data.Data.Attributes
		if !containsString(attributes.Formats, "EAC3_JOC") {
			return "", fmt.Errorf("Dolby Atmos is not available for this track")
		}
		const dataPrefix = "data:application/dash+xml;base64,"
		if strings.HasPrefix(attributes.URI, dataPrefix) {
			return "MANIFEST:" + strings.TrimPrefix(attributes.URI, dataPrefix), nil
		}
		manifestB64, manifestErr := fetchZarzAtmosManifest(attributes.URI)
		if manifestErr == nil {
			return "MANIFEST:" + manifestB64, nil
		}
		fmt.Printf("Zarz Tidal Atmos manifest fetch failed: %v\n", manifestErr)
		return "", fmt.Errorf("zarz tidal atmos response missing manifest URI")
	}

	var v2Response TidalAPIResponseV2
	if err := json.Unmarshal(body, &v2Response); err == nil {
		if strings.EqualFold(v2Response.Data.AssetPresentation, "PREVIEW") {
			return "", fmt.Errorf("zarz tidal returned a preview asset")
		}
		if v2Response.Data.Manifest != "" {
			fmt.Println("Zarz Tidal manifest found")
			return "MANIFEST:" + v2Response.Data.Manifest, nil
		}
	}

	if streamURL := extractQobuzStreamingURL(body); streamURL != "" {
		return streamURL, nil
	}
	return "", fmt.Errorf("no download URL in zarz tidal response")
}

func mapQobuzQualityToZarz(quality string) string {
	switch strings.TrimSpace(quality) {
	case "27":
		return "hi-res-max"
	case "7":
		return "hi-res"
	default:
		return "cd"
	}
}

func (q *QobuzDownloader) getQobuzZarzDownloadURL(trackID int64, quality string) (string, error) {
	trackIDStr := fmt.Sprintf("%d", trackID)
	trackURL := fmt.Sprintf("https://open.qobuz.com/track/%s", trackIDStr)
	zarzQuality := mapQobuzQualityToZarz(quality)
	fmt.Printf("Trying Zarz Qobuz API (%s)...\n", zarzQuality)

	payload := map[string]any{
		"quality":      zarzQuality,
		"upload_to_r2": false,
		"id":           trackIDStr,
		"type":         "track",
		"url":          trackURL,
	}
	body, err := zarzDownloadWithRetry("qbz", "/dl/qbz", "track", trackURL, payload)
	if err != nil {
		return "", err
	}
	downloadURL := extractQobuzStreamingURL(body)
	if downloadURL == "" {
		return "", fmt.Errorf("no streamable URL in zarz qobuz response")
	}
	fmt.Println("Zarz Qobuz URL found")
	return downloadURL, nil
}

type zarzAmazonAudio struct {
	URL      string   `json:"url"`
	Key      string   `json:"key"`
	Codec    string   `json:"codec"`
	Captcha  string   `json:"captcha"`
	KeySpecs []string `json:"key_specs"`
}

type zarzAmazonItem struct {
	Audio zarzAmazonAudio `json:"audio"`
	URL   string          `json:"url"`
	Key   string          `json:"key"`
	Codec string          `json:"codec"`
}

func mapAmazonQualityToZarzCodec(quality string) string {
	if amazonCommunityNormalizeQuality(quality) == "atmos" {
		return "eac3"
	}
	return "flac"
}

func amazonShouldFallbackToFlac(quality string) bool {
	return mapAmazonQualityToZarzCodec(quality) != "flac"
}

func (a *AmazonDownloader) getAmazonZarzStream(asin, quality string) (amazonCommunityResponse, error) {
	codec := mapAmazonQualityToZarzCodec(quality)
	fmt.Printf("Trying Zarz Amazon API (ASIN %s, %s)...\n", asin, codec)

	payload := map[string]string{
		"asin":  asin,
		"codec": codec,
	}
	body, err := zarzDownloadWithRetry("amazeamazeamaze", "/dl/amazeamazeamaze", "track", asin, payload)
	if err != nil {
		return amazonCommunityResponse{}, err
	}

	var items []zarzAmazonItem
	if err := json.Unmarshal(body, &items); err != nil {
		var single zarzAmazonItem
		if err2 := json.Unmarshal(body, &single); err2 != nil {
			return amazonCommunityResponse{}, fmt.Errorf("invalid zarz amazon response: %w", err)
		}
		items = []zarzAmazonItem{single}
	}
	if len(items) == 0 {
		return amazonCommunityResponse{}, fmt.Errorf("zarz amazon returned an empty response")
	}
	item := items[0]
	streamURL := strings.TrimSpace(item.Audio.URL)
	if streamURL == "" {
		streamURL = strings.TrimSpace(item.URL)
	}
	if streamURL == "" {
		return amazonCommunityResponse{}, fmt.Errorf("no stream URL in zarz amazon response")
	}
	key := strings.TrimSpace(item.Audio.Key)
	if key == "" {
		key = strings.TrimSpace(item.Key)
	}
	keySpecs := item.Audio.KeySpecs
	if len(keySpecs) == 0 && key != "" {
		keySpecs = []string{key}
	}
	outCodec := strings.TrimSpace(item.Audio.Codec)
	if outCodec == "" {
		outCodec = strings.TrimSpace(item.Codec)
	}
	if outCodec == "" {
		outCodec = codec
	}
	fmt.Println("Zarz Amazon stream found")
	return amazonCommunityResponse{
		ASIN:      asin,
		Codec:     outCodec,
		URL:       streamURL,
		StreamURL: streamURL,
		Key:       key,
		KeySpecs:  keySpecs,
		Captcha:   item.Audio.Captcha,
	}, nil
}

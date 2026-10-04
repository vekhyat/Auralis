package backend

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/titanous/json5"
)

var lucidaJobLabel = regexp.MustCompile(`^[A-Za-z0-9_-]{1,120}$`)

// lucidaWorkerSource points a handoff at the worker host. The environment
// cookie is an opaque string with no domain, so it is not forwarded onto a
// subdomain. Browser-jar cookies are attached later only when their own
// domain and path include that host.
func lucidaWorkerSource(source CommunitySource, server string) (CommunitySource, error) {
	if !lucidaJobLabel.MatchString(server) {
		return source, fmt.Errorf("Lucida did not create a download job")
	}
	u, err := url.Parse(source.BaseURL)
	if err != nil || u.Hostname() == "" {
		return source, fmt.Errorf("invalid Lucida server URL")
	}
	ip := net.ParseIP(u.Hostname())
	local := u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
	if local {
		return source, nil
	}
	worker := source
	u.Host = server + "." + u.Hostname()
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	worker.BaseURL = strings.TrimRight(u.String(), "/")
	worker.CredentialEnv = ""
	worker.CredentialType = ""
	return worker, nil
}

func lucidaPageData(body []byte) (map[string]any, error) {
	text := string(body)
	start := strings.Index(text, `,{"type":"data","data":`)
	if start < 0 {
		return nil, fmt.Errorf("Lucida page did not contain download data")
	}
	text = text[start+len(`,{"type":"data","data":`):]
	end := strings.Index(text, `,"uses":{"url":1}}];`)
	if end < 0 {
		return nil, fmt.Errorf("Lucida page data format changed")
	}
	var data map[string]any
	if json5.Unmarshal([]byte(text[:end]), &data) != nil {
		return nil, fmt.Errorf("Lucida page data could not be read")
	}
	return data, nil
}

func resolveLucidaSource(ctx context.Context, source CommunitySource, track SourceTrack, quality string) (string, error) {
	if sourceQuality(quality) == "atmos" {
		return "", fmt.Errorf("Lucida does not expose an Atmos selection through this adapter")
	}
	canonical := track.ServiceURL
	if canonical == "" {
		if source.Service == "qobuz" {
			canonical = "https://open.qobuz.com/track/" + track.ID
		} else {
			canonical = "https://music.amazon.com/tracks/" + track.ID
		}
	}
	params := url.Values{"url": {canonical}}
	country := ""
	if source.Service == "qobuz" {
		country = "US"
		params.Set("country", country)
	}
	body, _, err := sourceResponse(source, http.MethodGet, "/", params, nil, ctx)
	if err != nil {
		return "", err
	}
	if communityChallengeHTML(body) {
		return "", &communityAccessError{Status: 403}
	}
	data, err := lucidaPageData(body)
	if err != nil {
		return "", err
	}
	info, ok := data["info"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("Lucida did not return track information")
	}
	kind, _ := info["type"].(string)
	if kind != "track" && kind != "song" {
		return "", fmt.Errorf("Lucida did not return a single track")
	}
	trackURL, _ := info["url"].(string)
	requested, reqErr := url.Parse(canonical)
	returned, returnErr := url.Parse(trackURL)
	if reqErr != nil || returnErr != nil || requested.Hostname() != returned.Hostname() || requested.Path != returned.Path {
		return "", fmt.Errorf("Lucida returned a different track URL")
	}
	primary := info["csrf"]
	if primary == nil {
		token, _ := data["token"].(string)
		original := token
		for i := 0; i < 2; i++ {
			decoded, decodeErr := base64.StdEncoding.DecodeString(token)
			if decodeErr != nil || !utf8.Valid(decoded) {
				token = original
				break
			}
			token = string(decoded)
		}
		primary = token
	}
	if primary == nil || primary == "" {
		return "", fmt.Errorf("Lucida download token is unavailable")
	}
	account := country
	if account == "" {
		account = "auto"
	}
	payload := map[string]any{"account": map[string]string{"id": account, "type": "country"}, "compat": false, "downscale": "original", "handoff": true, "metadata": true, "private": false, "token": map[string]any{"expiry": data["expiry"], "primary": primary, "secondary": info["csrfFallback"]}, "upload": map[string]bool{"enabled": false}, "url": trackURL}
	body, _, err = sourceResponse(source, http.MethodPost, "/api/load", url.Values{"url": {"/api/fetch/stream/v2"}}, payload, ctx)
	if err != nil {
		return "", err
	}
	if communityChallengeHTML(body) {
		return "", &communityAccessError{Status: 403}
	}
	var job struct {
		Handoff string `json:"handoff"`
		Server  string `json:"server"`
	}
	if json5.Unmarshal(body, &job) != nil || !lucidaJobLabel.MatchString(job.Handoff) || !lucidaJobLabel.MatchString(job.Server) {
		return "", fmt.Errorf("Lucida did not create a download job")
	}
	worker, err := lucidaWorkerSource(source, job.Server)
	if err != nil {
		return "", err
	}
	path := "/api/fetch/request/" + job.Handoff
	for {
		body, _, err = sourceResponse(worker, http.MethodGet, path, nil, nil, ctx)
		if err != nil {
			return "", err
		}
		if communityChallengeHTML(body) {
			return "", &communityAccessError{Status: 403}
		}
		var status struct {
			Status string `json:"status"`
		}
		if json5.Unmarshal(body, &status) != nil {
			return "", fmt.Errorf("invalid Lucida job status")
		}
		switch status.Status {
		case "completed":
			return sourceURL(worker.BaseURL + path + "/download")
		case "error":
			return "", fmt.Errorf("Lucida could not complete the download")
		}
		select {
		case <-ctx.Done():
			return "", communityContextError(ctx)
		case <-time.After(time.Second):
		}
	}
}

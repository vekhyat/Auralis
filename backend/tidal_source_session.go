package backend

import (
	"fmt"
	"net/http"
)

func (t *TidalDownloader) applyMediaCredentials(req *http.Request) error {
	if t.communitySource == nil {
		return nil
	}
	header, err := communityCookieHeader(*t.communitySource, req.URL.String())
	if err != nil {
		return err
	}
	if header != "" {
		req.Header.Set("Cookie", header)
		if identity := browserUserAgentForSource(*t.communitySource); identity != "" {
			req.Header.Set("User-Agent", identity)
		}
	}
	return nil
}

func (t *TidalDownloader) mediaHTTPClient() *http.Client {
	client := newMediaHTTPClient()
	if t.communitySource == nil {
		return client
	}
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if _, err := sourceURL(next.URL.String()); err != nil {
			return fmt.Errorf("invalid source media redirect")
		}
		next.Header.Del("Cookie")
		next.Header.Del("Authorization")
		next.Header.Del("X-API-Key")
		return t.applyMediaCredentials(next)
	}
	return client
}

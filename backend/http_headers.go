package backend

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultDownloaderUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"

// SameHostCheckRedirect follows redirects only when scheme and host stay the
// same, so session/HMAC headers are not forwarded to a different origin.
func SameHostCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if len(via) == 0 {
		return nil
	}
	if req.URL == nil || via[0].URL == nil {
		return http.ErrUseLastResponse
	}
	orig := via[0].URL
	if !strings.EqualFold(req.URL.Scheme, orig.Scheme) || !strings.EqualFold(req.URL.Host, orig.Host) {
		return http.ErrUseLastResponse
	}
	return nil
}

func NewSignedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: SameHostCheckRedirect,
	}
}

func NewRequestWithDefaultHeaders(method string, rawURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", DefaultDownloaderUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")

	return req, nil
}

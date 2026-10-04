package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	mediaDownloadTimeout = 30 * time.Minute
	mediaHeaderTimeout   = 30 * time.Second
	downloadRetryLimit   = 3
)

// downloadIdleTimeout is how long a transfer may go without bytes before it
// is treated as stuck. Tests shorten it.
var downloadIdleTimeout = 25 * time.Second

// ErrDownloadStalled means the server accepted the request and then stopped
// sending. Callers retry it; it is not a permanent failure.
var ErrDownloadStalled = errors.New("download stalled")

func newMediaHTTPClient() *http.Client {
	return &http.Client{
		// A backstop for a transfer that trickles forever. Real stalls are
		// cut off by downloadIdleTimeout, which resets whenever bytes arrive,
		// so a long file can finish.
		Timeout: mediaDownloadTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   15 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          8,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: mediaHeaderTimeout,
			ExpectContinueTimeout: time.Second,
		},
	}
}

func retryableDownloadError(err error) bool {
	if err == nil || IsDownloadCancelledError(err) {
		return false
	}
	if errors.Is(err, ErrDownloadStalled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "tls handshake timeout") ||
		strings.Contains(msg, "stalled")
}

// copyDownloadBody copies until EOF, stop, or downloadIdleTimeout of silence.
// Closing the body unblocks a stalled Read so cancel and the idle watchdog
// actually interrupt the transfer.
func copyDownloadBody(dst io.Writer, body io.ReadCloser) (int64, error) {
	return copyDownloadBodyLimit(dst, body, 0)
}

func copyDownloadBodyLimit(dst io.Writer, body io.ReadCloser, limit int64) (int64, error) {
	if body == nil {
		return 0, fmt.Errorf("empty download body")
	}
	ctx := ActiveDownloadContext()
	activity := make(chan struct{}, 1)
	stopped := make(chan struct{})
	var reason atomic.Int32
	var closeOnce sync.Once
	closeBody := func() {
		closeOnce.Do(func() { _ = body.Close() })
	}
	go func() {
		timer := time.NewTimer(downloadIdleTimeout)
		defer timer.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				reason.Store(1)
				closeBody()
				return
			case <-activity:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(downloadIdleTimeout)
			case <-timer.C:
				reason.Store(2)
				closeBody()
				return
			}
		}
	}()
	defer close(stopped)
	defer closeBody()

	src := io.Reader(body)
	if limit > 0 {
		src = io.LimitReader(body, limit)
	}
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			select {
			case activity <- struct{}{}:
			default:
			}
			written, werr := dst.Write(buf[:n])
			total += int64(written)
			if werr != nil {
				return total, werr
			}
			if written < n {
				return total, io.ErrShortWrite
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			return total, nil
		}
		switch reason.Load() {
		case 1:
			return total, ErrDownloadCancelled
		case 2:
			return total, ErrDownloadStalled
		default:
			if ctx.Err() != nil || IsDownloadCancelledError(err) {
				return total, ErrDownloadCancelled
			}
			return total, err
		}
	}
}

// copyResponseWithRetry re-requests a transfer that stalls. Bytes written by
// a failed attempt are rewound so a retry does not append a second copy.
func copyResponseWithRetry(pw *ProgressWriter, fetch func() (*http.Response, error)) error {
	if pw == nil {
		return fmt.Errorf("missing download writer")
	}
	var lastErr error
	for attempt := 1; attempt <= downloadRetryLimit; attempt++ {
		if err := CheckDownloadCancelled(); err != nil {
			return err
		}
		if attempt > 1 {
			fmt.Printf("Download stalled, retrying (%d/%d): %v\n", attempt, downloadRetryLimit, lastErr)
			if err := SleepWithDownloadContext(time.Duration(attempt-1) * time.Second); err != nil {
				return err
			}
		}
		resp, err := fetch()
		if err != nil {
			lastErr = WrapDownloadCancelled(err)
			if IsDownloadCancelledError(lastErr) || !retryableDownloadError(lastErr) {
				return lastErr
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("download failed with status %d", resp.StatusCode)
		}
		before := pw.GetTotal()
		_, err = copyDownloadBody(pw, resp.Body)
		resp.Body.Close()
		if err == nil {
			return nil
		}
		pw.rewind(pw.GetTotal() - before)
		lastErr = err
		if IsDownloadCancelledError(err) || !retryableDownloadError(err) {
			return err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("download failed")
	}
	return lastErr
}

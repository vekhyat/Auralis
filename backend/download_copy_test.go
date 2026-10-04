package backend

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type stallReader struct {
	mu     sync.Mutex
	sent   bool
	closed chan struct{}
	once   sync.Once
}

func (s *stallReader) Read(p []byte) (int, error) {
	s.mu.Lock()
	if !s.sent && len(p) > 0 {
		p[0] = 'x'
		s.sent = true
		s.mu.Unlock()
		return 1, nil
	}
	s.mu.Unlock()
	<-s.closed
	return 0, errors.New("closed")
}

func (s *stallReader) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func TestCopyDownloadBodyCompletes(t *testing.T) {
	body := io.NopCloser(strings.NewReader("audio-bytes"))
	var dst bytes.Buffer
	n, err := copyDownloadBody(&dst, body)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len("audio-bytes")) || dst.String() != "audio-bytes" {
		t.Fatalf("copied %d %q", n, dst.String())
	}
}

func TestCopyDownloadBodyStalls(t *testing.T) {
	previous := downloadIdleTimeout
	downloadIdleTimeout = 40 * time.Millisecond
	t.Cleanup(func() { downloadIdleTimeout = previous })

	body := &stallReader{closed: make(chan struct{})}
	var dst bytes.Buffer
	_, err := copyDownloadBody(&dst, body)
	if !errors.Is(err, ErrDownloadStalled) {
		t.Fatalf("err = %v, want stalled", err)
	}
	if !retryableDownloadError(err) {
		t.Fatal("stall should be retried")
	}
}

func TestCopyDownloadBodyCancel(t *testing.T) {
	previous := downloadIdleTimeout
	downloadIdleTimeout = time.Minute
	t.Cleanup(func() { downloadIdleTimeout = previous })

	_, stop := BeginDownloadCancellationScope()
	defer stop()
	done := make(chan error, 1)
	go func() {
		body := &stallReader{closed: make(chan struct{})}
		var dst bytes.Buffer
		_, err := copyDownloadBody(&dst, body)
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	ForceStopActiveDownloads()
	select {
	case err := <-done:
		if !IsDownloadCancelledError(err) {
			t.Fatalf("err = %v, want cancelled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not unblock the stalled copy")
	}
}

func TestRetryableDownloadError(t *testing.T) {
	if retryableDownloadError(nil) || retryableDownloadError(ErrDownloadCancelled) {
		t.Fatal("nil and cancellation are not retried")
	}
	if !retryableDownloadError(errors.New("read tcp: connection reset by peer")) {
		t.Fatal("connection reset should be retried")
	}
	if retryableDownloadError(errors.New("download failed with status 404")) {
		t.Fatal("404 is not a stall")
	}
}

package backend

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogResolverSurvivesDownloadStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"pageData":{"entityData":{"isrc":"USABC2600001"}}}}}</script>`))
	}))
	defer server.Close()
	_, finish := BeginDownloadCancellationScope()
	defer finish()
	ForceStopActiveDownloads()
	result, err := NewSongLinkClient().scrapeSongLinkPage(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.ISRC != "USABC2600001" {
		t.Fatalf("ISRC = %q", result.ISRC)
	}
}

func TestDownloadResolverHonorsExplicitCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewSongLinkClientWithContext(ctx).scrapeSongLinkPage("http://127.0.0.1:1", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled resolver = %v", err)
	}
	_, err = requestSpotifyBytes(ctx, &http.Client{}, "http://127.0.0.1:1", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled identifier lookup = %v", err)
	}
}

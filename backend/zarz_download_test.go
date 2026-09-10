package backend

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAllowGatewayFetchURL(t *testing.T) {
	refuse := []string{
		"http://localhost/x",
		"https://127.0.0.1/x",
		"http://[::1]/x",
		"https://10.0.0.1/x",
		"https://192.168.1.1/x",
		"https://169.254.169.254/x",
		"file://secret",
	}
	for _, raw := range refuse {
		if err := allowGatewayFetchURL(raw); err == nil {
			t.Fatalf("%s: expected refusal", raw)
		}
	}
	if err := allowGatewayFetchURL("https://8.8.8.8/x"); err != nil {
		t.Fatalf("https://8.8.8.8/x: %v", err)
	}
}

func TestGatewayDialIPsPinsLiteral(t *testing.T) {
	ips, err := gatewayDialIPs("8.8.8.8")
	if err != nil {
		t.Fatalf("public literal: %v", err)
	}
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("8.8.8.8")) {
		t.Fatalf("pinned IPs = %v", ips)
	}
}

func withStubbedGatewayFetch(t *testing.T) {
	t.Helper()
	previous := allowGatewayFetch
	allowGatewayFetch = func(raw string) error {
		if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
			return nil
		}
		return fmt.Errorf("fetch URL must be http(s)")
	}
	t.Cleanup(func() { allowGatewayFetch = previous })
}

func TestFetchZarzAtmosManifestFollowsSameHostRedirect(t *testing.T) {
	withStubbedGatewayFetch(t)
	manifestXML := []byte(`<MPD xmlns="urn:mpeg:dash:schema:mpd:2011"></MPD>`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/from" {
			http.Redirect(w, r, "/to", http.StatusFound)
			return
		}
		_, _ = w.Write(manifestXML)
	}))
	defer server.Close()

	encoded, err := fetchZarzAtmosManifest(server.URL + "/from")
	if err != nil {
		t.Fatalf("same-host redirect: %v", err)
	}
	decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
	if decodeErr != nil {
		t.Fatalf("invalid base64: %v", decodeErr)
	}
	if string(decoded) != string(manifestXML) {
		t.Fatal("roundtrip mismatch")
	}
}

func TestFetchZarzAtmosManifestBlocksCrossHostRedirect(t *testing.T) {
	withStubbedGatewayFetch(t)
	secret := []byte("private-ssrf-body")
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(secret)
	}))
	defer private.Close()

	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, private.URL, http.StatusFound)
	}))
	defer public.Close()

	encoded, err := fetchZarzAtmosManifest(public.URL)
	if err == nil {
		decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr == nil && string(decoded) == string(secret) {
			t.Fatal("followed redirect to a different host")
		}
		t.Fatal("expected cross-host redirect to fail")
	}
}

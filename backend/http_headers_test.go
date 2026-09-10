package backend

import (
	"net/http"
	"net/url"
	"testing"
)

func TestSameHostCheckRedirectBlocksCrossOrigin(t *testing.T) {
	orig, _ := url.Parse("https://api.zarz.moe/v2/dl")
	next, _ := url.Parse("https://attacker.example/steal")
	err := SameHostCheckRedirect(&http.Request{URL: next}, []*http.Request{{URL: orig}})
	if err != http.ErrUseLastResponse {
		t.Fatalf("cross-origin redirect = %v, want ErrUseLastResponse", err)
	}
}

func TestSameHostCheckRedirectAllowsSameHost(t *testing.T) {
	orig, _ := url.Parse("https://api.zarz.moe/v2/dl")
	next, _ := url.Parse("https://api.zarz.moe/v2/dl/retry")
	if err := SameHostCheckRedirect(&http.Request{URL: next}, []*http.Request{{URL: orig}}); err != nil {
		t.Fatalf("same-host redirect: %v", err)
	}
}

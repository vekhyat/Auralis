package backend

import "testing"

func TestParseSpotifyCatalogHostsAndPlaylistUserPath(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		{"https://open.spotify.com/track/abc123", "track", "abc123"},
		{"https://www.open.spotify.com/album/alb456", "album", "alb456"},
		{"https://play.spotify.com/playlist/pl789", "playlist", "pl789"},
		{"https://open.spotify.com/user/someone/playlist/pluser", "playlist", "pluser"},
		{"spotify:track:uri999", "track", "uri999"},
	}
	for _, tc := range cases {
		got, err := parseSpotifyURI(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got.Type != tc.typ || got.ID != tc.id {
			t.Fatalf("%s: got %+v", tc.in, got)
		}
	}
}

func TestIsSpotifyShareHost(t *testing.T) {
	if !isSpotifyShareHost("spotify.link") || !isSpotifyShareHost("www.spotify.link") {
		t.Fatal("spotify.link should be a share host")
	}
	if isSpotifyShareHost("open.spotify.com") {
		t.Fatal("catalog host is not a share host")
	}
}

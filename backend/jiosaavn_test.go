package backend

import "testing"

func TestJioSaavnPickBestPrefersCloseTitleArtist(t *testing.T) {
	items := []map[string]any{
		{"id": "junk", "title": "Jee Karda", "primaryArtists": "Garry Sandhu"},
		{"id": "wanted", "title": "On Time", "primaryArtists": "Huntrix", "singers": "EJAE"},
	}

	got, score := jioSaavnPickBest(items, "On Time Huntrix EJAE")
	if score <= 0 {
		t.Fatalf("score = %d, want a close match", score)
	}
	if jioSaavnString(got, "id") != "wanted" {
		t.Fatalf("picked %#v, want the close title/artist over Data[0] junk", got)
	}
}

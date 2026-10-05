package library

import (
	"strings"
	"testing"
)

func TestRelativePathPoweramp(t *testing.T) {
	p := ProfileByID(ProfilePoweramp)
	got := p.RelativePath(TrackFields{
		Title: "Paranoid Android", Artist: "Radiohead", AlbumArtist: "Radiohead",
		Album: "OK Computer", TrackNumber: 2, DiscNumber: 1, DiscTotal: 1,
	})
	if want := "Radiohead/OK Computer/02. Paranoid Android"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRelativePathMultiDiscAndMissingTrack(t *testing.T) {
	p := ProfileByID(ProfilePoweramp)
	got := p.RelativePath(TrackFields{Title: "Intro", Album: "Live", Artist: "A", TrackNumber: 3, DiscNumber: 2, DiscTotal: 2})
	if want := "A/Live/2-03. Intro"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = p.RelativePath(TrackFields{Title: "Intro", Album: "Live", Artist: "A"})
	if want := "A/Live/Intro"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMediaServerYearOmittedWhenUnknown(t *testing.T) {
	p := ProfileByID(ProfileMediaServer)
	got := p.RelativePath(TrackFields{Title: "T", Artist: "A", Album: "B", TrackNumber: 1})
	if want := "A/B/01. T"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSanitizeComponentFAT(t *testing.T) {
	p := ProfileByID(ProfilePoweramp)
	cases := map[string]string{
		`What? Why: "Now"`: "What Why - Now",
		"trailing dots...": "trailing dots",
		"a/b\\c":           "a b c",
		"CON":              "_CON",
		"con.mp3":          "_con.mp3",
		"tab\there":        "tabhere",
	}
	for in, want := range cases {
		if got := p.SanitizeComponent(in); got != want {
			t.Errorf("SanitizeComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeComponentTruncatesOnRuneBoundary(t *testing.T) {
	p := ProfileByID(ProfilePoweramp)
	got := p.SanitizeComponent(strings.Repeat("é", 200)) // 400 bytes
	if len(got) > 255 || !strings.HasPrefix(got, "é") || strings.ContainsRune(got, '�') {
		t.Fatalf("bad truncation: %d bytes", len(got))
	}
}

func TestSanitizeRelativePathCannotEscape(t *testing.T) {
	p := ProfileByID(ProfilePoweramp)
	if got := p.SanitizeRelativePath(`..\..\Windows/./x`); got != "Windows/x" {
		t.Fatalf("got %q", got)
	}
}

func TestPrimaryArtist(t *testing.T) {
	cases := map[string]string{
		"Simon & Garfunkel":   "Simon & Garfunkel",
		"Drake feat. Rihanna": "Drake",
		"Daft Punk; Pharrell": "Daft Punk",
		"Earth, Wind & Fire":  "Earth, Wind & Fire",
	}
	for in, want := range cases {
		if got := PrimaryArtist(in); got != want {
			t.Errorf("PrimaryArtist(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnknownProfileFallsBackToPoweramp(t *testing.T) {
	if ProfileByID("nope").ID != ProfilePoweramp {
		t.Fatal("expected poweramp fallback")
	}
}

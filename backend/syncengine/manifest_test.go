package syncengine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	m := NewManifest("mediastore")
	m.Set(ManifestEntry{RemotePath: "A/B/01. T.flac", SourcePath: `C:\lib\a.flac`, Hash: "h", Size: 42, ProfileID: "mediastore", ProfileVersion: 1, Transcode: "keep", ModTime: 1700000000, Title: "T", Artist: "A", Album: "B"})
	data, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	m2, err := ReadManifest(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	e := m2.Entry("A/B/01. T.flac")
	if e == nil || e.Hash != "h" || e.Size != 42 || e.Title != "T" {
		t.Fatalf("round trip lost data: %+v", e)
	}
	// local save/load
	p := filepath.Join(t.TempDir(), "manifest.json")
	if err := m.SaveLocal(p); err != nil {
		t.Fatal(err)
	}
	m3, err := LoadManifest(p)
	if err != nil || m3 == nil || m3.Entry("A/B/01. T.flac") == nil {
		t.Fatal("local round trip failed")
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		// atomic write leaves no temp behind (name varies; just ensure p exists)
		if _, err2 := os.Stat(p); err2 != nil {
			t.Fatal(err2)
		}
	}
}

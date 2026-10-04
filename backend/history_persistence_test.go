package backend

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryWriteIsPromptAndRejectedAfterClose(t *testing.T) {
	isolateAppData(t)
	ResetHistoryStoreForTest()
	t.Cleanup(ResetHistoryStoreForTest)

	started := time.Now()
	item := HistoryItem{
		Title:     "Song",
		Artists:   "Artist",
		Album:     "Album",
		Path:      "C:/music/song.flac",
		Source:    "tidal",
		SpotifyID: "abc",
	}
	if err := AddHistoryItem(item, "Auralis"); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("history write took %s", time.Since(started))
	}

	items, err := GetHistoryItems("Auralis")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Song" || items[0].Path != item.Path {
		t.Fatalf("items = %#v", items)
	}

	if err := CloseHistoryDB(); err != nil {
		t.Fatal(err)
	}
	err = AddHistoryItem(item, "Auralis")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("write after close = %v, want closed error", err)
	}
}

func TestPersistentQueueUsesAppDirOverride(t *testing.T) {
	dir := isolateAppData(t)
	resetPersistentQueueForTest()
	t.Cleanup(resetPersistentQueueForTest)

	if err := InitPersistentQueueDB(); err != nil {
		t.Fatal(err)
	}
	path, err := getPersistentQueueDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Clean(filepath.Dir(path)), filepath.Clean(dir)) {
		t.Fatalf("queue db %s is outside %s", path, dir)
	}
	if err := ClosePersistentQueueDB(); err != nil {
		t.Fatal(err)
	}
	if err := InitPersistentQueueDB(); err == nil {
		t.Fatal("queue database reopened after close")
	}
}

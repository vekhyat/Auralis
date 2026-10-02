package ipod

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePod(t *testing.T, model, firewire string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "iPod_Control", "Device"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "ModelNumStr: " + model + "\n"
	if firewire != "" {
		body += "FirewireGuid: " + firewire + "\n"
	}
	if err := os.WriteFile(filepath.Join(root, "iPod_Control", "Device", "SysInfo"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInspectClassicAndRefusals(t *testing.T) {
	root := writePod(t, "MB147", "0x0011223344556677")
	dev, ok := inspectRoot(root, volumeInfo{Total: 80 << 30, Free: 10 << 30})
	if !ok || !dev.CanSend || dev.Mode != "stock" || dev.Checksum != "hash58" || dev.Name != "iPod classic" {
		t.Fatalf("classic = %+v ok=%v", dev, ok)
	}

	nano := writePod(t, "C027", "0x0011223344556677")
	dev, ok = inspectRoot(nano, volumeInfo{})
	if !ok || dev.CanSend || dev.Mode != "unsupported" {
		t.Fatalf("nano 5 = %+v", dev)
	}

	sql := writePod(t, "MB147", "0x0011223344556677")
	if err := os.MkdirAll(filepath.Join(sql, "iPod_Control", "iTunes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sql, "iPod_Control", "iTunes", "iTunesCDB"), []byte("cdb"), 0o644); err != nil {
		t.Fatal(err)
	}
	dev, ok = inspectRoot(sql, volumeInfo{})
	if !ok || dev.CanSend || dev.Warning != "unsupported" {
		t.Fatalf("sqlite = %+v", dev)
	}

	broken := writePod(t, "MB147", "0x0011223344556677")
	if err := os.MkdirAll(filepath.Join(broken, "iPod_Control", "iTunes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "iPod_Control", "iTunes", "iTunesDB"), []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	dev, ok = inspectRoot(broken, volumeInfo{})
	if !ok || dev.CanSend || dev.Warning != "database" {
		t.Fatalf("broken = %+v", dev)
	}

	shuffle := writePod(t, "C584", "")
	dev, ok = inspectRoot(shuffle, volumeInfo{})
	if !ok || dev.CanSend || dev.Warning != "unsupported" {
		t.Fatalf("shuffle 4 = %+v", dev)
	}
}

func TestRockboxCopiesOriginalAndLeavesStockDB(t *testing.T) {
	root := writePod(t, "C027", "0x0011223344556677")
	if err := os.MkdirAll(filepath.Join(root, "iPod_Control", "iTunes"), 0o755); err != nil {
		t.Fatal(err)
	}
	const original = "OLD-STOCK-DB"
	if err := os.WriteFile(dbPath(root), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".rockbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	dev, ok := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	if !ok || dev.Mode != "rockbox" || !dev.CanSend {
		t.Fatalf("rockbox = %+v", dev)
	}
	src := filepath.Join(t.TempDir(), "song.flac")
	payload := []byte("FLAC-BYTES-NOT-A-REAL-FILE")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := sendMount(context.Background(), dev, []string{src}, "alac", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 1 {
		t.Fatalf("sent = %+v", result)
	}
	got, err := os.ReadFile(dbPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("stock db changed to %q", got)
	}
	copied := filepath.Join(root, "Music", "Unknown", "Unknown", "song.flac")
	body, err := os.ReadFile(copied)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("copied = %q", body)
	}
}

func TestStockSendKeepsFilenameAndWritesBackup(t *testing.T) {
	root := writePod(t, "MB147", "0x0011223344556677")
	dev, ok := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	if !ok {
		t.Fatal("not an ipod")
	}
	src := filepath.Join(t.TempDir(), "Hello.mp3")
	payload := []byte("not-a-real-mp3")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := sendMount(context.Background(), dev, []string{src}, "alac", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 1 || result.Skipped != 0 {
		t.Fatalf("result = %+v", result)
	}
	db, err := openDB(dev)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.tracks) != 1 || db.tracks[0].title != "Hello" {
		t.Fatalf("track = %+v", db.tracks)
	}
	stored := mountFile(dev.Mount, db.tracks[0].path)
	body, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatal("mp3 was not copied as-is")
	}
	if _, err := os.Stat(dbPath(dev.Mount) + ".auralis.bak"); err == nil {
		t.Fatal("first create should not leave a backup")
	}

	second := filepath.Join(t.TempDir(), "Second.mp3")
	if err := os.WriteFile(second, []byte("second-mp3"), 0o644); err != nil {
		t.Fatal(err)
	}
	dev, _ = inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	if _, err := sendMount(context.Background(), dev, []string{second}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(dbPath(dev.Mount) + ".auralis.bak")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDatabase(bak)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.tracks) != 1 || parsed.tracks[0].title != "Hello" {
		t.Fatalf("backup tracks = %+v", parsed.tracks)
	}
	current, err := openDB(dev)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.tracks) != 2 {
		t.Fatalf("tracks = %d", len(current.tracks))
	}
	if pl := current.master(); pl == nil || len(pl.mhips) != 2 {
		t.Fatal("master playlist was not updated")
	}
}

func TestFailedSignRemovesCopiedFiles(t *testing.T) {
	root := writePod(t, "MB147", "0x0011223344556677")
	dev, ok := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	if !ok {
		t.Fatal("not an ipod")
	}
	first := filepath.Join(t.TempDir(), "Keep.mp3")
	if err := os.WriteFile(first, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sendMount(context.Background(), dev, []string{first}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	dev, _ = inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	before, err := os.ReadFile(dbPath(dev.Mount))
	if err != nil {
		t.Fatal(err)
	}
	dev.FirewireID = ""
	dev.Checksum = "hash58"
	second := filepath.Join(t.TempDir(), "Drop.mp3")
	if err := os.WriteFile(second, []byte("drop-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sendMount(context.Background(), dev, []string{second}, "alac", nil); err == nil {
		t.Fatal("expected the unsigned database to be refused")
	}
	after, err := os.ReadFile(dbPath(dev.Mount))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed send changed the database")
	}
	music := filepath.Join(root, "iPod_Control", "Music")
	foundDrop := false
	_ = filepath.Walk(music, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && bytes.Equal(mustRead(t, path), []byte("drop-me")) {
			foundDrop = true
		}
		return nil
	})
	if foundDrop {
		t.Fatal("copied file survived a failed database write")
	}
}

func TestRemoveKeepsFileWhenDatabaseCannotBeSigned(t *testing.T) {
	root := writePod(t, "MB147", "0x0011223344556677")
	dev, ok := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	if !ok {
		t.Fatal("not an ipod")
	}
	src := filepath.Join(t.TempDir(), "Stay.mp3")
	if err := os.WriteFile(src, []byte("stay"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sendMount(context.Background(), dev, []string{src}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	dev, _ = inspectRoot(root, volumeInfo{})
	if len(dev.Tracks) != 1 {
		t.Fatalf("tracks = %+v", dev.Tracks)
	}
	stored := mountFile(dev.Mount, dev.Tracks[0].Path)
	broken := dev
	broken.FirewireID = ""
	broken.Checksum = "hash58"
	if err := removeMount(broken, []int{dev.Tracks[0].ID}); err == nil {
		t.Fatal("expected remove to fail")
	}
	if _, err := os.Stat(stored); err != nil {
		t.Fatal("file was deleted even though the database was not updated")
	}
	again, _ := inspectRoot(root, volumeInfo{})
	if len(again.Tracks) != 1 {
		t.Fatalf("database lost the track: %+v", again.Tracks)
	}
}

func TestBrokenDatabaseIsNotReplacedUntilRebuild(t *testing.T) {
	root := writePod(t, "MB147", "0x0011223344556677")
	itunes := filepath.Join(root, "iPod_Control", "iTunes")
	if err := os.MkdirAll(itunes, 0o755); err != nil {
		t.Fatal(err)
	}
	const broken = "not-a-database"
	if err := os.WriteFile(dbPath(root), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	dev, ok := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	if !ok || dev.CanSend {
		t.Fatalf("broken device = %+v", dev)
	}
	src := filepath.Join(t.TempDir(), "No.mp3")
	if err := os.WriteFile(src, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sendMount(context.Background(), dev, []string{src}, "alac", nil); err == nil {
		t.Fatal("send replaced or ignored an unreadable database")
	}
	if got, err := os.ReadFile(dbPath(root)); err != nil || string(got) != broken {
		t.Fatalf("database after refused send = %q %v", got, err)
	}

	report, err := doctorMount(dev, "rebuild")
	if err != nil || !report.Changed {
		t.Fatalf("rebuild = %+v %v", report, err)
	}
	bak, err := os.ReadFile(dbPath(root) + ".auralis.bak")
	if err != nil || string(bak) != broken {
		t.Fatalf("backup = %q %v", bak, err)
	}
	if _, err := parseDatabase(mustRead(t, dbPath(root))); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveDeletesFileOnlyAfterDatabaseCommit(t *testing.T) {
	root := writePod(t, "MB147", "0x0011223344556677")
	dev, ok := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	if !ok {
		t.Fatal("not an ipod")
	}
	src := filepath.Join(t.TempDir(), "Gone.mp3")
	if err := os.WriteFile(src, []byte("gone"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sendMount(context.Background(), dev, []string{src}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	dev, _ = inspectRoot(root, volumeInfo{})
	stored := mountFile(dev.Mount, dev.Tracks[0].Path)
	if err := removeMount(dev, []int{dev.Tracks[0].ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stored); !os.IsNotExist(err) {
		t.Fatalf("file still present: %v", err)
	}
	again, _ := inspectRoot(root, volumeInfo{})
	if again.TrackCount != 0 {
		t.Fatalf("tracks left = %+v", again.Tracks)
	}
}

func TestMacFormattedPodIsExplained(t *testing.T) {
	root := t.TempDir()
	dev, ok := inspectRoot(root, volumeInfo{Label: "IPOD", FileSystem: ""})
	if !ok || dev.Mode != "unreadable" || dev.Warning != "no-drive" || dev.CanSend {
		t.Fatalf("mac pod = %+v ok=%v", dev, ok)
	}
}

func TestSafeInsideRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, ok := safeInside(root, ":..:..:Windows:System32:x"); ok {
		t.Fatal("path escaped the iPod")
	}
	full, ok := safeInside(root, ":iPod_Control:Music:F00:a.mp3")
	if !ok || !strings.Contains(full, "iPod_Control") {
		t.Fatalf("inside = %q ok=%v", full, ok)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

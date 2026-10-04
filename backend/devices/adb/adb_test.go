package adb

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQuoteArg(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"it's cold", "'it'\\''s cold'"},
		{"$VAR `cmd` \"quote\"", "'$VAR `cmd` \"quote\"'"},
		{"/sdcard/Music/Artist Name/01 - Track.mp3", "'/sdcard/Music/Artist Name/01 - Track.mp3'"},
		{"日本語の音楽", "'日本語の音楽'"},
	}

	for _, tt := range tests {
		got := QuoteArg(tt.input)
		if got != tt.expected {
			t.Errorf("QuoteArg(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestParseDfOutput(t *testing.T) {
	out1 := `Filesystem     1K-blocks      Used Available Use% Mounted on
/data/media     59281728  20112456  39044944  34% /storage/emulated`
	free, total, err := parseDfOutput(out1)
	if err != nil {
		t.Fatalf("parseDfOutput error: %v", err)
	}
	if free != 39044944*1024 {
		t.Errorf("free = %d; want %d", free, 39044944*1024)
	}
	if total != 59281728*1024 {
		t.Errorf("total = %d; want %d", total, 59281728*1024)
	}

	out2 := `/dev/fuse 50000000 10000000 40000000 20% /sdcard`
	free2, total2, err := parseDfOutput(out2)
	if err != nil {
		t.Fatalf("parseDfOutput error: %v", err)
	}
	if free2 != 40000000*1024 || total2 != 50000000*1024 {
		t.Errorf("free2=%d total2=%d; want free=40GB total=50GB", free2, total2)
	}
}

func TestClientWireProtocol(t *testing.T) {
	server := newFakeServer(t)
	defer server.close()

	client := NewClient(server.addr())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Version
	ver, err := client.Version(ctx)
	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}
	if ver != 41 { // 0x29 = 41
		t.Errorf("version = %d; want 41", ver)
	}

	// 2. Devices
	devices, err := client.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices failed: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	dev := devices[0]
	if dev.Serial != "emulator-5554" || dev.State != "device" || dev.Model != "Pixel 7" {
		t.Errorf("unexpected device: %+v", dev)
	}

	// 3. Shell
	out, err := client.Shell(ctx, dev.Serial, "df -k /sdcard/Music")
	if err != nil {
		t.Fatalf("Shell failed: %v", err)
	}
	if len(out) == 0 {
		t.Errorf("expected df output, got empty")
	}

	// 4. Stat
	st, err := client.Stat(ctx, dev.Serial, "/sdcard/Music/Track1.mp3")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if st.Size != 1024 {
		t.Errorf("st.Size = %d; want 1024", st.Size)
	}

	// Missing file stat
	stMissing, err := client.Stat(ctx, dev.Serial, "/sdcard/Music/NoSuchFile.mp3")
	if err != nil {
		t.Fatalf("Stat missing failed: %v", err)
	}
	if stMissing.Mode != 0 {
		t.Errorf("missing file mode = %d; want 0", stMissing.Mode)
	}

	// 5. List
	entries, err := client.List(ctx, dev.Serial, "/sdcard/Music")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "Track1.mp3" {
		t.Errorf("unexpected list entries: %+v", entries)
	}

	// 6. Send
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "upload.mp3")
	content := bytes.Repeat([]byte{0xAA, 0xBB, 0xCC}, 30000) // ~90 KB, tests chunking > 64 KiB
	if err := os.WriteFile(localFile, content, 0o644); err != nil {
		t.Fatal(err)
	}

	var progressCalls int
	err = client.Send(ctx, dev.Serial, localFile, "/sdcard/Music/uploaded.mp3", 0o644, time.Unix(1710000000, 0), func(sent int64) {
		progressCalls++
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if progressCalls < 2 {
		t.Errorf("expected at least 2 progress calls for 90KB, got %d", progressCalls)
	}

	// 7. Recv
	var recvBuf bytes.Buffer
	err = client.Recv(ctx, dev.Serial, "/sdcard/Music/uploaded.mp3", &recvBuf)
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	if !bytes.Equal(recvBuf.Bytes(), content) {
		t.Errorf("received content length %d does not match original %d", recvBuf.Len(), len(content))
	}
}

func TestTargetSyncTargetInterface(t *testing.T) {
	server := newFakeServer(t)
	defer server.close()

	client := NewClient(server.addr())
	target := NewTarget(client, "emulator-5554", "Pixel 7", "/sdcard/Music")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Info
	info := target.Info()
	if info.ID != "adb:emulator-5554" || info.Model != "Pixel 7" || info.Kind != "adb" {
		t.Errorf("unexpected target info: %+v", info)
	}
	if info.FreeBytes <= 0 {
		t.Errorf("expected free bytes > 0, got %d", info.FreeBytes)
	}

	// FreeSpace
	free, err := target.FreeSpace(ctx)
	if err != nil {
		t.Fatalf("FreeSpace failed: %v", err)
	}
	if free != 39044944*1024 {
		t.Errorf("free = %d; want %d", free, 39044944*1024)
	}

	// List
	remotes, err := target.List(ctx, "")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(remotes) != 1 || remotes[0].Path != "/sdcard/Music/Track1.mp3" {
		t.Errorf("unexpected remotes: %+v", remotes)
	}

	// StatSize
	sz, err := target.StatSize(ctx, "/sdcard/Music/Track1.mp3")
	if err != nil {
		t.Fatalf("StatSize failed: %v", err)
	}
	if sz != 1024 {
		t.Errorf("sz = %d; want 1024", sz)
	}

	// Open
	rc, err := target.Open(ctx, "/sdcard/Music/Track1.mp3")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil || len(data) != 1024 {
		t.Fatalf("Open read failed: len=%d err=%v", len(data), err)
	}

	// Put
	tmpDir := t.TempDir()
	putFile := filepath.Join(tmpDir, "new.mp3")
	_ = os.WriteFile(putFile, []byte("audio payload"), 0o644)
	var putProgress int64
	err = target.Put(ctx, putFile, "/sdcard/Music/NewAlbum/new.mp3", func(p int64) {
		putProgress = p
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if putProgress != int64(len("audio payload")) {
		t.Errorf("putProgress = %d; want %d", putProgress, len("audio payload"))
	}

	// Move
	err = target.Move(ctx, "/sdcard/Music/NewAlbum/new.mp3", "/sdcard/Music/NewAlbum/renamed.mp3")
	if err != nil {
		t.Fatalf("Move failed: %v", err)
	}

	// Delete
	err = target.Delete(ctx, "/sdcard/Music/NewAlbum/renamed.mp3")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Commit
	err = target.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Path escape rejections
	escapePaths := []string{
		"../../evil.mp3",
		"/sdcard/Other/evil.mp3",
		"/etc/hosts",
		"/sdcard/Music/../../escaped",
	}
	for _, ep := range escapePaths {
		if err := target.Put(ctx, putFile, ep, nil); err == nil {
			t.Errorf("expected Put(%q) to fail with escape error", ep)
		}
		if err := target.Delete(ctx, ep); err == nil {
			t.Errorf("expected Delete(%q) to fail with escape error", ep)
		}
		if err := target.Move(ctx, "/sdcard/Music/Track1.mp3", ep); err == nil {
			t.Errorf("expected Move to %q to fail with escape error", ep)
		}
		if _, err := target.StatSize(ctx, ep); err == nil {
			t.Errorf("expected StatSize(%q) to fail with escape error", ep)
		}
	}

	// Cannot delete or move target root itself
	if err := target.Delete(ctx, "/sdcard/Music"); err == nil {
		t.Errorf("expected Delete(root) to fail")
	}
	if err := target.Move(ctx, "/sdcard/Music", "/sdcard/Music2"); err == nil {
		t.Errorf("expected Move(root) to fail")
	}
}

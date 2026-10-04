package ipod

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vekhyat/Auralis/backend"
)

func TestDeviceFormatAndCodecSupport(t *testing.T) {
	alac := audioMeta{codec: "alac", sampleRate: 44100, bitDepth: 16}
	aiff := audioMeta{sampleRate: 44100, bitDepth: 16}
	if deviceFormat("SHUFFLE_2", "alac") != "aac" || deviceFormat("SHUFFLE_1", "") != "aac" {
		t.Fatal("shuffle must be forced to AAC when ALAC is requested")
	}
	if deviceFormat("CLASSIC_1", "alac") != "alac" || deviceFormat("CLASSIC_1", "aac") != "aac" {
		t.Fatal("classic keeps the requested format")
	}
	if !needsTranscode("SHUFFLE_2", ".m4a", alac) || !needsTranscode("SHUFFLE_1", ".m4a", alac) {
		t.Fatal("existing 16-bit 44.1 kHz ALAC must be converted for shuffle 1 and 2")
	}
	if needsTranscode("CLASSIC_1", ".m4a", alac) {
		t.Fatal("classic can keep 16-bit 44.1 kHz ALAC")
	}
	if !needsTranscode("SHUFFLE_1", ".aiff", aiff) {
		t.Fatal("shuffle 1 must not keep AIFF")
	}
	if needsTranscode("SHUFFLE_2", ".aiff", aiff) || needsTranscode("CLASSIC_1", ".aiff", aiff) {
		t.Fatal("shuffle 2 and classic can keep 16-bit 44.1 kHz AIFF")
	}
	if !needsTranscode("SHUFFLE_2", ".flac", audioMeta{sampleRate: 44100}) {
		t.Fatal("flac still needs conversion")
	}
}

func TestDuplicatePrefersSourceHash(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	other := strings.Repeat("cd", 32)
	db := newDatabase(ChecksumNone, nil)
	db.tracks = append(db.tracks, &track{
		title: "Song", artist: "Artist", size: 10, sourceHash: hash,
	})
	meta := audioMeta{title: "Song", artist: "Artist", size: 999}
	if !duplicate(db, meta, hash) {
		t.Fatal("matching source hash was not treated as a duplicate")
	}
	if !duplicate(db, meta, SourceHashPrefix+hash) {
		t.Fatal("prefixed source hash was not treated as a duplicate")
	}
	if duplicate(db, meta, other) {
		t.Fatal("a different source hash matched a stored track")
	}
	db.tracks[0].sourceHash = SourceHashPrefix + strings.ToUpper(hash)
	if !duplicate(db, meta, hash) {
		t.Fatal("stored comment prefix was not accepted as the source hash")
	}

	db.tracks[0].sourceHash = ""
	meta.size = 10
	if !duplicate(db, meta, "") {
		t.Fatal("legacy title, artist, and size match failed")
	}
	meta.size = 11
	if duplicate(db, meta, "") {
		t.Fatal("legacy match ignored size")
	}
	meta.size = 10
	meta.title = ""
	if duplicate(db, meta, "") {
		t.Fatal("legacy match accepted an empty title")
	}
}

func TestShuffleSDFailureRestoresBothDatabases(t *testing.T) {
	root := writePod(t, "9724", "")
	dev, ok := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	if !ok || !usesShuffleSD(dev.Generation) || !dev.CanSend {
		t.Fatalf("fixture: %+v", dev)
	}
	src := filepath.Join(t.TempDir(), "keep.mp3")
	if err := os.WriteFile(src, []byte("review-fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sendMount(context.Background(), dev, []string{src}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	dev, _ = inspectRoot(root, volumeInfo{})
	stored := mountFile(root, dev.Tracks[0].Path)
	beforeDB := mustRead(t, dbPath(root))
	beforeSD := mustRead(t, sdPath(root))
	if err := os.Mkdir(sdPath(root)+".auralis.tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	err := removeMount(dev, []int{dev.Tracks[0].ID})
	var partial *afterCommitError
	if err == nil || errors.As(err, &partial) {
		t.Fatalf("verified rollback returned partial-commit error: %v", err)
	}
	if !bytes.Equal(beforeDB, mustRead(t, dbPath(root))) {
		t.Fatal("iTunesDB changed after the shuffle database write failed")
	}
	if !bytes.Equal(beforeSD, mustRead(t, sdPath(root))) {
		t.Fatal("iTunesSD changed even though its temporary file could not be written")
	}
	if _, statErr := os.Stat(stored); statErr != nil {
		t.Fatalf("failed shuffle removal deleted audio still referenced by both databases: %v", statErr)
	}
	if err := os.Remove(sdPath(root) + ".auralis.tmp"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := removeMount(dev, []int{dev.Tracks[0].ID}); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(stored); !os.IsNotExist(statErr) {
		t.Fatalf("audio remained after both databases committed the removal: %v", statErr)
	}
}

func TestShuffleFirstSendRollsBackDatabaseAndAudio(t *testing.T) {
	root := writePod(t, "A546", "")
	dev, ok := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	if !ok || dev.Generation != "SHUFFLE_2" {
		t.Fatalf("fixture: %+v", dev)
	}
	if err := os.MkdirAll(filepath.Dir(sdPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sdPath(root)+".auralis.tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "new.mp3")
	if err := os.WriteFile(src, []byte("new-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := sendMount(context.Background(), dev, []string{src}, "alac", nil)
	var partial *afterCommitError
	if err == nil || errors.As(err, &partial) {
		t.Fatalf("first send rollback = %v", err)
	}
	if fileExists(dbPath(root)) {
		t.Fatal("iTunesDB remained after the shuffle database was not created")
	}
	if fileExists(sdPath(root)) {
		t.Fatal("iTunesSD was created despite the blocked temporary file")
	}
	if files := listMusicFiles(root); len(files) != 0 {
		t.Fatalf("new audio remained after rollback: %v", files)
	}
}

func TestShuffleSendRollsBackNewAudioWhenSDFails(t *testing.T) {
	root := writePod(t, "9724", "")
	dev, _ := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	first := filepath.Join(t.TempDir(), "one.mp3")
	if err := os.WriteFile(first, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sendMount(context.Background(), dev, []string{first}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	beforeDB := mustRead(t, dbPath(root))
	beforeSD := mustRead(t, sdPath(root))
	if err := os.Mkdir(sdPath(root)+".auralis.tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "two.mp3")
	if err := os.WriteFile(second, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	dev, _ = inspectRoot(root, volumeInfo{})
	_, err := sendMount(context.Background(), dev, []string{second}, "alac", nil)
	var partial *afterCommitError
	if err == nil || errors.As(err, &partial) {
		t.Fatalf("send rollback = %v", err)
	}
	if !bytes.Equal(beforeDB, mustRead(t, dbPath(root))) || !bytes.Equal(beforeSD, mustRead(t, sdPath(root))) {
		t.Fatal("failed second send changed a playback database")
	}
	bodies := musicBodies(t, root)
	if _, ok := bodies["one"]; !ok || len(bodies) != 1 {
		t.Fatalf("audio after rollback = %v", bodies)
	}
}

func TestRollbackFailureKeepsReferencedAudio(t *testing.T) {
	root := writePod(t, "9724", "")
	dev, _ := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	src := filepath.Join(t.TempDir(), "keep.mp3")
	if err := os.WriteFile(src, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sendMount(context.Background(), dev, []string{src}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	dev, _ = inspectRoot(root, volumeInfo{})
	stored := mountFile(root, dev.Tracks[0].Path)
	beforeDB := mustRead(t, dbPath(root))
	beforeSD := mustRead(t, sdPath(root))
	if err := os.Mkdir(sdPath(root)+".auralis.tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	prev := restoreRename
	restoreRename = func(oldpath, newpath string) error {
		if strings.HasSuffix(oldpath, ".auralis.rollback") {
			return errors.New("injected rollback failure")
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { restoreRename = prev })

	err := removeMount(dev, []int{dev.Tracks[0].ID})
	var partial *afterCommitError
	if !errors.As(err, &partial) {
		t.Fatalf("rollback failure = %v", err)
	}
	if partial.playbackCommitted {
		t.Fatal("uncertain rollback was treated as a finished playback commit")
	}
	if !bytes.Equal(beforeSD, mustRead(t, sdPath(root))) {
		t.Fatal("failed rollback changed iTunesSD")
	}
	if bytes.Equal(mustRead(t, dbPath(root)), beforeDB) {
		t.Fatal("injected failure still restored the live iTunesDB")
	}
	bak := mustRead(t, dbPath(root)+".auralis.bak")
	if !bytes.Equal(bak, beforeDB) {
		t.Fatal("rollback failure destroyed the original iTunesDB backup")
	}
	if _, statErr := os.Stat(stored); statErr != nil {
		t.Fatalf("rollback failure deleted audio still named by iTunesSD: %v", statErr)
	}
}

func TestSendKeepsNewAudioWhenRollbackFails(t *testing.T) {
	root := writePod(t, "9724", "")
	dev, _ := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	first := filepath.Join(t.TempDir(), "one.mp3")
	if err := os.WriteFile(first, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sendMount(context.Background(), dev, []string{first}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	beforeSD := mustRead(t, sdPath(root))
	beforeDB := mustRead(t, dbPath(root))
	if err := os.Mkdir(sdPath(root)+".auralis.tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	prev := restoreRename
	restoreRename = func(oldpath, newpath string) error {
		if strings.HasSuffix(oldpath, ".auralis.rollback") {
			return errors.New("injected rollback failure")
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { restoreRename = prev })
	second := filepath.Join(t.TempDir(), "two.mp3")
	if err := os.WriteFile(second, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	dev, _ = inspectRoot(root, volumeInfo{})
	_, err := sendMount(context.Background(), dev, []string{second}, "alac", nil)
	var partial *afterCommitError
	if !errors.As(err, &partial) {
		t.Fatalf("send rollback failure = %v", err)
	}
	if !bytes.Equal(beforeSD, mustRead(t, sdPath(root))) {
		t.Fatal("iTunesSD changed")
	}
	if !bytes.Equal(mustRead(t, dbPath(root)+".auralis.bak"), beforeDB) {
		t.Fatal("original iTunesDB backup was not preserved")
	}
	bodies := musicBodies(t, root)
	if _, ok := bodies["one"]; !ok {
		t.Fatal("original audio was deleted")
	}
	if _, ok := bodies["two"]; !ok {
		t.Fatal("new audio was deleted even though the new database may reference it")
	}
}

func TestCancelDuringDatabasePhaseDoesNotPublish(t *testing.T) {
	root := writePod(t, "MB147", "0x0011223344556677")
	dev, _ := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	src := filepath.Join(t.TempDir(), "stop.mp3")
	if err := os.WriteFile(src, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := sendMount(ctx, dev, []string{src}, "alac", func(p Progress) {
		if p.Phase == "database" {
			cancel()
		}
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if fileExists(dbPath(root)) {
		t.Fatal("database phase cancellation published iTunesDB")
	}
	if files := listMusicFiles(root); len(files) != 0 {
		t.Fatalf("cancelled send left audio: %v", files)
	}
}

func TestCommitBytesRefusesInaccessibleOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iTunesDB")
	original := []byte("original-db")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	prev := statFile
	statFile = func(name string) (os.FileInfo, error) {
		if name == path {
			return nil, os.ErrPermission
		}
		return os.Stat(name)
	}
	t.Cleanup(func() { statFile = prev })
	state, err := commitBytes(path, []byte("replacement"), nil)
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("commitBytes err=%v", err)
	}
	if state.published || state.uncertain {
		t.Fatalf("inaccessible original was treated as published: %+v", state)
	}
	if fileExists(path + ".auralis.tmp") {
		t.Fatal("temporary database was written before the original could be read")
	}
	if !bytes.Equal(mustRead(t, path), original) {
		t.Fatal("inaccessible original was replaced")
	}
}

func TestCancelLastFilePreventsCommitAndRemovesCopies(t *testing.T) {
	root := writePod(t, "MB147", "0x0011223344556677")
	dev, _ := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	dir := t.TempDir()
	first := filepath.Join(dir, "a.mp3")
	second := filepath.Join(dir, "b.mp3")
	if err := os.WriteFile(first, []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("bbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := sendMount(ctx, dev, []string{first, second}, "alac", func(p Progress) {
		if p.Phase == "copy" && p.Name == "b.mp3" {
			cancel()
		}
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if fileExists(dbPath(root)) {
		t.Fatal("cancelled send committed iTunesDB")
	}
	if files := listMusicFiles(root); len(files) != 0 {
		t.Fatalf("cancelled send left audio: %v", files)
	}
}

func TestCopyAndTranscodeRemoveIncompleteOutput(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	if err := os.WriteFile(src, bytes.Repeat([]byte("x"), 64*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prev := copyYield
	copyYield = func() { cancel() }
	t.Cleanup(func() { copyYield = prev })
	err := copyFileCtx(ctx, src, dst)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copy err = %v", err)
	}
	if fileExists(dst) {
		t.Fatal("cancelled copy left the destination")
	}

	sum, err := hashSource(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(mustRead(t, src))
	if sum != hex.EncodeToString(want[:]) {
		t.Fatalf("hash = %s", sum)
	}
	hashCtx, hashCancel := context.WithCancel(context.Background())
	hashCancel()
	if _, err := hashSource(hashCtx, src); !errors.Is(err, context.Canceled) {
		t.Fatalf("hash cancel = %v", err)
	}

}

func TestCancelledTranscodeRemovesOutput(t *testing.T) {
	if _, err := backend.GetFFmpegPath(); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	out := filepath.Join(dir, "out.m4a")
	if err := os.WriteFile(src, []byte("not-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prevWait := beforeFFmpegWait
	beforeFFmpegWait = func() { cancel() }
	t.Cleanup(func() { beforeFFmpegWait = prevWait })
	err := transcodeFileCtx(ctx, src, out, "aac")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("transcode err = %v", err)
	}
	if fileExists(out) {
		t.Fatal("cancelled transcode left the destination")
	}
}

func TestShuffleRecoverySkipsUnsupportedAudio(t *testing.T) {
	root := writePod(t, "9724", "")
	music := filepath.Join(root, "iPod_Control", "Music", "F00")
	if err := os.MkdirAll(music, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(music, "keep.mp3"), []byte("mp3"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(music, "skip.aiff"), []byte("aiff"), 0o644); err != nil {
		t.Fatal(err)
	}
	dev, ok := inspectRoot(root, volumeInfo{})
	if !ok || dev.Generation != "SHUFFLE_1" {
		t.Fatalf("fixture: %+v", dev)
	}
	report, err := doctorMount(dev, "rebuild")
	if err != nil || !report.Changed {
		t.Fatalf("rebuild = %+v %v", report, err)
	}
	db, err := openDB(dev)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.tracks) != 1 || !strings.HasSuffix(strings.ToLower(db.tracks[0].path), "keep.mp3") {
		t.Fatalf("tracks = %+v", db.tracks)
	}

	second := writePod(t, "A546", "")
	music = filepath.Join(second, "iPod_Control", "Music", "F00")
	if err := os.MkdirAll(music, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(music, "keep.aiff"), []byte("aiff"), 0o644); err != nil {
		t.Fatal(err)
	}
	dev, _ = inspectRoot(second, volumeInfo{})
	if _, err := doctorMount(dev, "rebuild"); err != nil {
		t.Fatal(err)
	}
	db, err = openDB(dev)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.tracks) != 1 || !strings.Contains(strings.ToLower(db.tracks[0].path), "keep.aiff") {
		t.Fatalf("shuffle 2 dropped AIFF: %+v", db.tracks)
	}
}

func TestSourceHashSkipsRepeatAfterReopen(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		if _, pathErr := backend.GetFFmpegPath(); pathErr != nil {
			t.Skip("FFmpeg unavailable")
		}
		ffmpeg, err = backend.GetFFmpegPath()
		if err != nil {
			t.Skip(err)
		}
	}
	root := writePod(t, "MB147", "0x0011223344556677")
	dev, _ := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	src := filepath.Join(t.TempDir(), "review.flac")
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=1", "-c:a", "flac", "-y", src)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, output)
	}
	if _, err := sendMount(context.Background(), dev, []string{src}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	dev, _ = inspectRoot(root, volumeInfo{})
	db, err := openDB(dev)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.tracks) != 1 {
		t.Fatalf("tracks = %d", len(db.tracks))
	}
	got := normalizeSourceHash(db.tracks[0].sourceHash)
	sum, err := hashSource(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if got != sum {
		t.Fatalf("reopened sourceHash = %q, want %s", db.tracks[0].sourceHash, sum)
	}
	result, err := sendMount(context.Background(), dev, []string{src}, "alac", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 0 || result.Skipped != 1 {
		t.Fatalf("resend = %+v", result)
	}
}

func TestShuffle2TranscodesToAAC(t *testing.T) {
	root := writePod(t, "A546", "")
	dev, ok := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	if !ok || dev.Generation != "SHUFFLE_2" || !dev.CanSend {
		t.Fatalf("fixture: %+v", dev)
	}
	src := sineFLAC(t, "44100")
	if _, err := sendMount(context.Background(), dev, []string{src}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	db, err := openDB(dev)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.tracks) != 1 {
		t.Fatalf("tracks = %d", len(db.tracks))
	}
	tr := db.tracks[0]
	if strings.Contains(strings.ToLower(tr.kind), "lossless") {
		t.Fatalf("kind = %s", tr.kind)
	}
	dest := mountFile(root, tr.path)
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	meta := readMeta(dest, info.Size())
	if strings.EqualFold(meta.codec, "alac") {
		t.Fatalf("shuffle 2 stored ALAC: codec=%s kind=%s", meta.codec, tr.kind)
	}
}

func TestTranscodeStoresOutputSampleRate(t *testing.T) {
	root := writePod(t, "MB147", "0x0011223344556677")
	dev, _ := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	src := sineFLAC(t, "96000")
	if _, err := sendMount(context.Background(), dev, []string{src}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	db, err := openDB(dev)
	if err != nil {
		t.Fatal(err)
	}
	tr := db.tracks[0]
	dest := mountFile(root, tr.path)
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	meta := readMeta(dest, info.Size())
	if meta.sampleRate != 44100 {
		t.Fatalf("output rate = %d", meta.sampleRate)
	}
	if len(tr.raw) < 0x40 {
		t.Fatal("track raw header is too short")
	}
	stored := u32(tr.raw[0x3c:]) >> 16
	if stored != meta.sampleRate {
		t.Fatalf("encoded rate = %d, file = %d", stored, meta.sampleRate)
	}
	if !strings.EqualFold(meta.codec, "alac") {
		t.Fatalf("classic format changed to %s", meta.codec)
	}
}

func TestShuffleTranscodesExistingALAC(t *testing.T) {
	ffmpeg := ffmpegPath(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "already.m4a")
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=1", "-sample_fmt", "s16p", "-c:a", "alac", "-f", "ipod", "-y", src)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("alac fixture: %v %s", err, output)
	}
	probed := readMeta(src, mustSize(t, src))
	if !strings.EqualFold(probed.codec, "alac") || probed.sampleRate != 44100 {
		t.Fatalf("fixture codec=%s rate=%d", probed.codec, probed.sampleRate)
	}
	root := writePod(t, "A546", "")
	dev, _ := inspectRoot(root, volumeInfo{Total: 1 << 30, Free: 1 << 30})
	if _, err := sendMount(context.Background(), dev, []string{src}, "alac", nil); err != nil {
		t.Fatal(err)
	}
	db, err := openDB(dev)
	if err != nil {
		t.Fatal(err)
	}
	dest := mountFile(root, db.tracks[0].path)
	meta := readMeta(dest, mustSize(t, dest))
	if strings.EqualFold(meta.codec, "alac") {
		t.Fatal("existing ALAC was copied onto shuffle 2")
	}
}

func sineFLAC(t *testing.T, rate string) string {
	t.Helper()
	ffmpeg := ffmpegPath(t)
	src := filepath.Join(t.TempDir(), "review.flac")
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate="+rate+":duration=1", "-c:a", "flac", "-sample_fmt", "s32", "-metadata", "title=Review Song", "-metadata", "artist=Review Artist", "-y", src)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, output)
	}
	return src
}

func ffmpegPath(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path
	}
	path, err := backend.GetFFmpegPath()
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	return path
}

func musicBodies(t *testing.T, root string) map[string]struct{} {
	t.Helper()
	out := map[string]struct{}{}
	for _, path := range listMusicFiles(root) {
		out[string(mustRead(t, path))] = struct{}{}
	}
	return out
}

func mustSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

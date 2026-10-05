package adb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vekhyat/Auralis/backend/syncengine"
)

const DefaultMusicRoot = "/sdcard/Music"

// Target implements syncengine.SyncTarget over ADB for an Android device.
type Target struct {
	Serial      string
	Model       string
	Root        string
	client      *Client
	createdDirs map[string]bool
	mu          sync.Mutex
}

// NewTarget creates an ADB sync target for a device serial.
func NewTarget(client *Client, serial, model, root string) *Target {
	if client == nil {
		client = NewClient("")
	}
	if root == "" {
		root = DefaultMusicRoot
	}
	return &Target{
		Serial:      serial,
		Model:       model,
		Root:        path.Clean("/" + strings.Trim(root, "/")),
		client:      client,
		createdDirs: make(map[string]bool),
	}
}

var (
	_ syncengine.SyncTarget = (*Target)(nil)
	_ syncengine.StatSize   = (*Target)(nil)
	_ syncengine.Open       = (*Target)(nil)
)

// resolve validates that targetPath does not escape t.Root.
func (t *Target) resolve(targetPath string) (string, error) {
	if t.Root == "" {
		return "", fmt.Errorf("target root is empty")
	}
	cleanRoot := path.Clean("/" + strings.Trim(t.Root, "/"))
	if cleanRoot == "/" {
		// Root is the whole device: every absolute path is inside it.
		if targetPath == "" || targetPath == "/" {
			return "/", nil
		}
		if strings.HasPrefix(targetPath, "/") {
			return path.Clean(targetPath), nil
		}
		return path.Clean("/" + targetPath), nil
	}
	var cleanTarget string
	if strings.HasPrefix(targetPath, "/") {
		cleanTarget = path.Clean(targetPath)
	} else {
		cleanTarget = path.Clean(cleanRoot + "/" + targetPath)
	}
	if cleanTarget != cleanRoot && !strings.HasPrefix(cleanTarget, cleanRoot+"/") {
		return "", fmt.Errorf("path %q escapes target root %q", targetPath, t.Root)
	}
	return cleanTarget, nil
}

// Info returns device details.
func (t *Target) Info() syncengine.DeviceInfo {
	name := t.Model
	if name == "" {
		name = "Android (" + t.Serial + ")"
	}
	info := syncengine.DeviceInfo{
		ID:        "adb:" + t.Serial,
		Name:      name,
		Kind:      "adb",
		Model:     t.Model,
		Root:      t.Root,
		Connected: true,
	}
	ctxInfo, cancel := ctxWithInfoTimeout()
	defer cancel()
	if free, err := t.FreeSpace(ctxInfo); err == nil {
		info.FreeBytes = free
	}
	return info
}

// infoTimeout bounds the FreeSpace probe that Info() performs while
// describing a target. Info is called on every sync-workspace open, so it
// must never block on a stalled device.
const infoCheckTimeout = 800 * time.Millisecond

func ctxWithInfoTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), infoCheckTimeout)
}

// List walks root and returns every file below it using sync LIST.
func (t *Target) List(ctx context.Context, root string) ([]syncengine.RemoteEntry, error) {
	walkRoot := t.Root
	if root != "" {
		resolved, err := t.resolve(root)
		if err != nil {
			return nil, err
		}
		walkRoot = resolved
	}

	var out []syncengine.RemoteEntry
	queue := []string{walkRoot}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		curr := queue[0]
		queue = queue[1:]

		entries, err := t.client.List(ctx, t.Serial, curr)
		if err != nil {
			// A directory that does not exist yet simply has no entries,
			// but connection/authorization/transport errors must surface:
			// swallowing them would make sync diff against an empty
			// device and queue destructive "delete everything" operations.
			if isMissingPathError(err) {
				continue
			}
			return nil, fmt.Errorf("adb list %s: %w", curr, err)
		}

		for _, e := range entries {
			if e.Name == "." || e.Name == ".." {
				continue
			}
			if e.IsSymlink {
				// Never follow phone-side links: a link pointing outside
				// the music root would make the engine read or write
				// files it does not manage.
				continue
			}
			full := path.Join(curr, e.Name)
			if e.IsDir {
				queue = append(queue, full)
			} else {
				out = append(out, syncengine.RemoteEntry{
					Path:  full,
					Size:  e.Size,
					Mtime: e.Mtime,
					IsDir: false,
				})
			}
		}
	}
	return out, nil
}

// Put sends localPath to remotePath on the Android device.
func (t *Target) Put(ctx context.Context, localPath, remotePath string, progress func(int64)) error {
	resolved, err := t.resolve(remotePath)
	if err != nil {
		return err
	}
	if resolved == path.Clean(t.Root) {
		return fmt.Errorf("cannot put to root directory %q", t.Root)
	}

	dir := path.Dir(resolved)
	t.mu.Lock()
	alreadyCreated := t.createdDirs[dir]
	t.mu.Unlock()

	if !alreadyCreated {
		if _, err := t.run(ctx, "mkdir -p "+QuoteArg(dir)); err != nil {
			return fmt.Errorf("adb mkdir: %w", err)
		}
		t.mu.Lock()
		t.createdDirs[dir] = true
		t.mu.Unlock()
	}

	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	// Refuse to stage through a remote parent that is a symlink (where the
	// server reports link modes): the copy would land outside the root.
	if err := t.checkRemoteHops(ctx, path.Dir(resolved)); err != nil {
		return err
	}
	// Send to a unique staging name and rename once complete, so an
	// interrupted transfer never leaves a truncated track or damages the
	// previous copy. A fixed suffix could collide with another transfer to
	// the same path or be planted as a hijacked name.
	staging := remoteStagingPath(resolved)
	if err := t.client.Send(ctx, t.Serial, localPath, staging, 0o644, info.ModTime(), progress); err != nil {
		_, _ = t.run(context.Background(), "rm -f "+QuoteArg(staging))
		return err
	}
	if _, err := t.run(ctx, "mv -f "+QuoteArg(staging)+" "+QuoteArg(resolved)); err != nil {
		return fmt.Errorf("adb rename: %w", err)
	}
	return nil
}

// remoteStagingPath derives a unique per-transfer staging path so two
// concurrent Puts to the same destination never share a temp file.
func remoteStagingPath(resolved string) string {
	return fmt.Sprintf("%s.auralis-%d.part", resolved, time.Now().UnixNano())
}

// checkRemoteHops walks the parent chain of dir toward the root and fails
// when the server reports any hop as a symlink. adbd often follows links,
// so this is a best-effort guard, not a guarantee.
func (t *Target) checkRemoteHops(ctx context.Context, dir string) error {
	root := path.Clean(t.Root)
	for dir != root && dir != "/" && dir != "." && dir != "" {
		st, err := t.client.Stat(ctx, t.Serial, dir)
		if err == nil && st != nil && st.IsSymlink {
			return fmt.Errorf("remote parent %q is a symlink", dir)
		}
		dir = path.Dir(dir)
	}
	return nil
}

// isMissingPathError reports whether err is a "directory not found" failure
// from adbd, as opposed to a transport, permission, or authorization error.
func isMissingPathError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "not exist") ||
		strings.Contains(msg, "enoent") ||
		strings.Contains(msg, "not found")
}

// rcMarker carries the exit status back. The adb "shell:" service does not
// report exit codes, so without it a failed mv or rm would look successful.
const rcMarker = "__AURALIS_RC="

// run executes a shell command and returns an error when it exits non-zero.
func (t *Target) run(ctx context.Context, cmd string) (string, error) {
	out, err := t.client.Shell(ctx, t.Serial, "("+cmd+") 2>&1; echo "+rcMarker+"$?")
	if err != nil {
		return "", err
	}
	text := strings.TrimRight(string(out), "\r\n")
	idx := strings.LastIndex(text, rcMarker)
	if idx < 0 {
		return "", fmt.Errorf("adb shell returned no exit status: %q", text)
	}
	body := strings.TrimSpace(text[:idx])
	if rc := strings.TrimSpace(text[idx+len(rcMarker):]); rc != "0" {
		return body, fmt.Errorf("exit status %s: %s", rc, body)
	}
	return body, nil
}

// Move renames from to to using adb shell mv.
func (t *Target) Move(ctx context.Context, from, to string) error {
	fromPath, err := t.resolve(from)
	if err != nil {
		return err
	}
	toPath, err := t.resolve(to)
	if err != nil {
		return err
	}
	if fromPath == path.Clean(t.Root) || toPath == path.Clean(t.Root) {
		return fmt.Errorf("cannot move target root %q", t.Root)
	}

	to = QuoteArg(toPath)
	cmd := fmt.Sprintf("if [ -e %s ]; then echo 'destination exists'; exit 17; fi; mkdir -p %s && mv %s %s",
		to, QuoteArg(path.Dir(toPath)), QuoteArg(fromPath), to)
	if _, err := t.run(ctx, cmd); err != nil {
		return fmt.Errorf("adb move: %w", err)
	}
	return nil
}

// Delete removes remotePath using adb shell rm -f.
func (t *Target) Delete(ctx context.Context, remotePath string) error {
	resolved, err := t.resolve(remotePath)
	if err != nil {
		return err
	}
	if resolved == path.Clean(t.Root) {
		return fmt.Errorf("cannot delete root directory %q", t.Root)
	}

	if _, err := t.run(ctx, "rm -f "+QuoteArg(resolved)); err != nil {
		return fmt.Errorf("adb delete: %w", err)
	}
	return nil
}

// FreeSpace queries free bytes using df -k.
func (t *Target) FreeSpace(ctx context.Context) (int64, error) {
	cmd := fmt.Sprintf("df -k %s", QuoteArg(t.Root))
	out, err := t.client.Shell(ctx, t.Serial, cmd)
	if err != nil {
		// Fallback to df without -k
		cmd = fmt.Sprintf("df %s", QuoteArg(t.Root))
		out, err = t.client.Shell(ctx, t.Serial, cmd)
		if err != nil {
			return 0, err
		}
	}
	free, _, err := parseDfOutput(string(out))
	return free, err
}

// parseDfOutput parses the output of Android's df command.
func parseDfOutput(out string) (free, total int64, err error) {
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToLower(line), "filesystem") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 4 {
			tBlocks, errT := strconv.ParseInt(fields[1], 10, 64)
			var fBlocks int64
			var errF error
			if len(fields) >= 5 && strings.HasSuffix(fields[4], "%") {
				fBlocks, errF = strconv.ParseInt(fields[3], 10, 64)
			} else {
				fBlocks, errF = strconv.ParseInt(fields[3], 10, 64)
				if errF != nil && len(fields) >= 3 {
					fBlocks, errF = strconv.ParseInt(fields[2], 10, 64)
				}
			}
			if errT == nil && errF == nil && fBlocks >= 0 {
				return fBlocks * 1024, tBlocks * 1024, nil
			}
		}
	}
	return 0, 0, fmt.Errorf("unable to parse df output: %q", out)
}

// Commit asks Android to index the music folder. Android 11+ indexes files
// written to shared storage on its own; the broadcast helps older versions.
// Players with their own scanner (Poweramp) pick changes up on next launch.
func (t *Target) Commit(ctx context.Context) error {
	cmd := "am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d " + QuoteArg("file://"+t.Root)
	_, _ = t.client.Shell(ctx, t.Serial, cmd)
	return nil
}

// StatSize returns file size via sync STAT.
func (t *Target) StatSize(ctx context.Context, remotePath string) (int64, error) {
	resolved, err := t.resolve(remotePath)
	if err != nil {
		return 0, err
	}
	st, err := t.client.Stat(ctx, t.Serial, resolved)
	if err != nil {
		return 0, err
	}
	if st == nil || st.Mode == 0 {
		return 0, fmt.Errorf("stat %s: %w", resolved, os.ErrNotExist)
	}
	return st.Size, nil
}

// Open streams remotePath via sync RECV.
func (t *Target) Open(ctx context.Context, remotePath string) (io.ReadCloser, error) {
	resolved, err := t.resolve(remotePath)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.client.Recv(ctx, t.Serial, resolved, &buf); err != nil {
		return nil, err
	}
	return io.NopCloser(&buf), nil
}

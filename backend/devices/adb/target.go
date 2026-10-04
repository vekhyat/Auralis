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
	if free, err := t.FreeSpace(context.Background()); err == nil {
		info.FreeBytes = free
	}
	return info
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
			// Directory might not exist yet; treat as empty
			continue
		}

		for _, e := range entries {
			if e.Name == "." || e.Name == ".." {
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
		cmd := fmt.Sprintf("mkdir -p %s", QuoteArg(dir))
		if _, err := t.client.Shell(ctx, t.Serial, cmd); err != nil {
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
	return t.client.Send(ctx, t.Serial, localPath, resolved, 0o644, info.ModTime(), progress)
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

	dir := path.Dir(toPath)
	cmd := fmt.Sprintf("mkdir -p %s && mv %s %s", QuoteArg(dir), QuoteArg(fromPath), QuoteArg(toPath))
	out, err := t.client.Shell(ctx, t.Serial, cmd)
	if err != nil {
		return fmt.Errorf("adb move: %w: %s", err, string(out))
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

	cmd := fmt.Sprintf("rm -f %s", QuoteArg(resolved))
	out, err := t.client.Shell(ctx, t.Serial, cmd)
	if err != nil {
		return fmt.Errorf("adb delete: %w: %s", err, string(out))
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

// Commit triggers media scanner broadcast to refresh Android library.
func (t *Target) Commit(ctx context.Context) error {
	// 1. Android standard media scanner broadcast
	cmd := fmt.Sprintf("am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d %s", QuoteArg("file://"+t.Root))
	_, _ = t.client.Shell(ctx, t.Serial, cmd)

	// 2. Poweramp rescan intent (best-effort)
	_, _ = t.client.Shell(ctx, t.Serial, "am broadcast -a com.maxmpz.audioplayer.API_COMMAND --ei cmd 100")
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
		return 0, os.ErrNotExist
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

package syncengine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"time"

	"github.com/vekhyat/Auralis/backend"
)

// ManifestVersion is the current on-disk manifest schema version.
const ManifestVersion = 1

// ManifestPathRel is where the manifest lives, relative to the music root.
const ManifestPathRel = ".auralis/manifest.json"

// TrashDirRel holds files removed by a sync, relative to the music root.
const TrashDirRel = ".auralis/trash"

// ManifestEntry records one managed file on the device.
type ManifestEntry struct {
	// RemotePath is the file's path relative to the music root.
	RemotePath string `json:"remote_path"`
	// SourcePath is the library file it was produced from.
	SourcePath string `json:"source_path"`
	// Hash is the source content hash (see HashSource).
	Hash string `json:"hash"`
	// Size is the delivered (post-transcode) file size in bytes.
	Size int64 `json:"size"`
	// ProfileID and ProfileVersion identify the layout used.
	ProfileID      string `json:"profile_id"`
	ProfileVersion int    `json:"profile_version"`
	// Transcode records the format policy applied ("keep", "aac", "opus", "mp3").
	Transcode string `json:"transcode"`
	// ModTime is the source file's mtime (unix seconds) at sync time.
	ModTime int64 `json:"mod_time"`
	// Metadata carried for playlist regeneration without the PC present.
	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	DurationSec int    `json:"duration_sec,omitempty"`
}

// Manifest is the device-side record of what Auralis manages.
type Manifest struct {
	Version   int                      `json:"version"`
	ProfileID string                   `json:"profile_id"`
	UpdatedAt int64                    `json:"updated_at"`
	Entries   map[string]ManifestEntry `json:"entries"` // keyed by RemotePath
}

func NewManifest(profileID string) *Manifest {
	return &Manifest{Version: ManifestVersion, ProfileID: profileID, Entries: map[string]ManifestEntry{}}
}

// ReadManifest parses a manifest from r.
func ReadManifest(r io.Reader) (*Manifest, error) {
	var m Manifest
	data, err := io.ReadAll(io.LimitReader(r, 64<<20))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Entries == nil {
		m.Entries = map[string]ManifestEntry{}
	}
	return &m, nil
}

// LoadManifest reads a local manifest file; a missing file yields an empty manifest.
func LoadManifest(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	return ReadManifest(f)
}

// Marshal serialises the manifest canonically (sorted keys).
func (m *Manifest) Marshal() ([]byte, error) {
	m.UpdatedAt = time.Now().Unix()
	return json.MarshalIndent(m, "", "  ")
}

// SaveLocal atomically writes the manifest to a local path.
func (m *Manifest) SaveLocal(path string) error {
	data, err := m.Marshal()
	if err != nil {
		return err
	}
	return backend.WriteFileAtomic(path, data, 0o644)
}

// SortedPaths returns entry keys in stable order (used by tests and diffs).
func (m *Manifest) SortedPaths() []string {
	out := make([]string, 0, len(m.Entries))
	for k := range m.Entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Set adds or replaces an entry.
func (m *Manifest) Set(e ManifestEntry) { m.Entries[e.RemotePath] = e }

// Remove deletes an entry.
func (m *Manifest) Remove(rel string) { delete(m.Entries, rel) }

// Entry returns the entry for rel, or nil.
func (m *Manifest) Entry(rel string) *ManifestEntry {
	if e, ok := m.Entries[rel]; ok {
		return &e
	}
	return nil
}

// LoadRemoteManifest downloads the manifest from the target via Open.
// A missing manifest yields nil, nil.
func LoadRemoteManifest(ctx context.Context, t SyncTarget, root string) (*Manifest, error) {
	opener, ok := t.(Open)
	if !ok {
		return nil, fmt.Errorf("target does not support reading files")
	}
	rc, err := opener.Open(ctx, JoinRemote(root, ManifestPathRel))
	if err != nil {
		return nil, nil
	}
	defer rc.Close()
	return ReadManifest(rc)
}

// WriteRemoteManifest uploads the manifest via the target's Put. Manifest
// writes are infrequent (once per completed operation batch), so simplicity
// beats streaming.
func WriteRemoteManifest(ctx context.Context, t SyncTarget, root string, m *Manifest) error {
	data, err := m.Marshal()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "auralis-manifest-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	tmp := path.Join(dir, "manifest.json")
	if err := backend.WriteFileAtomic(tmp, data, 0o644); err != nil {
		return err
	}
	return t.Put(ctx, tmp, JoinRemote(root, ManifestPathRel), nil)
}

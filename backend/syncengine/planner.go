package syncengine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/vekhyat/Auralis/backend/library"
)

// OpKind classifies one planned change.
type OpKind string

const (
	OpAdd    OpKind = "add"
	OpUpdate OpKind = "update"
	OpMove   OpKind = "move"
	OpDelete OpKind = "delete"
	OpKeep   OpKind = "keep"
)

// Operation is one planned change.
type Operation struct {
	Kind OpKind `json:"kind"`
	// Album/Artist carry preview grouping info.
	Album  string `json:"album"`
	Artist string `json:"artist"`
	// Source is the PC path for add/update; the old remote path for move.
	Source string `json:"source"`
	// Remote is the destination, relative to the music root.
	Remote string `json:"remote"`
	// Size projects the space the op consumes (delivered size).
	Size int64 `json:"size"`
	// Track is the source track for add/update/move/keep. Keeps carry it
	// so playlist mapping, ModTime refreshes, and missing-file repair all
	// work from the snapshot without rescanning.
	Track *SourceTrack `json:"-"`
	// TrackSourcePath lets snapshots reattach the track pointer.
	TrackSourcePath string `json:"track_source_path,omitempty"`
}

// AlbumGroup groups preview counts by album.
type AlbumGroup struct {
	Album    string `json:"album"`
	Adds     int    `json:"adds"`
	Updates  int    `json:"updates"`
	Moves    int    `json:"moves"`
	Deletes  int    `json:"deletes"`
	Keeps    int    `json:"keeps"`
	AddBytes int64  `json:"add_bytes"`
}

// Plan is the result of planning one sync.
type Plan struct {
	Ops     []Operation  `json:"ops"`
	Groups  []AlbumGroup `json:"groups"`
	Adds    int          `json:"adds"`
	Updates int          `json:"updates"`
	Moves   int          `json:"moves"`
	Deletes int          `json:"deletes"`
	Keeps   int          `json:"keeps"`
	// FreeNeeded is the projected new bytes required (adds + updates only;
	// moves and deletes-to-trash do not free anything).
	FreeNeeded int64 `json:"free_needed"`
	// FreeAvailable is the device's reported free bytes at plan time.
	FreeAvailable int64 `json:"free_available"`
	Insufficient  bool  `json:"insufficient"`
	// ProfileID and ProfileVersion identify the layout the plan was built
	// for; Policy is the format policy. They bind the plan Hash to the
	// approved settings so a settings change invalidates journal matching,
	// and they survive Snapshot.Plan round-trips for execution.
	ProfileID      string       `json:"profile_id,omitempty"`
	ProfileVersion int          `json:"profile_version,omitempty"`
	Policy         FormatPolicy `json:"policy,omitempty"`
}

// Summary renders the headline for the preview, e.g. "+312 tracks, 9.4 GB; −12 tracks".
func (p *Plan) Summary() string {
	parts := []string{}
	if p.Adds > 0 {
		parts = append(parts, fmt.Sprintf("+%d tracks, %s", p.Adds, HumanBytes(p.AddBytes())))
	}
	if p.Updates > 0 {
		parts = append(parts, fmt.Sprintf("~%d updated", p.Updates))
	}
	if p.Moves > 0 {
		parts = append(parts, fmt.Sprintf("→%d moved", p.Moves))
	}
	if p.Deletes > 0 {
		parts = append(parts, fmt.Sprintf("−%d tracks", p.Deletes))
	}
	if len(parts) == 0 {
		return "No changes"
	}
	return strings.Join(parts, "; ")
}

// AddBytes sums bytes for add operations.
func (p *Plan) AddBytes() int64 {
	var total int64
	for _, op := range p.Ops {
		if op.Kind == OpAdd || op.Kind == OpUpdate {
			total += op.Size
		}
	}
	return total
}

// Hash returns a stable digest of the plan for journal matching. It binds
// the operation list (including each source track's content hash) and the
// profile/policy settings, so a retag, re-encode, or settings change can
// never silently reuse a journal recorded for different bytes.
func (p *Plan) Hash() string {
	sum := sha256.New()
	fmt.Fprintf(sum, "profile:%s|%d|%s\n", p.ProfileID, p.ProfileVersion, p.Policy.SettingsKey())
	for _, op := range p.Ops {
		trackHash := ""
		if op.Track != nil {
			trackHash = op.Track.Hash
		}
		fmt.Fprintf(sum, "%s|%s|%s|%s\n", op.Kind, op.Source, op.Remote, trackHash)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// PlanOptions carries everything the planner needs.
type PlanOptions struct {
	Tracks         []SourceTrack
	Profile        library.Profile
	ProfileVersion int
	Policy         FormatPolicy
	Manifest       *Manifest // nil treated as empty
	FreeBytes      int64
}

// Plan computes the diff between the desired selection and the manifest.
// Manifest entries with unsafe remote paths are ignored: they are left
// alone on the device and never become delete or move sources, so a corrupt
// or hostile manifest cannot drive the engine outside the music root.
// Operation Size is the source file size, an upper bound on the delivered
// bytes when a transcode policy shrinks the file. Free-space planning is
// therefore conservative for transcodes, never optimistic.
func PlanSelect(opts PlanOptions) *Plan {
	m := opts.Manifest
	if m == nil {
		m = NewManifest(opts.Profile.ID)
	}
	desired := map[string]*SourceTrack{}
	takenFold := map[string]bool{}
	for i := range opts.Tracks {
		tr := &opts.Tracks[i]
		rel := opts.Profile.RelativePath(library.TrackFields{
			Title: tr.Title, Artist: tr.Artist, AlbumArtist: tr.AlbumArtist,
			Album: tr.Album, Year: tr.Year, TrackNumber: tr.TrackNumber,
			DiscNumber: tr.DiscNumber, DiscTotal: tr.DiscTotal,
		})
		// Track-aware extension so ALAC-in-".m4a" lands on the transcoded
		// name instead of colliding with a lossy ".m4a" copy.
		rel = uniqueRel(takenFold, opts.Profile.SanitizeRelativePath(rel+opts.Policy.RemoteExtForTrack(tr)))
		if err := ValidateRemoteRel(rel); err != nil {
			// SanitizeRelativePath should never produce this; skipping
			// beats syncing to an unmanaged path.
			continue
		}
		takenFold[strings.ToLower(rel)] = true
		desired[rel] = tr
	}
	safeManifest := map[string]ManifestEntry{}
	for _, rel := range m.SortedPaths() {
		if ValidateRemoteRel(rel) != nil {
			continue
		}
		safeManifest[rel] = m.Entries[rel]
	}

	// Pass 1: same remote path in the manifest means keep or update.
	plan := &Plan{
		FreeAvailable: opts.FreeBytes,
		ProfileID:     opts.Profile.ID, ProfileVersion: opts.ProfileVersion, Policy: opts.Policy,
	}
	placed := map[*SourceTrack]bool{}
	handledManifestRels := map[string]bool{}
	for _, rel := range sortedKeys(desired) {
		tr := desired[rel]
		entry := m.Entry(rel)
		if entry == nil {
			continue
		}
		handledManifestRels[rel] = true
		placed[tr] = true
		match := entry.Hash == tr.Hash && entry.ProfileID == opts.Profile.ID &&
			entry.ProfileVersion == opts.ProfileVersion && entry.Transcode == opts.Policy.SettingsKey()
		if match {
			plan.addOp(OpKeep, "", rel, tr)
		} else {
			plan.addOp(OpUpdate, tr.Path, rel, tr)
		}
	}

	// Pass 2: an unplaced track whose hash matches an unhandled manifest
	// entry is a move (no re-copy). A profile or format change forces an
	// update instead, which pass 1 would have classified at the same rel;
	// here the source is stale for the new settings, so we re-copy.
	byHash := map[string][]string{}
	for _, rel := range sortedManifestKeys(safeManifest) {
		if !handledManifestRels[rel] {
			byHash[safeManifest[rel].Hash] = append(byHash[safeManifest[rel].Hash], rel)
		}
	}
	for _, rel := range sortedKeys(desired) {
		tr := desired[rel]
		if placed[tr] {
			continue
		}
		candidates := byHash[tr.Hash]
		if len(candidates) == 0 {
			plan.addOp(OpAdd, tr.Path, rel, tr)
			continue
		}
		old := candidates[0]
		byHash[tr.Hash] = candidates[1:]
		handledManifestRels[old] = true
		oldEntry := safeManifest[old]
		if oldEntry.ProfileID == opts.Profile.ID && oldEntry.ProfileVersion == opts.ProfileVersion &&
			oldEntry.Transcode == opts.Policy.SettingsKey() {
			plan.addOp(OpMove, old, rel, tr)
		} else {
			// Stale settings: re-copy to the new path, and the old file is
			// still removed below because it is stale for this plan.
			plan.addOp(OpAdd, tr.Path, rel, tr)
			plan.addOp(OpDelete, old, old, nil)
			plan.Ops[len(plan.Ops)-1].Album = oldEntry.Album
			plan.Ops[len(plan.Ops)-1].Artist = oldEntry.Artist
		}
	}

	// Pass 3: manifest entries no longer desired are deleted (to trash).
	for _, rel := range sortedManifestKeys(safeManifest) {
		if handledManifestRels[rel] {
			continue
		}
		if _, wanted := desired[rel]; wanted {
			continue
		}
		entry := safeManifest[rel]
		plan.addOp(OpDelete, rel, rel, nil)
		plan.Ops[len(plan.Ops)-1].Album = entry.Album
		plan.Ops[len(plan.Ops)-1].Artist = entry.Artist
	}

	plan.FreeNeeded = plan.AddBytes()
	plan.Insufficient = opts.FreeBytes >= 0 && plan.FreeNeeded > opts.FreeBytes
	plan.Group()
	return plan
}

// addOp records one planned operation. source is the PC path for add/update
// and the old remote-relative path for move and delete.
func (p *Plan) addOp(kind OpKind, source, rel string, tr *SourceTrack) {
	var album, artist string
	var size int64
	if tr != nil {
		album, artist = tr.Album, tr.Artist
		size = tr.Size
	}
	op := Operation{Kind: kind, Album: album, Artist: artist, Source: source, Remote: rel, Size: size, Track: tr}
	p.Ops = append(p.Ops, op)
	switch kind {
	case OpAdd:
		p.Adds++
	case OpUpdate:
		p.Updates++
	case OpMove:
		p.Moves++
	case OpDelete:
		p.Deletes++
	case OpKeep:
		p.Keeps++
	}
}

// Group rebuilds the album-grouped preview from the flat op list.
func (p *Plan) Group() {
	byAlbum := map[string]*AlbumGroup{}
	for _, op := range p.Ops {
		name := op.Album
		if name == "" {
			name = "(unknown album)"
		}
		g := byAlbum[name]
		if g == nil {
			g = &AlbumGroup{Album: name}
			byAlbum[name] = g
		}
		switch op.Kind {
		case OpAdd:
			g.Adds++
			g.AddBytes += op.Size
		case OpUpdate:
			g.Updates++
			g.AddBytes += op.Size
		case OpMove:
			g.Moves++
		case OpDelete:
			g.Deletes++
		case OpKeep:
			g.Keeps++
		}
	}
	names := make([]string, 0, len(byAlbum))
	for name := range byAlbum {
		names = append(names, name)
	}
	sort.Strings(names)
	p.Groups = nil
	for _, name := range names {
		p.Groups = append(p.Groups, *byAlbum[name])
	}
}

func sortedKeys(m map[string]*SourceTrack) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedManifestKeys(m map[string]ManifestEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// HumanBytes renders bytes compactly for previews.
func HumanBytes(v int64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d B", v)
	}
	div, exp := int64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(v)/float64(div), "KMGTPE"[exp])
}

// uniqueRel returns rel, or "name (2).ext", "name (3).ext"... when two
// tracks would land on the same device path (e.g. duplicate tags), so one
// cannot silently replace the other. takenFold holds lower-cased paths
// because FAT, exFAT and Android shared storage ignore case.
func uniqueRel(takenFold map[string]bool, rel string) string {
	if !takenFold[strings.ToLower(rel)] {
		return rel
	}
	ext := path.Ext(rel)
	base := strings.TrimSuffix(rel, ext)
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, n, ext)
		if !takenFold[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

// SanitizedManifestEntry builds the manifest entry for a completed operation.
func SanitizedManifestEntry(tr *SourceTrack, rel, profileID string, profileVersion int, policy FormatPolicy, size int64) ManifestEntry {
	return ManifestEntry{
		RemotePath: rel, SourcePath: tr.Path, Hash: tr.Hash, Size: size,
		ProfileID: profileID, ProfileVersion: profileVersion,
		Transcode: policy.SettingsKey(), ModTime: tr.ModTime.Unix(),
		Title: tr.Title, Artist: tr.Artist, Album: tr.Album, DurationSec: tr.DurationSec,
	}
}

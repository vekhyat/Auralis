package syncengine

import (
	"encoding/json"
	"os"
	"time"

	"github.com/vekhyat/Auralis/backend"
)

// SourceTrack JSON support for persisted plans.
func (t SourceTrack) MarshalJSON() ([]byte, error) {
	type wire struct {
		Path        string    `json:"path"`
		Size        int64     `json:"size"`
		ModTime     time.Time `json:"mod_time"`
		Hash        string    `json:"hash"`
		Title       string    `json:"title"`
		Artist      string    `json:"artist"`
		AlbumArtist string    `json:"album_artist"`
		Album       string    `json:"album"`
		Year        string    `json:"year"`
		TrackNumber int       `json:"track_number"`
		DiscNumber  int       `json:"disc_number"`
		DiscTotal   int       `json:"disc_total"`
		Ext         string    `json:"ext"`
		DurationSec int       `json:"duration_sec"`
		AddedAt     time.Time `json:"added_at"`
		Lossless    bool      `json:"lossless,omitempty"`
		Codec       string    `json:"codec,omitempty"`
	}
	return json.Marshal(wire(t))
}

func (t *SourceTrack) UnmarshalJSON(data []byte) error {
	type wire struct {
		Path        string    `json:"path"`
		Size        int64     `json:"size"`
		ModTime     time.Time `json:"mod_time"`
		Hash        string    `json:"hash"`
		Title       string    `json:"title"`
		Artist      string    `json:"artist"`
		AlbumArtist string    `json:"album_artist"`
		Album       string    `json:"album"`
		Year        string    `json:"year"`
		TrackNumber int       `json:"track_number"`
		DiscNumber  int       `json:"disc_number"`
		DiscTotal   int       `json:"disc_total"`
		Ext         string    `json:"ext"`
		DurationSec int       `json:"duration_sec"`
		AddedAt     time.Time `json:"added_at"`
		Lossless    bool      `json:"lossless,omitempty"`
		Codec       string    `json:"codec,omitempty"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*t = SourceTrack(w)
	return nil
}

// PlanSnapshot persists a plan so an interrupted device sync can resume
// with the exact same operations after an app restart.
//
// The association fields (profile, policy, target, root) are additive:
// snapshots written before they existed load with zero values and are
// treated as unbound, while new snapshots record exactly which device
// configuration the previewed ops belong to. Callers outside syncengine
// (e.g. the Wails bindings) must check Matches before running a snapshot
// against a live target so a profile or policy change forces a fresh
// preview instead of executing stale ops with new settings.
type PlanSnapshot struct {
	Hash   string        `json:"hash"`
	Ops    []Operation   `json:"ops"`
	Tracks []SourceTrack `json:"tracks"`
	// ProfileID and ProfileVersion identify the layout the plan was built for.
	ProfileID      string `json:"profile_id,omitempty"`
	ProfileVersion int    `json:"profile_version,omitempty"`
	// Policy is the format policy the plan was built for.
	Policy FormatPolicy `json:"policy,omitempty"`
	// TargetID is the stable device ID the plan was previewed for.
	TargetID string `json:"target_id,omitempty"`
	// Root is the music root the plan was previewed for.
	Root string `json:"root,omitempty"`
	// FreeAvailable is the device free bytes observed at plan time.
	FreeAvailable int64 `json:"free_available,omitempty"`
	// FreeKnown reports whether FreeAvailable was actually measured.
	// Old snapshots have false and are treated as unknown space.
	FreeKnown bool `json:"free_known,omitempty"`
}

func NewPlanSnapshot(p *Plan) *PlanSnapshot {
	snap := &PlanSnapshot{Hash: p.Hash(), Ops: append([]Operation(nil), p.Ops...)}
	// Carry the settings the plan was built for so Snapshot.Plan restores
	// them and Matches can enforce exact-preview authorization even when
	// the caller used the target-agnostic constructor.
	snap.ProfileID, snap.ProfileVersion, snap.Policy = p.ProfileID, p.ProfileVersion, p.Policy
	seen := map[string]bool{}
	for i := range snap.Ops {
		if snap.Ops[i].Track != nil {
			snap.Ops[i].TrackSourcePath = snap.Ops[i].Track.Path
			if !seen[snap.Ops[i].Track.Path] {
				seen[snap.Ops[i].Track.Path] = true
				snap.Tracks = append(snap.Tracks, *snap.Ops[i].Track)
			}
		}
	}
	snap.FreeAvailable = p.FreeAvailable
	// A stored zero could be "no space" or "old snapshot"; only mark known
	// when the plan actually carried a measurement. PlanSelect always sets
	// FreeAvailable from opts, and callers pass -1 for unknown, so any
	// non-negative value is a real observation.
	snap.FreeKnown = p.FreeAvailable >= 0
	return snap
}

// NewPlanSnapshotForTarget is NewPlanSnapshot plus the policy/profile/target
// association. Prefer it whenever the caller knows the target.
func NewPlanSnapshotForTarget(p *Plan, profileID string, profileVersion int, policy FormatPolicy, targetID, root string) *PlanSnapshot {
	snap := NewPlanSnapshot(p)
	snap.ProfileID = profileID
	snap.ProfileVersion = profileVersion
	snap.Policy = policy
	snap.TargetID = targetID
	snap.Root = root
	return snap
}

// Matches reports whether the snapshot may run against the given live
// configuration. Unbound fields (zero values from old snapshots) act as
// wildcards so interrupted syncs planned before the association existed
// still resume.
func (s *PlanSnapshot) Matches(profileID string, profileVersion int, policy FormatPolicy, targetID, root string) bool {
	if s.ProfileID != "" && s.ProfileID != profileID {
		return false
	}
	if s.ProfileID != "" && s.ProfileVersion != profileVersion {
		return false
	}
	if s.Policy.Mode != "" && s.Policy.Mode != policy.Mode {
		return false
	}
	if s.TargetID != "" && s.TargetID != targetID {
		return false
	}
	if s.Root != "" && s.Root != root {
		return false
	}
	return true
}

// Plan rebuilds a runnable Plan from the snapshot, reattaching track
// pointers by source path. Keeps retain their tracks so ModTime refreshes,
// playlist mapping, and missing-file repair all work without rescanning;
// deletes never carry tracks. Profile, version, policy, counts, free-space
// figures, and album groups are all restored so the rebuilt plan executes
// exactly the previewed operations.
func (s *PlanSnapshot) Plan() *Plan {
	p := &Plan{ProfileID: s.ProfileID, ProfileVersion: s.ProfileVersion, Policy: s.Policy}
	byPath := map[string]*SourceTrack{}
	for i := range s.Tracks {
		byPath[s.Tracks[i].Path] = &s.Tracks[i]
	}
	for i := range s.Ops {
		op := s.Ops[i]
		if op.Kind == OpAdd || op.Kind == OpUpdate || op.Kind == OpMove || op.Kind == OpKeep {
			if tr, ok := byPath[op.TrackSourcePath]; ok {
				op.Track = tr
			} else if op.Track != nil {
				// Old snapshots may inline Track via JSON ignoring "-";
				// keep the inlined pointer as a fallback.
			} else {
				op.Track = nil
			}
		}
		p.Ops = append(p.Ops, op)
		switch op.Kind {
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
	p.FreeNeeded = p.AddBytes()
	if s.FreeKnown {
		p.FreeAvailable = s.FreeAvailable
		p.Insufficient = p.FreeAvailable >= 0 && p.FreeNeeded > p.FreeAvailable
	} else {
		p.FreeAvailable = -1
		p.Insufficient = false
	}
	p.Group()
	return p
}

func (s *PlanSnapshot) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return backend.WriteFileAtomic(path, data, 0o644)
}

func LoadPlanSnapshot(path string) (*PlanSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s PlanSnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

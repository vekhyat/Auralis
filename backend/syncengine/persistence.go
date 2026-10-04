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
type PlanSnapshot struct {
	Hash   string        `json:"hash"`
	Ops    []Operation   `json:"ops"`
	Tracks []SourceTrack `json:"tracks"`
}

func NewPlanSnapshot(p *Plan) *PlanSnapshot {
	snap := &PlanSnapshot{Hash: p.Hash(), Ops: append([]Operation(nil), p.Ops...)}
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
	return snap
}

// Plan rebuilds a runnable Plan from the snapshot, reattaching track
// pointers by source path.
func (s *PlanSnapshot) Plan() *Plan {
	p := &Plan{}
	byPath := map[string]*SourceTrack{}
	for i := range s.Tracks {
		byPath[s.Tracks[i].Path] = &s.Tracks[i]
	}
	for i := range s.Ops {
		op := s.Ops[i]
		if op.Kind == OpAdd || op.Kind == OpUpdate || op.Kind == OpMove {
			if tr, ok := byPath[op.TrackSourcePath]; ok {
				op.Track = tr
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

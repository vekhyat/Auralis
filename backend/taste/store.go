package taste

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/vekhyat/Auralis/backend"
)

const (
	dbFileName        = "taste.db"
	eventsBucketName  = "TasteEvents"      // id -> TasteEvent JSON (deduped, re-import safe)
	profileBucketName = "TasteProfile"     // "cached" -> Profile JSON (recomputable from events)
	feedbackBucket    = "TasteFeedback"    // key -> Feedback JSON
	gapsBucket        = "TasteGaps"        // spotify artist id -> gapsCacheEntry JSON
	metaBucket        = "TasteMeta"        // last_sync, event_count, client ids (non-secret)
)

// Store persists raw events, the derived profile cache, user feedback and the
// discography-gap cache in a single bbolt database inside the app data dir.
type Store struct {
	db *bolt.DB
	mu sync.RWMutex
}

type gapsCacheEntry struct {
	FetchedAt time.Time   `json:"fetched_at"`
	Albums    []GapAlbum  `json:"albums"`
}

// GapAlbum is one missing album of an artist.
type GapAlbum struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	AlbumType   string    `json:"album_type"`
	ReleaseDate string    `json:"release_date"`
	Image       string    `json:"images"`
	TotalTracks int       `json:"total_tracks"`
}

// Feedback captures persisted user intent that survives syncs.
type Feedback struct {
	DismissedItems map[string]bool `json:"dismissed_items,omitempty"` // item fingerprint -> dismissed
	PinnedArtists  []string        `json:"pinned_artists,omitempty"`
	BannedArtists  []string        `json:"banned_artists,omitempty"`
}

func OpenStore(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range []string{eventsBucketName, profileBucketName, feedbackBucket, gapsBucket, metaBucket} {
			if _, err := tx.CreateBucketIfNotExists([]byte(b)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := backend.RestrictPrivateFilePath(path); err != nil {
		// Non-fatal: hardening is best-effort on filesystems that support it.
		_ = err
	}
	return s, nil
}

func DefaultStorePath() (string, error) {
	dir, err := backend.EnsureAppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dbFileName), nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// AddEvents stores events by stable ID so re-importing the same history is
// idempotent. It returns the number of newly added events.
func (s *Store) AddEvents(events []TasteEvent) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	added := 0
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(eventsBucketName))
		for _, e := range events {
			if e.ID == "" {
				return errors.New("taste: event missing ID")
			}
			if b.Get([]byte(e.ID)) != nil {
				continue
			}
			raw, err := json.Marshal(e)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(e.ID), raw); err != nil {
				return err
			}
			added++
		}
		return nil
	})
	return added, err
}

// AllEvents returns every stored event, oldest first.
func (s *Store) AllEvents() ([]TasteEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	events := []TasteEvent{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(eventsBucketName)).ForEach(func(_, v []byte) error {
			var e TasteEvent
			if err := json.Unmarshal(v, &e); err != nil {
				return err
			}
			events = append(events, e)
			return nil
		})
	})
	sort.Slice(events, func(i, j int) bool { return events[i].Timestamp.Before(events[j].Timestamp) })
	return events, err
}

func (s *Store) EventCount() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	err := s.db.View(func(tx *bolt.Tx) error {
		n = tx.Bucket([]byte(eventsBucketName)).Stats().KeyN
		return nil
	})
	return n, err
}

func (s *Store) SetCachedProfile(p Profile) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(profileBucketName)).Put([]byte("cached"), raw)
	})
}

func (s *Store) GetCachedProfile() (*Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var p Profile
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(profileBucketName)).Get([]byte("cached"))
		if raw == nil {
			return errors.New("taste: no cached profile")
		}
		return json.Unmarshal(raw, &p)
	})
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) getFeedbackLocked() (Feedback, error) {
	fb := Feedback{DismissedItems: map[string]bool{}}
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(feedbackBucket)).Get([]byte("feedback"))
		if raw == nil {
			return nil
		}
		return json.Unmarshal(raw, &fb)
	})
	if fb.DismissedItems == nil {
		fb.DismissedItems = map[string]bool{}
	}
	return fb, err
}

func (s *Store) GetFeedback() (Feedback, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getFeedbackLocked()
}

func (s *Store) putFeedback(fb Feedback) error {
	raw, err := json.Marshal(fb)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(feedbackBucket)).Put([]byte("feedback"), raw)
	})
}

func (s *Store) UpdateFeedback(mut func(*Feedback)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fb, err := s.getFeedbackLocked()
	if err != nil {
		return err
	}
	mut(&fb)
	return s.putFeedback(fb)
}

func (s *Store) SetGaps(artistID string, albums []GapAlbum, fetchedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(gapsCacheEntry{FetchedAt: fetchedAt, Albums: albums})
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(gapsBucket)).Put([]byte(artistID), raw)
	})
}

func (s *Store) GetGaps(artistID string) ([]GapAlbum, time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var entry gapsCacheEntry
	found := false
	_ = s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(gapsBucket)).Get([]byte(artistID))
		if raw != nil {
			if json.Unmarshal(raw, &entry) == nil {
				found = true
			}
		}
		return nil
	})
	if !found {
		return nil, time.Time{}, false
	}
	return entry.Albums, entry.FetchedAt, true
}

func (s *Store) SetMeta(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(metaBucket)).Put([]byte(key), []byte(value))
	})
}

func (s *Store) GetMeta(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value := ""
	found := false
	_ = s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(metaBucket)).Get([]byte(key))
		if raw != nil {
			value = string(raw)
			found = true
		}
		return nil
	})
	return value, found
}

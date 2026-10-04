package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

type HistoryItem struct {
	ID          string `json:"id"`
	SpotifyID   string `json:"spotify_id"`
	Title       string `json:"title"`
	Artists     string `json:"artists"`
	Album       string `json:"album"`
	DurationStr string `json:"duration_str"`
	CoverURL    string `json:"cover_url"`
	Quality     string `json:"quality"`
	Format      string `json:"format"`
	Path        string `json:"path"`
	Source      string `json:"source"`
	Timestamp   int64  `json:"timestamp"`
}

var (
	historyMu     sync.Mutex
	historyDB     *bolt.DB
	historyClosed bool
)

const (
	historyBucket = "DownloadHistory"
	maxHistory    = 10000
)

func InitHistoryDB(appName string) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	return openHistoryDBLocked()
}

func openHistoryDBLocked() error {
	if historyDB != nil {
		return nil
	}
	if historyClosed {
		return errors.New("history database is closed")
	}

	appDir, err := EnsureAppDataDir()
	if err != nil {
		return err
	}
	dbPath := filepath.Join(appDir, "history.db")

	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return err
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(historyBucket))
		return err
	})
	if err != nil {
		_ = db.Close()
		return err
	}

	historyDB = db
	return nil
}

func CloseHistoryDB() error {
	historyMu.Lock()
	defer historyMu.Unlock()
	historyClosed = true
	return closeHistoryDBLocked()
}

func closeHistoryDBLocked() error {
	if historyDB == nil {
		return nil
	}
	err := historyDB.Close()
	historyDB = nil
	return err
}

func ResetHistoryStoreForTest() {
	historyMu.Lock()
	defer historyMu.Unlock()
	_ = closeHistoryDBLocked()
	historyClosed = false
}

func withHistoryDB(fn func(*bolt.DB) error) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	if err := openHistoryDBLocked(); err != nil {
		return err
	}
	return fn(historyDB)
}

func AddHistoryItem(item HistoryItem, appName string) error {
	err := withHistoryDB(func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			b, err := tx.CreateBucketIfNotExists([]byte(historyBucket))
			if err != nil {
				return err
			}
			id, _ := b.NextSequence()

			item.ID = fmt.Sprintf("%d-%d", time.Now().UnixNano(), id)
			item.Timestamp = time.Now().Unix()

			buf, err := json.Marshal(item)
			if err != nil {
				return err
			}

			if b.Stats().KeyN >= maxHistory {
				c := b.Cursor()

				toDelete := maxHistory / 20
				if toDelete < 1 {
					toDelete = 1
				}

				count := 0
				for k, _ := c.First(); k != nil && count < toDelete; k, _ = c.Next() {
					if err := b.Delete(k); err != nil {
						return err
					}
					count++
				}
			}

			return b.Put([]byte(item.ID), buf)
		})
	})
	if err != nil {
		return fmt.Errorf("write download history: %w", err)
	}
	return nil
}

func GetHistoryItems(appName string) ([]HistoryItem, error) {
	var items []HistoryItem
	err := withHistoryDB(func(db *bolt.DB) error {
		return db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(historyBucket))
			if b == nil {
				return nil
			}
			c := b.Cursor()

			for k, v := c.First(); k != nil; k, v = c.Next() {
				var item HistoryItem
				if err := json.Unmarshal(v, &item); err == nil {
					items = append(items, item)
				}
			}
			return nil
		})
	})

	sort.Slice(items, func(i, j int) bool {
		return items[i].Timestamp > items[j].Timestamp
	})

	return items, err
}

func ClearHistory(appName string) error {
	return withHistoryDB(func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			return tx.DeleteBucket([]byte(historyBucket))
		})
	})
}

type FetchHistoryItem struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Info       string `json:"info"`
	Image      string `json:"image"`
	Data       string `json:"data"`
	IsExplicit bool   `json:"is_explicit,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

const (
	fetchHistoryBucket = "FetchHistory"
)

func AddFetchHistoryItem(item FetchHistoryItem, appName string) error {
	return withHistoryDB(func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			b, err := tx.CreateBucketIfNotExists([]byte(fetchHistoryBucket))
			if err != nil {
				return err
			}
			id, _ := b.NextSequence()

			if item.URL != "" {
				c := b.Cursor()
				for k, v := c.First(); k != nil; k, v = c.Next() {
					var existing FetchHistoryItem
					if err := json.Unmarshal(v, &existing); err == nil {
						if existing.URL == item.URL && existing.Type == item.Type {
							if err := b.Delete(k); err != nil {
								return err
							}
						}
					}
				}
			}

			item.ID = fmt.Sprintf("%d-%d", time.Now().UnixNano(), id)
			item.Timestamp = time.Now().Unix()

			buf, err := json.Marshal(item)
			if err != nil {
				return err
			}

			if b.Stats().KeyN >= maxHistory {
				c := b.Cursor()
				toDelete := maxHistory / 20
				if toDelete < 1 {
					toDelete = 1
				}
				count := 0
				for k, _ := c.First(); k != nil && count < toDelete; k, _ = c.Next() {
					if err := b.Delete(k); err != nil {
						return err
					}
					count++
				}
			}

			return b.Put([]byte(item.ID), buf)
		})
	})
}

func GetFetchHistoryItems(appName string) ([]FetchHistoryItem, error) {
	var items []FetchHistoryItem
	err := withHistoryDB(func(db *bolt.DB) error {
		return db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(fetchHistoryBucket))
			if b == nil {
				return nil
			}
			c := b.Cursor()

			for k, v := c.First(); k != nil; k, v = c.Next() {
				var item FetchHistoryItem
				if err := json.Unmarshal(v, &item); err == nil {
					items = append(items, item)
				}
			}
			return nil
		})
	})

	sort.Slice(items, func(i, j int) bool {
		return items[i].Timestamp > items[j].Timestamp
	})

	return items, err
}

func ClearFetchHistory(appName string) error {
	return withHistoryDB(func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			return tx.DeleteBucket([]byte(fetchHistoryBucket))
		})
	})
}

func ClearFetchHistoryByType(itemType string, appName string) error {
	return withHistoryDB(func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(fetchHistoryBucket))
			if b == nil {
				return nil
			}

			var keysToDelete [][]byte

			c := b.Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var item FetchHistoryItem
				if err := json.Unmarshal(v, &item); err == nil {
					if item.Type == itemType {
						keysToDelete = append(keysToDelete, append([]byte(nil), k...))
					}
				}
			}

			for _, k := range keysToDelete {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

func DeleteHistoryItem(id string, appName string) error {
	return withHistoryDB(func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(historyBucket))
			if b == nil {
				return nil
			}

			return b.Delete([]byte(id))
		})
	})
}

func DeleteFetchHistoryItem(id string, appName string) error {
	return withHistoryDB(func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(fetchHistoryBucket))
			if b == nil {
				return nil
			}
			return b.Delete([]byte(id))
		})
	})
}

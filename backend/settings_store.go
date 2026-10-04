package backend

import (
	"bytes"
	"encoding/json"
	"os"
	"sync"
)

// settingsRepository is the only writer for config.json. Load sanitizes in
// memory and does not write. Migrate is the startup path that rewrites the
// file, and only when the canonical bytes differ.
type settingsRepository struct {
	mu sync.Mutex
}

var settingsRepo = &settingsRepository{}

func LoadConfigSettings() (map[string]interface{}, error) {
	return settingsRepo.Load()
}

func SaveConfigSettings(settings map[string]interface{}) error {
	return settingsRepo.Save(settings)
}

func MigratePersistedConfigSettings() error {
	return settingsRepo.Migrate()
}

// SanitizePersistedConfigSettings remains for callers that still name the
// startup rewrite. It is the migration, not a write-on-read.
func SanitizePersistedConfigSettings() error {
	return MigratePersistedConfigSettings()
}

func (r *settingsRepository) Load() (map[string]interface{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readSanitized()
}

func (r *settingsRepository) Save(settings map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	payload, err := MarshalConfigSettings(SanitizeSettingsMap(settings))
	if err != nil {
		return err
	}
	path, err := GetConfigPath()
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, payload, 0o644)
}

func (r *settingsRepository) Migrate() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	path, err := GetConfigPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	payload, err := MarshalConfigSettings(SanitizeSettingsMap(FlattenConfigSettings(raw)))
	if err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(data), bytes.TrimSpace(payload)) {
		return nil
	}
	return WriteFileAtomic(path, payload, 0o644)
}

func (r *settingsRepository) readSanitized() (map[string]interface{}, error) {
	path, err := GetConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return SanitizeSettingsMap(FlattenConfigSettings(raw)), nil
}

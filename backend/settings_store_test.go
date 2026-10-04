package backend

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func isolateAppData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(appDataDirEnv, dir)
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return dir
}

func TestLoadSettingsDoesNotRewrite(t *testing.T) {
	dir := isolateAppData(t)
	path := filepath.Join(dir, "config.json")
	original := []byte("{\n  \"downloader\": \"not-a-service\",\n  \"customTidalApi\": \"http://evil.example/api\",\n  \"kept\": \"value\"\n}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfigSettings()
	if err != nil {
		t.Fatal(err)
	}
	if loaded["downloader"] != "auto" {
		t.Fatalf("downloader = %#v, want auto", loaded["downloader"])
	}
	if loaded["customTidalApi"] != "" {
		t.Fatalf("customTidalApi = %#v, want empty", loaded["customTidalApi"])
	}
	if loaded["kept"] != "value" {
		t.Fatalf("kept = %#v", loaded["kept"])
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, after) {
		t.Fatalf("load rewrote config:\n%s", after)
	}
}

func TestMigrateSettingsIsAtomicAndIdempotent(t *testing.T) {
	dir := isolateAppData(t)
	path := filepath.Join(dir, "config.json")
	original := []byte(`{"downloader":"QOBUZ","autoOrder":"qobuz-tidal","downloadPath":"D:\\Music","customTidalApi":"https://tidal.example/api","customThing":true}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MigratePersistedConfigSettings(); err != nil {
		t.Fatal(err)
	}
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(bytes.TrimSpace(original), bytes.TrimSpace(migrated)) {
		t.Fatal("migration left unsanitized config in place")
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(migrated, &raw); err != nil {
		t.Fatalf("migrated config is not JSON: %v\n%s", err, migrated)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".auralis-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}

	if err := MigratePersistedConfigSettings(); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(migrated, again) {
		t.Fatal("second migration rewrote an already canonical file")
	}

	loaded, err := LoadConfigSettings()
	if err != nil {
		t.Fatal(err)
	}
	if loaded["downloader"] != "qobuz" || loaded["autoOrder"] != "qobuz-tidal" {
		t.Fatalf("sanitized settings = %#v", loaded)
	}
	if loaded["customTidalApi"] != "https://tidal.example/api" {
		t.Fatalf("https custom API was dropped: %#v", loaded["customTidalApi"])
	}
	if loaded["customThing"] != true {
		t.Fatalf("unknown setting was dropped: %#v", loaded["customThing"])
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
}

func TestSaveSettingsRoundTripUnderContention(t *testing.T) {
	dir := isolateAppData(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := SaveConfigSettings(map[string]interface{}{
				"downloader":   "tidal",
				"downloadPath": "D:\\Music",
				"autoOrder":    "tidal-qobuz-amazon",
				"generation":   i,
			})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	path, err := GetConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("config path %s is outside the test app dir", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("saved config is not JSON: %v\n%s", err, data)
	}
	loaded, err := LoadConfigSettings()
	if err != nil {
		t.Fatal(err)
	}
	if loaded["downloader"] != "tidal" {
		t.Fatalf("downloader = %#v", loaded["downloader"])
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".auralis-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestWriteFileAtomicReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("old-contents-that-must-not-survive"), 0o644); err != nil {
		t.Fatal(err)
	}
	next := []byte("{\"ok\":true}\n")
	if err := WriteFileAtomic(path, next, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, next) {
		t.Fatalf("file = %q", got)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".auralis-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

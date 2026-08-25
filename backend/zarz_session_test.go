package backend

import (
	"strings"
	"testing"
	"time"
)

func TestImportZarzStoreUsesAuralisWhenValid(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	primary := []byte(`{"install_id":"auralis-id","sessions":{"tidal-web@1.1.0":{"session_id":"sess-a","session_secret":"secret-a","expires_at":"` + future + `"}}}`)
	legacy := []byte(`{"install_id":"spotiflac-id","sessions":{"tidal-web@1.1.0":{"session_id":"sess-s","session_secret":"secret-s","expires_at":"` + future + `"}}}`)

	store, migrated := importZarzStore(primary, true, legacy, true)
	if migrated {
		t.Fatal("valid Auralis session should not be replaced")
	}
	if store.InstallID != "auralis-id" {
		t.Fatalf("install id = %q", store.InstallID)
	}
	if store.Sessions["tidal-web@1.1.0"].SessionID != "sess-a" {
		t.Fatalf("session id = %q", store.Sessions["tidal-web@1.1.0"].SessionID)
	}
}

func TestImportZarzStoreCopiesSpotiFLACSession(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	legacy := []byte(`{"install_id":"spotiflac-id","sessions":{"tidal-web@1.1.0":{"session_id":"sess-s","session_secret":"secret-s","expires_at":"` + future + `"}}}`)

	store, migrated := importZarzStore(nil, false, legacy, true)
	if !migrated {
		t.Fatal("missing Auralis store should import SpotiFLAC session")
	}
	if store.InstallID != "spotiflac-id" {
		t.Fatalf("install id = %q", store.InstallID)
	}
	if !zarzStoreHasValidSession(store) {
		t.Fatal("imported session should be valid")
	}
}

func TestImportZarzStoreReplacesEmptyAuralisWithValidLegacy(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	primary := []byte(`{"install_id":"auralis-id","sessions":{}}`)
	legacy := []byte(`{"install_id":"spotiflac-id","sessions":{"qobuz-web@1.1.0":{"session_id":"sess-q","session_secret":"secret-q","expires_at":"` + future + `"}}}`)

	store, migrated := importZarzStore(primary, true, legacy, true)
	if !migrated {
		t.Fatal("empty Auralis sessions should import a valid SpotiFLAC session")
	}
	if store.InstallID != "spotiflac-id" {
		t.Fatalf("install id = %q", store.InstallID)
	}
	if store.Sessions["qobuz-web@1.1.0"].SessionID != "sess-q" {
		t.Fatalf("session id = %q", store.Sessions["qobuz-web@1.1.0"].SessionID)
	}
}

func TestParseZarzStoreDataLegacyRecord(t *testing.T) {
	data := []byte(`{"install_id":"id1","session_id":"sess","session_secret":"secret"}`)
	store := parseZarzStoreData(data)
	if store.InstallID != "id1" {
		t.Fatalf("install id = %q", store.InstallID)
	}
	record := store.Sessions["tidal-web@1.1.0"]
	if record.SessionID != "sess" || record.SessionSecret != "secret" {
		t.Fatalf("legacy record not imported: %+v", record)
	}
	if strings.TrimSpace(record.AppVersion) == "" {
		t.Fatal("legacy record should get a default app version")
	}
}

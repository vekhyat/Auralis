package backend

import (
	"testing"
	"time"
)

func TestImportCommunitySessionKeepsValidAuralis(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	primary := []byte(`{"install_id":"auralis-id","session_id":"sess-a","session_secret":"secret-a","expires_at":"` + future + `"}`)
	legacy := []byte(`{"install_id":"spotiflac-id","session_id":"sess-s","session_secret":"secret-s","expires_at":"` + future + `"}`)

	record, migrated := importCommunitySession(primary, true, legacy, true)
	if migrated {
		t.Fatal("valid Auralis community session should not be replaced")
	}
	if record.InstallID != "auralis-id" || record.SessionID != "sess-a" {
		t.Fatalf("record = %+v", record)
	}
}

func TestImportCommunitySessionCopiesSpotiFLAC(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	legacy := []byte(`{"install_id":"spotiflac-id","session_id":"sess-s","session_secret":"secret-s","expires_at":"` + future + `"}`)

	record, migrated := importCommunitySession(nil, false, legacy, true)
	if !migrated {
		t.Fatal("missing Auralis community session should import SpotiFLAC session")
	}
	if record.InstallID != "spotiflac-id" || record.SessionID != "sess-s" {
		t.Fatalf("record = %+v", record)
	}
	if !communitySessionValid(record) {
		t.Fatal("imported community session should be valid")
	}
}

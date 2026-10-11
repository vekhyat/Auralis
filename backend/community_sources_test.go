package backend

import "testing"

func TestRetiredSourcesDoNotReturnFromSavedSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(appDataDirEnv, dir)
	writeCommunitySourceFixture(t, dir, `[
		{"id":"hifi-wolf.qqdl.site","name":"Hi-Fi / wolf.qqdl.site","service":"tidal","protocol":"hifi","base_url":"https://wolf.qqdl.site","enabled":true},
		{"id":"dab-xyz","name":"DAB Music","service":"qobuz","protocol":"dab","base_url":"https://dabmusic.xyz/","enabled":true},
		{"id":"qobuz-dl","name":"Qobuz-DL server","service":"qobuz","protocol":"qobuz-dl"},
		{"id":"qobuz-rest","name":"My REST server","service":"qobuz","protocol":"qobuz-rest","base_url":"http://127.0.0.1:8000","enabled":true}
	]`)
	rows, err := ListCommunitySources()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]CommunitySource{}
	for _, row := range rows {
		ids[row.ID] = row
	}
	for _, dead := range []string{"hifi-wolf.qqdl.site", "dab-xyz", "qobuz-dl"} {
		if _, ok := ids[dead]; ok {
			t.Fatalf("retired source %s came back from saved settings", dead)
		}
	}
	if own, ok := ids["qobuz-rest"]; !ok || own.BaseURL != "http://127.0.0.1:8000" {
		t.Fatalf("a user's own server under a template ID was dropped: %+v", own)
	}
	if len(rows) != len(defaultCommunitySources())+1 {
		t.Fatalf("got %d sources, want the built-in ones plus the user's server", len(rows))
	}
}

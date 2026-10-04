package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

func TestLiveCommunitySourceAdapters(t *testing.T) {
	if os.Getenv("AURALIS_LIVE_SOURCE_CHECK") != "1" {
		t.Skip("live community adapters are opt-in")
	}
	root := os.Getenv("AURALIS_LIVE_RESULTS_DIR")
	if !filepath.IsAbs(root) {
		t.Fatal("isolated results directory required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(appDataDirEnv, filepath.Join(root, "app-data"))
	t.Setenv("USERPROFILE", filepath.Join(root, "profile"))
	sources, err := ListCommunitySources()
	if err != nil {
		t.Fatal(err)
	}
	var rows []CommunitySourceCheck
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, source := range sources {
		if source.BaseURL == "" {
			rows = append(rows, CommunitySourceCheck{ID: source.ID, State: "not_configured", Message: "Requires a configured server"})
			continue
		}
		wg.Add(1)
		go func(source CommunitySource) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			row, err := CheckCommunitySource(source.ID, false)
			if err != nil {
				row = CommunitySourceCheck{ID: source.ID, State: "failed", Message: err.Error()}
			}
			mu.Lock()
			rows = append(rows, row)
			mu.Unlock()
		}(source)
	}
	wg.Wait()
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	body, err := json.MarshalIndent(rows, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(root, "community-adapters.json"), body, 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.State == "available" {
			audio, err := CheckCommunitySource(row.ID, true)
			if err == nil {
				body, _ := json.Marshal(audio)
				t.Log(string(body))
			}
		}
	}
}

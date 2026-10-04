package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestLiveSourceResolverAudit(t *testing.T) {
	if os.Getenv("AURALIS_LIVE_SOURCE_CHECK") != "1" {
		t.Skip("live resolver audit is opt-in")
	}
	root := os.Getenv("AURALIS_LIVE_RESULTS_DIR")
	if !filepath.IsAbs(root) {
		t.Fatal("isolated results directory required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(appDataDirEnv, filepath.Join(root, "app-data"))
	var rows []sourceAuditRow
	for _, id := range []string{"resource-songstats", "resource-songlink", "resource-spotify"} {
		row := sourceAuditRow{ID: id, Role: "resolver", State: "failed", CheckedAt: time.Now().UTC()}
		var times []float64
		for i := 0; i < 3; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			client := &SongLinkClient{client: &http.Client{Timeout: 12 * time.Second}, ctx: ctx}
			start := time.Now()
			var err error
			ok := false
			switch id {
			case "resource-songstats":
				links := resolvedTrackLinks{ISRC: "GBAYE0601690"}
				ok, err = client.resolveLinksViaSongstats(&links)
			case "resource-songlink":
				links := resolvedTrackLinks{ISRC: "GBAYE0601690"}
				ok, err = client.resolveLinksViaDeezerSongLink(&links, "2EqlS6tkEnglzr7tkKAAYD", "")
			case "resource-spotify":
				var isrc string
				isrc, err = client.GetISRCDirect("2EqlS6tkEnglzr7tkKAAYD")
				ok = isrc == "GBAYE0601690"
			}
			row.Attempts++
			if err == nil && ok {
				row.Successes++
				row.State = "available"
			}
			times = append(times, float64(time.Since(start))/float64(time.Millisecond))
			cancel()
		}
		sort.Float64s(times)
		row.MedianMS = times[len(times)/2]
		rows = append(rows, row)
		t.Log(fmt.Sprintf("%s: %d/%d, %.1fms", id, row.Successes, row.Attempts, row.MedianMS))
	}
	body, err := json.MarshalIndent(rows, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(root, "resolver-audit.json"), body, 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
}

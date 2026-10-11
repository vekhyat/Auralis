package library

import (
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// perfLibrary builds a synthetic scan of albums*perAlbum readable tracks laid
// out so that every per-file and album-wide fixable rule has work to do, the
// way a large, messy library does. Nothing is written to disk.
//
// By album number modulo 4:
//
//	0  tags are fine, file names miss the template      -> a PATH issue per track
//	1  a quarter of the tracks lack ALBUMARTIST         -> an ALBUMARTIST issue per gap
//	2  no track numbers (recoverable from the names)    -> a TRACKNUMBER issue per track
//	3  three artists, half the tracks have a disc no.   -> COMPILATION and DISCNUMBER
func perfLibrary(root string, albums, perAlbum int) *Scan {
	scan := &Scan{Root: root, Tracks: make([]Track, 0, albums*perAlbum)}
	for a := 0; a < albums; a++ {
		artist := fmt.Sprintf("Artist %04d", a)
		album := fmt.Sprintf("Album %04d", a)
		for n := 1; n <= perAlbum; n++ {
			title := fmt.Sprintf("Song %04d-%02d", a, n)
			rel := fmt.Sprintf("%s/%s/%02d - %s.flac", artist, album, n, title)
			track := Track{
				Path:            filepath.Join(root, filepath.FromSlash(rel)),
				RelPath:         rel,
				Format:          "flac",
				Title:           title,
				Artist:          artist,
				AlbumArtist:     artist,
				Album:           album,
				Year:            "2001",
				TrackNumber:     n,
				TrackTotal:      perAlbum,
				TrackNumberRaw:  strconv.Itoa(n),
				DurationSeconds: float64(120 + a%7 + n),
				Size:            1 << 20,
				CoverBytes:      4096, CoverWidth: 600, CoverHeight: 600, FolderCover: true,
			}
			switch a % 4 {
			case 1:
				if n%4 == 0 {
					track.AlbumArtist = ""
				}
			case 2:
				track.TrackNumber, track.TrackNumberRaw = 0, ""
			case 3:
				track.Artist = fmt.Sprintf("Guest %d", n%3)
				track.AlbumArtist = ""
				if n%2 == 0 {
					track.DiscNumber = 1
				}
			}
			scan.Tracks = append(scan.Tracks, track)
		}
	}
	return scan
}

// TestPlanBuildScalesToLargeLibraries guards the per-issue lookups: before the
// scan carried an index, every ALBUMARTIST/COMPILATION fix regrouped the whole
// library and every TRACKNUMBER/PATH fix scanned it for its track, which made a
// plan for a 50k-track library cost minutes.
func TestPlanBuildScalesToLargeLibraries(t *testing.T) {
	const albums, perAlbum = 1000, 20
	scan := perfLibrary(t.TempDir(), albums, perAlbum)
	profile := ProfileByID(ProfilePoweramp)

	start := time.Now()
	issues := DetectAll(scan, profile)
	detected := time.Since(start)

	counts := map[string]int{}
	for _, issue := range issues {
		if issue.Fixable {
			counts[issue.RuleID]++
		}
	}
	for _, rule := range []string{"ALBUMARTIST", "COMPILATION", "TRACKNUMBER", "DISCNUMBER", "PATH"} {
		if counts[rule] == 0 {
			t.Fatalf("synthetic library raised no fixable %s issue: %v", rule, counts)
		}
	}

	planStart := time.Now()
	plan := BuildPlan(scan, profile, issues)
	built := time.Since(planStart)
	if len(plan.Operations) == 0 {
		t.Fatal("expected a plan")
	}
	t.Logf("%d tracks, %d issues (%v fixable): detect %v, plan %v (%d operations)",
		len(scan.Tracks), len(issues), counts, detected, built, len(plan.Operations))

	// Seconds on any machine, against minutes without the index; the bound is
	// loose so a slow CI runner does not flake. The race detector alone costs
	// several-fold, so under -race the run is functional only.
	if raceEnabled {
		return
	}
	if total := detected + built; total > 5*time.Second {
		t.Fatalf("detect + plan took %v for %d tracks; per-issue lookups are no longer O(1)", total, len(scan.Tracks))
	}
}

func BenchmarkDetectAndPlan20kTracks(b *testing.B) {
	profile := ProfileByID(ProfilePoweramp)
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		scan := perfLibrary(b.TempDir(), 1000, 20)
		b.StartTimer()
		BuildPlan(scan, profile, DetectAll(scan, profile))
	}
}

package library

import (
	"fmt"
	"strings"
	"testing"
)

// gapTrack is a numbered track of album A/L. A zero number leaves it untagged.
func gapTrack(name string, number, disc int) Track {
	t := Track{
		Path:        `C:\m\A\L\` + name,
		RelPath:     "A/L/" + name,
		Format:      "flac",
		Album:       "L",
		DiscNumber:  disc,
		TrackNumber: number,
	}
	if number > 0 {
		t.TrackNumberRaw = fmt.Sprint(number)
	}
	return t
}

func trackNumberMessages(tracks ...Track) (gaps, duplicates []string) {
	scan := &Scan{Root: `C:\m`, Tracks: tracks}
	for _, issue := range trackNumberRule.Detect(scan, ProfileByID(ProfilePoweramp)) {
		switch {
		case strings.Contains(issue.Message, "may be missing"):
			gaps = append(gaps, issue.Message)
		case strings.Contains(issue.Message, "is also used by"):
			duplicates = append(duplicates, issue.Message)
		}
	}
	return gaps, duplicates
}

func TestTrackNumberGapNotReportedWhenOnlyDuplicatesCauseIt(t *testing.T) {
	cases := map[string][]Track{
		// "2" twice and no "3": the repeat is the likely 3, not a missing file.
		"duplicate where the gap is": {
			gapTrack("01.flac", 1, 0), gapTrack("02.flac", 2, 0), gapTrack("02b.flac", 2, 0), gapTrack("04.flac", 4, 0),
		},
		// Three files for numbers up to 3: nothing is missing.
		"duplicate at the top": {
			gapTrack("01.flac", 1, 0), gapTrack("03.flac", 3, 0), gapTrack("03b.flac", 3, 0),
		},
		// Two repeats of one number fill two gaps.
		"several repeats": {
			gapTrack("01.flac", 1, 0), gapTrack("01b.flac", 1, 0), gapTrack("01c.flac", 1, 0), gapTrack("04.flac", 4, 0),
		},
	}
	for name, tracks := range cases {
		t.Run(name, func(t *testing.T) {
			gaps, duplicates := trackNumberMessages(tracks...)
			if len(gaps) != 0 {
				t.Fatalf("duplicates alone must not read as missing tracks: %v", gaps)
			}
			if len(duplicates) == 0 {
				t.Fatal("the duplicate itself must still be reported")
			}
		})
	}
}

func TestTrackNumberGapStillReportedWhenTracksAreMissing(t *testing.T) {
	cases := []struct {
		name   string
		tracks []Track
		want   string
	}{
		{
			name:   "plain gap",
			tracks: []Track{gapTrack("01.flac", 1, 0), gapTrack("03.flac", 3, 0), gapTrack("04.flac", 4, 0)},
			want:   "disc 1 has 3 numbered tracks up to 4; some tracks may be missing",
		},
		{
			// 4 files but 5 numbers: at least one track is absent even though
			// one repeat could be renumbered.
			name: "repeat plus a real gap",
			tracks: []Track{
				gapTrack("01.flac", 1, 0), gapTrack("02.flac", 2, 0), gapTrack("02b.flac", 2, 0), gapTrack("05.flac", 5, 0),
			},
			want: "disc 1 has 3 numbered tracks up to 5; some tracks may be missing",
		},
		{
			name:   "second disc",
			tracks: []Track{gapTrack("1-01.flac", 1, 1), gapTrack("1-02.flac", 2, 1), gapTrack("2-01.flac", 1, 2), gapTrack("2-03.flac", 3, 2)},
			want:   "disc 2 has 2 numbered tracks up to 3; some tracks may be missing",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gaps, _ := trackNumberMessages(c.tracks...)
			if len(gaps) != 1 || gaps[0] != c.want {
				t.Fatalf("got %q, want [%q]", gaps, c.want)
			}
		})
	}
}

// An untagged file can be the track the numbers skip; its own "missing track
// number" issue is the report, not a claim that a file is absent.
func TestTrackNumberGapNotReportedWhenUnnumberedFileCouldFillIt(t *testing.T) {
	scan := &Scan{Root: `C:\m`, Tracks: []Track{
		gapTrack("01.flac", 1, 0), gapTrack("02.flac", 2, 0), gapTrack("03 Song.flac", 0, 0), gapTrack("04.flac", 4, 0),
	}}
	var sawMissingNumber bool
	for _, issue := range trackNumberRule.Detect(scan, ProfileByID(ProfilePoweramp)) {
		if strings.Contains(issue.Message, "may be missing") {
			t.Fatalf("unnumbered file explains the gap: %+v", issue)
		}
		if issue.Message == "missing track number" {
			sawMissingNumber = true
		}
	}
	if !sawMissingNumber {
		t.Fatal("the untagged file must still be reported")
	}
}

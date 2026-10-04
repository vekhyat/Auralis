package library

import (
	"sort"
	"strings"
)

// --- Mixed formats / bit depths (report only) ---------------------------------

var mixedFormatRule = Rule{
	ID:       "MIXEDFORMAT",
	Severity: SeverityInfo,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		for dir, tracks := range Albums(scan) {
			formats := map[string]bool{}
			depths := map[int]bool{}
			for _, t := range tracks {
				formats[t.Format] = true
				if t.BitDepth > 0 {
					depths[t.BitDepth] = true
				}
			}
			if len(formats) > 1 {
				var values []string
				for f := range formats {
					values = append(values, f)
				}
				sort.Strings(values)
				issues = append(issues, Issue{RuleID: "MIXEDFORMAT", Path: dir, AlbumDir: dir, Message: "album mixes formats: " + strings.Join(values, ", "), Fixable: false})
			}
			if len(depths) > 1 {
				issues = append(issues, Issue{RuleID: "MIXEDFORMAT", Path: dir, AlbumDir: dir, Message: "album mixes bit depths", Fixable: false})
			}
		}
		return issues
	},
}

// --- Likely duplicates (report only, never auto-delete) ------------------------

var duplicateRule = Rule{
	ID:       "DUPLICATES",
	Severity: SeverityInfo,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		byISRC := map[string][]string{}
		type key struct{ artist, title string }
		byTag := map[key][]*Track{}
		for i := range scan.Tracks {
			t := &scan.Tracks[i]
			if isrc := strings.ToUpper(strings.TrimSpace(t.ISRC)); isrc != "" {
				byISRC[isrc] = append(byISRC[isrc], t.Path)
			}
			k := key{
				artist: strings.ToLower(strings.TrimSpace(t.Artist)),
				title:  strings.ToLower(strings.TrimSpace(t.Title)),
			}
			if k.artist != "" && k.title != "" {
				byTag[k] = append(byTag[k], t)
			}
		}
		for isrc, paths := range byISRC {
			if len(paths) < 2 {
				continue
			}
			sort.Strings(paths)
			for _, p := range paths {
				issues = append(issues, Issue{RuleID: "DUPLICATES", Path: p, AlbumDir: TrackByPath(scan, p).AlbumDir(), Message: "same ISRC " + isrc + " also at " + otherPath(paths, p), Fixable: false})
			}
		}
		for _, tracks := range byTag {
			if len(tracks) < 2 {
				continue
			}
			for i := 0; i < len(tracks); i++ {
				for j := i + 1; j < len(tracks); j++ {
					a, b := tracks[i], tracks[j]
					if a.DurationSeconds <= 0 || b.DurationSeconds <= 0 {
						continue
					}
					diff := a.DurationSeconds - b.DurationSeconds
					if diff < 0 {
						diff = -diff
					}
					if diff <= 2.0 {
						issues = append(issues, Issue{
							RuleID:   "DUPLICATES",
							Path:     b.Path,
							AlbumDir: b.AlbumDir(),
							Message:  "same artist/title and duration within ±2s of " + a.Path,
							Fixable:  false,
						})
					}
				}
			}
		}
		return issues
	},
}

func otherPath(paths []string, self string) string {
	for _, p := range paths {
		if p != self {
			return p
		}
	}
	return ""
}

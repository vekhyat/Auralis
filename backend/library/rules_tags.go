package library

import (
	"path"
	"sort"
	"strconv"
	"strings"
)

// --- ALBUMARTIST consistency ------------------------------------------------

var albumArtistRule = Rule{
	ID:       "ALBUMARTIST",
	Severity: SeverityError,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		for dir, tracks := range Albums(scan) {
			seen := map[string]int{}
			for _, t := range tracks {
				if strings.TrimSpace(t.AlbumArtist) != "" {
					seen[strings.TrimSpace(t.AlbumArtist)]++
				}
			}
			if len(seen) == 0 {
				issues = append(issues, Issue{
					RuleID:   "ALBUMARTIST",
					Path:     dir,
					AlbumDir: dir,
					Message:  "album has no ALBUMARTIST on any track",
					Fixable:  true,
				})
				continue
			}
			if len(seen) > 1 {
				var values []string
				for v := range seen {
					values = append(values, v)
				}
				issues = append(issues, Issue{
					RuleID:   "ALBUMARTIST",
					Path:     dir,
					AlbumDir: dir,
					Message:  "inconsistent ALBUMARTIST values: " + strings.Join(values, ", "),
					Fixable:  true,
				})
				continue
			}
			// One value, but check each track actually carries it.
			for _, t := range tracks {
				if strings.TrimSpace(t.AlbumArtist) == "" {
					issues = append(issues, Issue{
						RuleID:   "ALBUMARTIST",
						Path:     t.Path,
						AlbumDir: dir,
						Message:  "track is missing ALBUMARTIST",
						Fixable:  true,
					})
				}
			}
		}
		return issues
	},
	Fix: func(scan *Scan, profile Profile, issues []Issue) []Operation {
		var ops []Operation
		for _, issue := range issues {
			tracks := Albums(scan)[issue.AlbumDir]
			target := mostCommon(groupDistinct(albumArtistValues(tracks)))
			if target == "" {
				target = mostCommon(groupDistinct(artistValues(tracks)))
			}
			if target == "" {
				target = "Unknown Artist"
			}
			for _, t := range tracks {
				if strings.TrimSpace(t.AlbumArtist) == target {
					continue
				}
				ops = append(ops, tagOp(t, map[string]string{"ALBUMARTIST": target}, "set consistent ALBUMARTIST"))
			}
		}
		return ops
	},
}

func albumArtistValues(tracks []*Track) []string {
	out := []string{}
	for _, t := range tracks {
		out = append(out, t.AlbumArtist)
	}
	return out
}

func artistValues(tracks []*Track) []string {
	out := []string{}
	for _, t := range tracks {
		out = append(out, t.Artist)
	}
	return out
}

// --- Compilation -----------------------------------------------------------

var compilationRule = Rule{
	ID:       "COMPILATION",
	Severity: SeverityWarning,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		for dir, tracks := range Albums(scan) {
			artists := map[string]bool{}
			for _, t := range tracks {
				if a := strings.ToLower(strings.TrimSpace(t.Artist)); a != "" {
					artists[a] = true
				}
			}
			if len(artists) < 3 {
				continue
			}
			albumArtist := strings.ToLower(mostCommon(groupDistinct(albumArtistValues(tracks))))
			marked := false
			for _, t := range tracks {
				if t.Compilation {
					marked = true
				}
			}
			if albumArtist == "various artists" || marked {
				continue
			}
			issues = append(issues, Issue{
				RuleID:   "COMPILATION",
				Path:     dir,
				AlbumDir: dir,
				Message:  "multiple artists, one folder, but no Various Artists/COMPILATION tag",
				Fixable:  true,
			})
		}
		return issues
	},
	Fix: func(scan *Scan, profile Profile, issues []Issue) []Operation {
		var ops []Operation
		for _, issue := range issues {
			for _, t := range Albums(scan)[issue.AlbumDir] {
				ops = append(ops, tagOp(t, map[string]string{
					"ALBUMARTIST": "Various Artists",
					"COMPILATION": "1",
				}, "mark album as compilation"))
			}
		}
		return ops
	},
}

// --- Track numbers ----------------------------------------------------------

var trackNumberRule = Rule{
	ID:       "TRACKNUMBER",
	Severity: SeverityWarning,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		for dir, tracks := range Albums(scan) {
			perDisc := map[int][]*Track{}
			for _, t := range tracks {
				d := t.DiscNumber
				if d == 0 {
					d = 1
				}
				perDisc[d] = append(perDisc[d], t)
			}
			for disc, discTracks := range perDisc {
				numbers := map[int]bool{}
				for _, t := range discTracks {
					if t.TrackNumberRaw != "" && t.TrackNumber == 0 {
						issues = append(issues, Issue{RuleID: "TRACKNUMBER", Path: t.Path, AlbumDir: dir, Message: "unparseable track number \"" + t.TrackNumberRaw + "\"", Fixable: true})
					}
					if t.TrackNumber == 0 {
						issues = append(issues, Issue{RuleID: "TRACKNUMBER", Path: t.Path, AlbumDir: dir, Message: "missing track number", Fixable: true})
						continue
					}
					numbers[t.TrackNumber] = true
					if t.TrackTotal != 0 && t.TrackTotal != len(discTracks) {
						issues = append(issues, Issue{RuleID: "TRACKNUMBER", Path: t.Path, AlbumDir: dir, Message: "track total " + strconv.Itoa(t.TrackTotal) + " does not match " + strconv.Itoa(len(discTracks)) + " tracks", Fixable: true})
					}
				}
				max := 0
				for n := range numbers {
					if n > max {
						max = n
					}
				}
				if max > len(numbers) {
					issues = append(issues, Issue{RuleID: "TRACKNUMBER", Path: dir, AlbumDir: dir, Message: "gap in track numbering on disc " + strconv.Itoa(disc), Fixable: true})
				}
			}
		}
		return dedupeIssues(issues)
	},
	Fix: func(scan *Scan, profile Profile, issues []Issue) []Operation {
		albums := Albums(scan)
		seen := map[string]bool{}
		var ops []Operation
		for _, issue := range issues {
			if seen[issue.AlbumDir] {
				continue
			}
			seen[issue.AlbumDir] = true
			tracks := append([]*Track{}, albums[issue.AlbumDir]...)
			sort.Slice(tracks, func(i, j int) bool {
				a, b := tracks[i], tracks[j]
				if a.DiscNumber != b.DiscNumber {
					return a.DiscNumber < b.DiscNumber
				}
				if a.TrackNumber != b.TrackNumber {
					if a.TrackNumber == 0 {
						return false
					}
					if b.TrackNumber == 0 {
						return true
					}
					return a.TrackNumber < b.TrackNumber
				}
				return a.RelPath < b.RelPath
			})
			// Group per disc so totals are per-disc counts.
			perDisc := map[int][]*Track{}
			for _, t := range tracks {
				d := t.DiscNumber
				if d == 0 {
					d = 1
				}
				perDisc[d] = append(perDisc[d], t)
			}
			for _, discTracks := range perDisc {
				for i, t := range discTracks {
					num := i + 1
					total := len(discTracks)
					if t.TrackNumber == num && t.TrackTotal == total {
						continue
					}
					ops = append(ops, tagOp(t, trackSet(t.Format, num, total), "normalise track number + total"))
				}
			}
		}
		return ops
	},
}

var discNumberRule = Rule{
	ID:       "DISCNUMBER",
	Severity: SeverityWarning,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		for dir, tracks := range Albums(scan) {
			hasSet, hasMissing, maxDisc := false, false, 0
			for _, t := range tracks {
				if t.DiscNumber > 0 {
					hasSet = true
					if t.DiscNumber > maxDisc {
						maxDisc = t.DiscNumber
					}
				} else {
					hasMissing = true
				}
			}
			if hasMissing && hasSet {
				issues = append(issues, Issue{RuleID: "DISCNUMBER", Path: dir, AlbumDir: dir, Message: "disc numbers are inconsistent across the album", Fixable: true})
			} else if hasMissing && maxDisc == 0 && looksMultiDisc(dir) {
				issues = append(issues, Issue{RuleID: "DISCNUMBER", Path: dir, AlbumDir: dir, Message: "missing disc numbers", Fixable: true})
			}
		}
		return dedupeIssues(issues)
	},
	Fix: func(scan *Scan, profile Profile, issues []Issue) []Operation {
		albums := Albums(scan)
		seen := map[string]bool{}
		var ops []Operation
		for _, issue := range issues {
			if seen[issue.AlbumDir] {
				continue
			}
			seen[issue.AlbumDir] = true
			tracks := albums[issue.AlbumDir]
			maxDisc := 1
			for _, t := range tracks {
				if t.DiscNumber > maxDisc {
					maxDisc = t.DiscNumber
				}
			}
			for _, t := range tracks {
				if t.DiscNumber != 0 && t.DiscTotal == maxDisc {
					continue
				}
				disc := t.DiscNumber
				if disc == 0 {
					disc = 1
				}
				ops = append(ops, tagOp(t, discSet(t.Format, disc, maxDisc), "normalise disc number"))
			}
		}
		return ops
	},
}

func looksMultiDisc(dir string) bool {
	lower := strings.ToLower(path.Base(dir))
	return strings.Contains(lower, "disc") || strings.Contains(lower, "cd") || strings.Contains(lower, "disk")
}

func dedupeIssues(issues []Issue) []Issue {
	seen := map[string]bool{}
	out := issues[:0]
	for _, issue := range issues {
		key := issue.Path + "|" + issue.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, issue)
	}
	return out
}

func tagOp(t *Track, set map[string]string, reason string) Operation {
	return Operation{
		Type: "tags", Path: t.Path, Set: set, Reason: reason,
		Size: t.Size, ModTime: t.ModTimeUnixNano,
	}
}

// trackSet returns the container-appropriate tag overlay for a track number.
func trackSet(format string, number, total int) map[string]string {
	switch format {
	case "flac", "ogg":
		m := map[string]string{"TRACKNUMBER": strconv.Itoa(number)}
		if total > 0 {
			m["TRACKTOTAL"] = strconv.Itoa(total)
			m["TOTALTRACKS"] = strconv.Itoa(total)
		}
		return m
	default:
		if total > 0 {
			return map[string]string{"TRACKNUMBER": strconv.Itoa(number) + "/" + strconv.Itoa(total)}
		}
		return map[string]string{"TRACKNUMBER": strconv.Itoa(number)}
	}
}

func discSet(format string, number, total int) map[string]string {
	switch format {
	case "flac", "ogg":
		m := map[string]string{"DISCNUMBER": strconv.Itoa(number)}
		if total > 0 {
			m["DISCTOTAL"] = strconv.Itoa(total)
			m["TOTALDISCS"] = strconv.Itoa(total)
		}
		return m
	default:
		if total > 0 {
			return map[string]string{"DISCNUMBER": strconv.Itoa(number) + "/" + strconv.Itoa(total)}
		}
		return map[string]string{"DISCNUMBER": strconv.Itoa(number)}
	}
}

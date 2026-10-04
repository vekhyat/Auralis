package library

import (
	"path"
	"regexp"
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
			if !singleAlbum(dir, tracks) {
				continue
			}
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
			if !singleAlbum(dir, tracks) {
				continue
			}
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

// Gaps and track totals larger than the files present usually mean the album
// is incomplete, not that the numbers are wrong, so those are reported only.
// Fixes never renumber a track that already has a number.

var trackNumberRule = Rule{
	ID:       "TRACKNUMBER",
	Severity: SeverityWarning,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		for dir, tracks := range Albums(scan) {
			if !singleAlbum(dir, tracks) {
				continue
			}
			for disc, discTracks := range tracksPerDisc(tracks) {
				seen := map[int]string{}
				maxNum := 0
				for _, t := range discTracks {
					switch {
					case t.TrackNumber == 0 && t.TrackNumberRaw != "":
						issues = append(issues, Issue{Path: t.Path, AlbumDir: dir, Message: "unparseable track number \"" + t.TrackNumberRaw + "\"", Fixable: filenameTrackNumber(t) > 0})
						continue
					case t.TrackNumber == 0:
						issues = append(issues, Issue{Path: t.Path, AlbumDir: dir, Message: "missing track number", Fixable: filenameTrackNumber(t) > 0})
						continue
					}
					if other, dup := seen[t.TrackNumber]; dup {
						issues = append(issues, Issue{Path: t.Path, AlbumDir: dir, Message: "track number " + strconv.Itoa(t.TrackNumber) + " is also used by " + other})
					}
					seen[t.TrackNumber] = t.RelPath
					if t.TrackNumber > maxNum {
						maxNum = t.TrackNumber
					}
					if t.Format == "flac" && strings.Contains(t.TrackNumberRaw, "/") {
						issues = append(issues, Issue{Path: t.Path, AlbumDir: dir, Message: "track number stored as \"" + t.TrackNumberRaw + "\" instead of separate TRACKNUMBER/TRACKTOTAL", Fixable: true})
					}
				}
				highest := highestTrackNumber(discTracks)
				for _, t := range discTracks {
					if t.TrackNumber > 0 && t.TrackTotal > 0 && t.TrackTotal < highest {
						issues = append(issues, Issue{Path: t.Path, AlbumDir: dir, Message: "track total " + strconv.Itoa(t.TrackTotal) + " is lower than track number " + strconv.Itoa(highest), Fixable: true})
					}
				}
				if maxNum > len(seen) {
					issues = append(issues, Issue{Path: dir, AlbumDir: dir, Message: "disc " + strconv.Itoa(disc) + " has " + strconv.Itoa(len(seen)) + " numbered tracks up to " + strconv.Itoa(maxNum) + "; some tracks may be missing"})
				}
			}
		}
		return issues
	},
	Fix: func(scan *Scan, profile Profile, issues []Issue) []Operation {
		albums := Albums(scan)
		done := map[string]bool{}
		var ops []Operation
		for _, issue := range issues {
			t := TrackByPath(scan, issue.Path)
			if t == nil || done[t.Path] {
				continue
			}
			done[t.Path] = true
			num := effectiveTrackNumber(t)
			if num == 0 {
				continue
			}
			disc := t.DiscNumber
			if disc == 0 {
				disc = 1
			}
			total := t.TrackTotal
			if highest := highestTrackNumber(tracksPerDisc(albums[issue.AlbumDir])[disc]); total < highest {
				total = highest
			}
			ops = append(ops, tagOp(t, trackSet(t.Format, num, total), "normalise track number and total"))
		}
		return ops
	},
}

// effectiveTrackNumber is the tagged number, or the one in the file name.
func effectiveTrackNumber(t *Track) int {
	if t.TrackNumber > 0 {
		return t.TrackNumber
	}
	return filenameTrackNumber(t)
}

func highestTrackNumber(tracks []*Track) int {
	highest := 0
	for _, t := range tracks {
		if n := effectiveTrackNumber(t); n > highest {
			highest = n
		}
	}
	return highest
}

func tracksPerDisc(tracks []*Track) map[int][]*Track {
	perDisc := map[int][]*Track{}
	for _, t := range tracks {
		d := t.DiscNumber
		if d == 0 {
			d = 1
		}
		perDisc[d] = append(perDisc[d], t)
	}
	return perDisc
}

// filenameTrackPattern matches "03 Title", "03. Title", "03 - Title" and the
// profile layout "2-03. Title".
var filenameTrackPattern = regexp.MustCompile(`^(?:\d{1,2}-)?(\d{1,3})(?:[ ._)-]|$)`)

// filenameTrackNumber recovers a track number from the file name, or 0.
func filenameTrackNumber(t *Track) int {
	base := strings.TrimSuffix(path.Base(t.RelPath), path.Ext(t.RelPath))
	m := filenameTrackPattern.FindStringSubmatch(base)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

var discNumberRule = Rule{
	ID:       "DISCNUMBER",
	Severity: SeverityWarning,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		for dir, tracks := range Albums(scan) {
			if !singleAlbum(dir, tracks) {
				continue
			}
			hasSet, hasMissing := false, false
			for _, t := range tracks {
				if t.DiscNumber > 0 {
					hasSet = true
				} else {
					hasMissing = true
				}
			}
			if hasMissing && hasSet {
				issues = append(issues, Issue{Path: dir, AlbumDir: dir, Message: "disc numbers are inconsistent across the album", Fixable: true})
			} else if hasMissing && looksMultiDisc(dir) {
				issues = append(issues, Issue{Path: dir, AlbumDir: dir, Message: "folder looks like one disc of a set but has no disc numbers"})
			}
		}
		return issues
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

var multiDiscPattern = regexp.MustCompile(`(?i)\b(?:disc|disk|cd)\s*\d`)

// looksMultiDisc matches folder names such as "CD1" or "Disc 2", but not
// names that merely contain the letters, such as "ACDC".
func looksMultiDisc(dir string) bool {
	return multiDiscPattern.MatchString(path.Base(dir))
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

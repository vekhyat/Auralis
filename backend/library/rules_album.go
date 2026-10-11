package library

import (
	"sort"
	"strconv"
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

// duplicateListCap bounds how many other copies one DUPLICATES message names.
// The rest are summarized as "(+N more)" so a message, and the issue ID built
// from it, stays short and does not change as unrelated copies are added.
const duplicateListCap = 3

// duplicateDurationWindow is the largest duration gap, in seconds, at which
// two files with the same artist and title still count as the same recording.
const duplicateDurationWindow = 2.0

// formatOtherPaths renders up to duplicateListCap paths (already ascending)
// followed by a "(+N more)" tail when total counts more than were listed.
func formatOtherPaths(listed []string, total int) string {
	out := strings.Join(listed, ", ")
	if more := total - len(listed); more > 0 {
		out += " (+" + strconv.Itoa(more) + " more)"
	}
	return out
}

// pathSummary keeps the duplicateListCap smallest paths seen so far and the
// total number of paths added, without storing the rest.
type pathSummary struct {
	listed []string
	total  int
}

func (s *pathSummary) add(path string) {
	s.total++
	i := sort.SearchStrings(s.listed, path)
	if i >= duplicateListCap {
		return
	}
	s.listed = append(s.listed, "")
	copy(s.listed[i+1:], s.listed[i:])
	s.listed[i] = path
	if len(s.listed) > duplicateListCap {
		s.listed = s.listed[:duplicateListCap]
	}
}

func isrcKey(t *Track) string {
	return strings.ToUpper(strings.TrimSpace(t.ISRC))
}

var duplicateRule = Rule{
	ID:       "DUPLICATES",
	Severity: SeverityInfo,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		type tagKey struct{ artist, title string }
		byISRC := map[string][]*Track{}
		byTag := map[tagKey][]*Track{}
		for i := range scan.Tracks {
			t := &scan.Tracks[i]
			if isrc := isrcKey(t); isrc != "" {
				byISRC[isrc] = append(byISRC[isrc], t)
			}
			k := tagKey{
				artist: strings.ToLower(strings.TrimSpace(t.Artist)),
				title:  strings.ToLower(strings.TrimSpace(t.Title)),
			}
			if k.artist != "" && k.title != "" {
				byTag[k] = append(byTag[k], t)
			}
		}

		// Same ISRC: one issue per file naming the other copies. Groups and
		// files are visited in sorted order so output and messages are stable.
		isrcs := make([]string, 0, len(byISRC))
		for isrc := range byISRC {
			isrcs = append(isrcs, isrc)
		}
		sort.Strings(isrcs)
		for _, isrc := range isrcs {
			group := byISRC[isrc]
			if len(group) < 2 {
				continue
			}
			sort.Slice(group, func(i, j int) bool { return group[i].Path < group[j].Path })
			// Unique, ascending paths of the whole group.
			paths := make([]string, 0, len(group))
			for _, t := range group {
				if len(paths) == 0 || paths[len(paths)-1] != t.Path {
					paths = append(paths, t.Path)
				}
			}
			if len(paths) < 2 {
				continue
			}
			for _, t := range group {
				listed := make([]string, 0, duplicateListCap)
				for _, p := range paths {
					if p == t.Path {
						continue
					}
					listed = append(listed, p)
					if len(listed) == duplicateListCap {
						break
					}
				}
				issues = append(issues, Issue{
					RuleID:   "DUPLICATES",
					Path:     t.Path,
					AlbumDir: t.AlbumDir(),
					Message:  "same ISRC " + isrc + " also at " + formatOtherPaths(listed, len(paths)-1),
					Fixable:  false,
				})
			}
		}

		// Same artist/title with a close duration: one issue per file naming
		// the other matches, not one per pair. Durations are sorted so each
		// file's matches are a contiguous window instead of a pairwise scan.
		// Matches that share the file's ISRC are left out: the branch above
		// already reports that set.
		tagKeys := make([]tagKey, 0, len(byTag))
		for k, group := range byTag {
			if len(group) >= 2 {
				tagKeys = append(tagKeys, k)
			}
		}
		sort.Slice(tagKeys, func(i, j int) bool {
			if tagKeys[i].artist != tagKeys[j].artist {
				return tagKeys[i].artist < tagKeys[j].artist
			}
			return tagKeys[i].title < tagKeys[j].title
		})
		for _, k := range tagKeys {
			var timed []*Track
			for _, t := range byTag[k] {
				if t.DurationSeconds > 0 {
					timed = append(timed, t)
				}
			}
			if len(timed) < 2 {
				continue
			}
			sort.Slice(timed, func(i, j int) bool {
				if timed[i].DurationSeconds != timed[j].DurationSeconds {
					return timed[i].DurationSeconds < timed[j].DurationSeconds
				}
				return timed[i].Path < timed[j].Path
			})
			// Matches of timed[i] are timed[lo:hi], self included.
			lo, hi := 0, 0
			var perFile []Issue
			for i, t := range timed {
				for t.DurationSeconds-timed[lo].DurationSeconds > duplicateDurationWindow {
					lo++
				}
				if hi < i+1 {
					hi = i + 1
				}
				for hi < len(timed) && timed[hi].DurationSeconds-t.DurationSeconds <= duplicateDurationWindow {
					hi++
				}
				if hi-lo < 2 {
					continue
				}
				own := isrcKey(t)
				var others pathSummary
				for _, o := range timed[lo:hi] {
					if o == t || o.Path == t.Path {
						continue
					}
					if own != "" && isrcKey(o) == own {
						continue
					}
					others.add(o.Path)
				}
				if others.total == 0 {
					continue
				}
				perFile = append(perFile, Issue{
					RuleID:   "DUPLICATES",
					Path:     t.Path,
					AlbumDir: t.AlbumDir(),
					Message:  "same artist/title and duration within ±2s of " + formatOtherPaths(others.listed, others.total),
					Fixable:  false,
				})
			}
			// The window walk is in duration order; report in path order.
			sort.Slice(perFile, func(i, j int) bool { return perFile[i].Path < perFile[j].Path })
			issues = append(issues, perFile...)
		}
		return issues
	},
}

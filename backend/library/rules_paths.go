package library

import (
	"path"
	"path/filepath"
	"strings"
)

// --- Path legality & template conformance -------------------------------------

var pathRule = Rule{
	ID:       "PATH",
	Severity: SeverityError,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		for i := range scan.Tracks {
			t := &scan.Tracks[i]
			if !pathIsLegal(profile, t.RelPath) {
				issues = append(issues, Issue{
					RuleID:   "PATH",
					Path:     t.Path,
					AlbumDir: t.AlbumDir(),
					Message:  "path contains characters illegal for this profile",
					Fixable:  true,
				})
				continue
			}
			expected := expectedRelPath(profile, t)
			if !strings.EqualFold(expected, strings.TrimSuffix(t.RelPath, filepath.Ext(t.RelPath))) {
				issues = append(issues, Issue{
					RuleID:   "PATH",
					Path:     t.Path,
					AlbumDir: t.AlbumDir(),
					Message:  "path does not follow the profile template",
					Fixable:  true,
				})
			}
		}
		return issues
	},
	Fix: func(scan *Scan, profile Profile, issues []Issue) []Operation {
		var ops []Operation
		for _, issue := range issues {
			t := TrackByPath(scan, issue.Path)
			if t == nil {
				continue
			}
			newRel := expectedRelPath(profile, t) + strings.ToLower(filepath.Ext(t.RelPath))
			if !pathIsLegal(profile, newRel) {
				newRel = profile.SanitizeRelativePath(newRel)
			}
			oldNoExt := strings.TrimSuffix(t.RelPath, filepath.Ext(t.RelPath))
			newNoExt := strings.TrimSuffix(newRel, strings.ToLower(filepath.Ext(t.RelPath)))
			if strings.EqualFold(newNoExt, oldNoExt) {
				// Only illegal characters differ from the template.
				sanitized := profile.SanitizeRelativePath(t.RelPath)
				if strings.EqualFold(sanitized, t.RelPath) {
					continue
				}
				newRel = sanitized
			}
			dest := filepath.Join(scan.Root, filepath.FromSlash(newRel))
			if dest == t.Path {
				continue
			}
			ops = append(ops, Operation{
				Type: "move", Path: t.Path, NewPath: dest, Reason: "move to profile template path",
				Size: t.Size, ModTime: t.ModTimeUnixNano,
			})
		}
		return ops
	},
}

func pathIsLegal(profile Profile, rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if profile.SanitizeComponent(seg) != seg {
			return false
		}
	}
	return true
}

func expectedRelPath(profile Profile, t *Track) string {
	fields := TrackFields{
		Title: t.Title, Artist: t.Artist, AlbumArtist: t.AlbumArtist,
		Album: t.Album, Year: year4(t.Year),
		TrackNumber: t.TrackNumber, DiscNumber: t.DiscNumber, DiscTotal: t.DiscTotal,
	}
	return profile.RelativePath(fields)
}

func year4(year string) string {
	year = strings.TrimSpace(year)
	if len(year) >= 4 {
		return year[:4]
	}
	return year
}

var _ = path.Join

package library

import (
	"strconv"
)

// --- Embedded / folder cover -------------------------------------------------

const maxEmbeddedCoverBytes = 1.5 * 1024 * 1024

var coverRule = Rule{
	ID:       "COVER",
	Severity: SeverityWarning,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		folderWarned := map[string]bool{}
		for _, t := range scan.Tracks {
			if t.CoverBytes == 0 {
				issues = append(issues, Issue{RuleID: "COVER", Path: t.Path, AlbumDir: t.AlbumDir(), Message: "no embedded cover", Fixable: false})
			} else {
				if profile.MaxCoverPx > 0 && (t.CoverWidth > profile.MaxCoverPx || t.CoverHeight > profile.MaxCoverPx) {
					issues = append(issues, Issue{RuleID: "COVER", Path: t.Path, AlbumDir: t.AlbumDir(), Message: "embedded cover exceeds " + itoa(profile.MaxCoverPx) + "px", Fixable: false})
				}
				if t.CoverBytes > maxEmbeddedCoverBytes {
					issues = append(issues, Issue{RuleID: "COVER", Path: t.Path, AlbumDir: t.AlbumDir(), Message: "embedded cover is larger than 1.5 MB", Fixable: false})
				}
			}
			dir := t.AlbumDir()
			if profile.FolderCover && !folderWarned[dir] && !t.FolderCover {
				folderWarned[dir] = true
				issues = append(issues, Issue{RuleID: "COVER", Path: dir, AlbumDir: dir, Message: "profile expects a folder cover.jpg but none exists", Fixable: false})
			}
		}
		return issues
	},
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

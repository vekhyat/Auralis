package library

import (
	"os"
	"path"
	"path/filepath"
	"sort"
)

// --- Orphans -------------------------------------------------------------------

var orphanRule = Rule{
	ID:       "ORPHAN",
	Severity: SeverityInfo,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue

		// Directories that contain no audio anywhere below them.
		audioDirs := map[string]bool{}
		for i := range scan.Tracks {
			dir := scan.Tracks[i].AlbumDir()
			audioDirs[dir] = true
			for parent := path.Dir(dir); parent != "." && parent != "/" && parent != ""; parent = path.Dir(parent) {
				audioDirs[parent] = true
			}
		}
		for _, dir := range scan.Dirs {
			if audioDirs[dir] {
				continue
			}
			if dirIsEmpty(filepath.Join(scan.Root, filepath.FromSlash(dir))) {
				issues = append(issues, Issue{
					RuleID:   "ORPHAN",
					Path:     filepath.Join(scan.Root, filepath.FromSlash(dir)),
					AlbumDir: dir,
					Message:  "empty folder",
					Fixable:  true,
				})
			}
		}

		for _, stray := range scan.StrayFiles {
			issues = append(issues, Issue{
				RuleID:   "ORPHAN",
				Path:     filepath.Join(scan.Root, filepath.FromSlash(stray)),
				AlbumDir: path.Dir(stray),
				Message:  "stray artwork/lyrics file with no audio in the folder",
				Fixable:  false,
			})
		}

		for i := range scan.Tracks {
			t := &scan.Tracks[i]
			if t.Size == 0 {
				issues = append(issues, Issue{
					RuleID:   "ORPHAN",
					Path:     t.Path,
					AlbumDir: t.AlbumDir(),
					Message:  "zero-byte audio file",
					Fixable:  false,
				})
			}
		}
		sort.Slice(issues, func(i, j int) bool { return issues[i].Path < issues[j].Path })
		return issues
	},
	Fix: func(scan *Scan, profile Profile, issues []Issue) []Operation {
		var ops []Operation
		for _, issue := range issues {
			if !issue.Fixable {
				continue
			}
			ops = append(ops, Operation{Type: "rmdir", Path: issue.Path, Reason: "remove empty folder"})
		}
		return ops
	},
}

func dirIsEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) == 0
}

package library

// --- Unreadable files (report only) --------------------------------------------

// unreadableRule reports files taglib could not read. Every other rule skips
// those tracks: with no tags they would otherwise raise a pile of unrelated
// issues (no cover, no album artist, ...) that all share one root cause.
//
// A zero-byte file is the exception: the ORPHAN rule already reports it as an
// empty audio file, which says more than "damaged", so it is not reported twice.
var unreadableRule = Rule{
	ID:       "UNREADABLE",
	Severity: SeverityError,
	Detect: func(scan *Scan, profile Profile) []Issue {
		var issues []Issue
		for i := range scan.Tracks {
			t := &scan.Tracks[i]
			if !t.Unreadable || t.Size == 0 {
				continue
			}
			issues = append(issues, Issue{
				RuleID:   "UNREADABLE",
				Path:     t.Path,
				AlbumDir: t.AlbumDir(),
				Message:  "tags could not be read: file may be damaged",
				Fixable:  false,
			})
		}
		return issues
	},
}

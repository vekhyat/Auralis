package library

import "testing"

func TestUnreadableRuleSkipsZeroByteFiles(t *testing.T) {
	scan := &Scan{Root: `C:\m`, Tracks: []Track{
		{Path: `C:\m\A\L\damaged.flac`, RelPath: "A/L/damaged.flac", Format: "flac", Unreadable: true, Size: 4096},
		{Path: `C:\m\A\L\empty.flac`, RelPath: "A/L/empty.flac", Format: "flac", Unreadable: true, Size: 0},
		{Path: `C:\m\A\L\fine.flac`, RelPath: "A/L/fine.flac", Format: "flac", Size: 4096},
	}}
	issues := unreadableRule.Detect(scan, ProfileByID(ProfilePoweramp))
	if len(issues) != 1 || issues[0].Path != `C:\m\A\L\damaged.flac` {
		t.Fatalf("UNREADABLE must name only the non-empty damaged file: %+v", issues)
	}
}

// A zero-byte file shows up exactly once across all rules: as an empty audio
// file, which is the more specific diagnosis.
func TestZeroByteFileIsReportedOnceAcrossRules(t *testing.T) {
	scan := &Scan{Root: `C:\m`, Tracks: []Track{
		{Path: `C:\m\A\L\empty.flac`, RelPath: "A/L/empty.flac", Format: "flac", Unreadable: true, Size: 0},
	}}
	var ids []string
	for _, issue := range DetectAll(scan, ProfileByID(ProfilePoweramp)) {
		ids = append(ids, issue.RuleID+": "+issue.Message)
	}
	if len(ids) != 1 || ids[0] != "ORPHAN: zero-byte audio file" {
		t.Fatalf("issues: %q", ids)
	}
}

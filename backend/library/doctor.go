package library

import (
	"sort"
	"strings"

	"go.senan.xyz/taglib"
)

// Severity classifies how loudly a rule should be reported.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Issue is one detected problem. It never mutates the library.
type Issue struct {
	ID       string   `json:"id"`
	RuleID   string   `json:"rule_id"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	AlbumDir string   `json:"album_dir"`
	Message  string   `json:"message"`
	Fixable  bool     `json:"fixable"`
}

// Rule is one Library Doctor check. Fix returns the operations that resolve
// the given issues of that rule; report-only rules return nil.
type Rule struct {
	ID       string
	Severity Severity
	Detect   func(scan *Scan, profile Profile) []Issue
	Fix      func(scan *Scan, profile Profile, issues []Issue) []Operation
}

var rules = []Rule{
	albumArtistRule,
	compilationRule,
	trackNumberRule,
	discNumberRule,
	coverRule,
	pathRule,
	mixedFormatRule,
	duplicateRule,
	orphanRule,
}

// Rules returns the registered rule IDs in display order.
func Rules() []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.ID)
	}
	return out
}

// RuleByID returns a rule or nil.
func RuleByID(id string) *Rule {
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i]
		}
	}
	return nil
}

// DetectAll runs every rule over the scan and returns all issues, sorted.
func DetectAll(scan *Scan, profile Profile) []Issue {
	var issues []Issue
	for _, rule := range rules {
		detected := rule.Detect(scan, profile)
		for i := range detected {
			if detected[i].RuleID == "" {
				detected[i].RuleID = rule.ID
			}
			if detected[i].Severity == "" {
				detected[i].Severity = rule.Severity
			}
			if detected[i].Fixable && rule.Fix == nil {
				detected[i].Fixable = false
			}
			if detected[i].ID == "" {
				// Several issues can share a rule and path (e.g. a missing
				// cover and an oversized one), so the message is part of the ID.
				detected[i].ID = rule.ID + "|" + detected[i].Path + "|" + detected[i].Message
			}
		}
		issues = append(issues, detected...)
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].RuleID != issues[j].RuleID {
			return issues[i].RuleID < issues[j].RuleID
		}
		return issues[i].ID < issues[j].ID
	})
	return dedupeIssueIDs(issues)
}

func dedupeIssueIDs(issues []Issue) []Issue {
	seen := make(map[string]bool, len(issues))
	out := issues[:0]
	for _, issue := range issues {
		if seen[issue.ID] {
			continue
		}
		seen[issue.ID] = true
		out = append(out, issue)
	}
	return out
}

// Fixable issues of the given rules produce a Plan. Never mutates anything.
func BuildPlan(scan *Scan, profile Profile, issues []Issue) *Plan {
	byRule := map[string][]Issue{}
	for _, issue := range issues {
		if !issue.Fixable {
			continue
		}
		byRule[issue.RuleID] = append(byRule[issue.RuleID], issue)
	}
	plan := &Plan{}
	seen := map[string]bool{}
	// Sort so a plan does not depend on map iteration order.
	ruleIDs := make([]string, 0, len(byRule))
	for ruleID := range byRule {
		ruleIDs = append(ruleIDs, ruleID)
	}
	sort.Strings(ruleIDs)
	for _, ruleID := range ruleIDs {
		rule := RuleByID(ruleID)
		if rule == nil || rule.Fix == nil {
			continue
		}
		for _, op := range rule.Fix(scan, profile, byRule[ruleID]) {
			key := operationKey(op)
			if seen[key] {
				continue
			}
			seen[key] = true
			plan.Operations = append(plan.Operations, op)
		}
	}
	// Read the current tag values so the preview shows a real before/after
	// diff. A file that cannot be read simply has no old value shown.
	for i := range plan.Operations {
		op := &plan.Operations[i]
		if op.Type != "tags" || len(op.Set) == 0 {
			continue
		}
		tags, err := taglib.ReadTags(op.Path)
		if err != nil {
			continue
		}
		old := map[string]string{}
		byUpper := map[string]string{}
		for key := range tags {
			byUpper[strings.ToUpper(key)] = key
		}
		for _, key := range append(keysOf(op.Set), op.Delete...) {
			if actual, ok := byUpper[strings.ToUpper(key)]; ok {
				old[key] = firstTag(tags, actual)
			} else {
				old[key] = ""
			}
		}
		op.Old = old
	}
	// A move that would overwrite another planned destination is unsafe.
	dests := map[string]int{}
	for i := range plan.Operations {
		op := &plan.Operations[i]
		if op.Type == "move" {
			if dests[strings.ToLower(op.NewPath)] > 0 {
				op.Error = "destination conflict with another planned move"
			}
			dests[strings.ToLower(op.NewPath)]++
		}
	}
	sort.Slice(plan.Operations, func(i, j int) bool {
		a, b := plan.Operations[i], plan.Operations[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.Path < b.Path
	})
	return plan
}

// operationKey identifies an operation for plan de-duplication. The tag
// values are part of the key: two rules can legitimately write different
// tags to the same file.
func operationKey(op Operation) string {
	keys := keysOf(op.Set)
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(op.Type)
	b.WriteString("|")
	b.WriteString(op.Path)
	b.WriteString("|")
	b.WriteString(op.NewPath)
	for _, key := range keys {
		b.WriteString("|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(op.Set[key])
	}
	return b.String()
}

// TrackByPath finds a scanned track by its absolute path.
func TrackByPath(scan *Scan, path string) *Track {
	for i := range scan.Tracks {
		if strings.EqualFold(scan.Tracks[i].Path, path) {
			return &scan.Tracks[i]
		}
	}
	return nil
}

// singleAlbum reports whether a folder's tracks can be treated as one release.
// Loose files in the library root, or a folder holding several differently
// named albums, are left alone so album-wide fixes cannot retag unrelated
// tracks.
func singleAlbum(dir string, tracks []*Track) bool {
	if dir == "" {
		return false
	}
	albums := map[string]bool{}
	for _, t := range tracks {
		if a := strings.ToLower(strings.TrimSpace(t.Album)); a != "" {
			albums[a] = true
		}
	}
	return len(albums) <= 1
}

// Albums groups track indexes by album directory.
func Albums(scan *Scan) map[string][]*Track {
	groups := map[string][]*Track{}
	for i := range scan.Tracks {
		dir := scan.Tracks[i].AlbumDir()
		groups[dir] = append(groups[dir], &scan.Tracks[i])
	}
	return groups
}

func groupDistinct(values []string) map[string]int {
	counts := map[string]int{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			counts[v]++
		}
	}
	return counts
}

func mostCommon(counts map[string]int) string {
	best := ""
	bestN := -1
	for v, n := range counts {
		if n > bestN || (n == bestN && v < best) {
			best, bestN = v, n
		}
	}
	return best
}

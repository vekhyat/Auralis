package library

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
				detected[i].ID = rule.ID + "|" + detected[i].Path
			}
		}
		issues = append(issues, detected...)
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].RuleID != issues[j].RuleID {
			return issues[i].RuleID < issues[j].RuleID
		}
		return issues[i].Path < issues[j].Path
	})
	return issues
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
	for ruleID, ruleIssues := range byRule {
		rule := RuleByID(ruleID)
		if rule == nil || rule.Fix == nil {
			continue
		}
		for _, op := range rule.Fix(scan, profile, ruleIssues) {
			key := op.Type + "|" + op.Path + "|" + op.NewPath
			if seen[key] {
				continue
			}
			seen[key] = true
			plan.Operations = append(plan.Operations, op)
		}
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

// TrackByPath finds a scanned track by its absolute path.
func TrackByPath(scan *Scan, path string) *Track {
	for i := range scan.Tracks {
		if strings.EqualFold(scan.Tracks[i].Path, path) {
			return &scan.Tracks[i]
		}
	}
	return nil
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

var _ = fmt.Sprintf
var _ = context.Canceled

package backend

import (
	"strings"
	"testing"
)

func TestQobuzTitleSearchVariantsStripsMixYear(t *testing.T) {
	variants := qobuzTitleSearchVariants("Come Together - 2019 Mix")
	joined := strings.Join(variants, " | ")
	if len(variants) < 2 {
		t.Fatalf("expected stripped title variant, got %q", joined)
	}
	found := false
	for _, variant := range variants {
		if strings.EqualFold(variant, "Come Together") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing clean title, got %q", joined)
	}
}

func TestQobuzSearchQueriesPreferCleanTitle(t *testing.T) {
	queries := qobuzSearchQueries("", "Something - 2019 Mix", "The Beatles", "Abbey Road (2019 Mix)")
	found := false
	for _, query := range queries {
		if strings.EqualFold(query, "Something The Beatles") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected clean artist query, got %#v", queries)
	}
}

func TestLooksLikeQobuzReleaseVersion(t *testing.T) {
	if !looksLikeQobuzReleaseVersion("2019 Mix") {
		t.Fatal("2019 Mix should be treated as a version suffix")
	}
	if looksLikeQobuzReleaseVersion("Maxwell's Silver Hammer") {
		t.Fatal("a normal title should not look like a version suffix")
	}
}

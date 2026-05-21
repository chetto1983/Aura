package wiki

import (
	"context"
	"strings"
	"testing"
)

// _ ensures context is imported even if future refactors drop the only usage.
var _ = context.Background

func TestSubgraphSnippet_ReturnsHeadingSection(t *testing.T) {
	s := newWritesTestStore(t)
	body := "## Introduction\n\nThis is the intro paragraph.\n\n## Conclusion\n\nThis is the end.\n"
	writeFixturePage(t, s, "Test Page", body, "2026-05-21T00:00:00Z")

	snippet := s.SubgraphSnippet("test-page#introduction")
	if snippet == nil {
		t.Fatal("expected non-nil snippet for test-page#introduction")
	}
	snippetStr := string(snippet)
	if !strings.Contains(snippetStr, "intro paragraph") {
		t.Errorf("snippet missing expected content; got %q", snippetStr)
	}
	// Must not bleed into the Conclusion section.
	if strings.Contains(snippetStr, "Conclusion") {
		t.Errorf("snippet contains next section heading; got %q", snippetStr)
	}
}

func TestSubgraphSnippet_RespectsNextHeadingBoundary(t *testing.T) {
	s := newWritesTestStore(t)
	body := "## Section A\n\nContent A.\n\n## Section B\n\nContent B.\n\n## Section C\n\nContent C.\n"
	writeFixturePage(t, s, "Multi Section", body, "2026-05-21T00:00:00Z")

	snippet := s.SubgraphSnippet("multi-section#section-b")
	if snippet == nil {
		t.Fatal("expected non-nil snippet for section-b")
	}
	snippetStr := string(snippet)
	if !strings.Contains(snippetStr, "Content B") {
		t.Errorf("section B snippet missing Content B; got %q", snippetStr)
	}
	if strings.Contains(snippetStr, "Content A") {
		t.Errorf("section B snippet leaked into section A; got %q", snippetStr)
	}
	if strings.Contains(snippetStr, "Content C") {
		t.Errorf("section B snippet leaked into section C; got %q", snippetStr)
	}
}

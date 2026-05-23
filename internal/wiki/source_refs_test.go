package wiki

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func writePage(t *testing.T, dir, slug, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, slug+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", slug, err)
	}
}

func TestPagesReferencingSource_HitsExactMatchOnly(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	writePage(t, dir, "a", `---
title: A
sources:
  - source:src_aaa111
---
body`)
	writePage(t, dir, "b", `---
title: B
sources:
  - source:src_aaa111
  - source:src_bbb222
---
body`)
	writePage(t, dir, "c", `---
title: C
sources:
  - source:src_bbb222
---
body`)
	writePage(t, dir, "d", `---
title: D
---
no sources frontmatter`)
	// Page e references a source whose id has src_aaa111 as a prefix —
	// must NOT match an exact lookup for src_aaa111.
	writePage(t, dir, "e", `---
title: E
sources:
  - source:src_aaa111deeper
---
body`)

	s, err := NewStore(dir, logger)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	got, err := s.PagesReferencingSource(context.Background(), "src_aaa111")
	if err != nil {
		t.Fatalf("PagesReferencingSource: %v", err)
	}

	want := map[string]bool{"a": true, "b": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want exactly %v", got, want)
	}
	for _, slug := range got {
		if !want[slug] {
			t.Fatalf("unexpected hit %q in %v", slug, got)
		}
	}
}

func TestPagesReferencingSource_RejectsEmptyID(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s, err := NewStore(dir, logger)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := s.PagesReferencingSource(context.Background(), "   "); err == nil {
		t.Fatal("expected error on empty source id")
	}
}

func TestPagesReferencingSource_EmptyWikiReturnsNil(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s, err := NewStore(dir, logger)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	got, err := s.PagesReferencingSource(context.Background(), "src_anything")
	if err != nil {
		t.Fatalf("PagesReferencingSource: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil for empty wiki", got)
	}
}

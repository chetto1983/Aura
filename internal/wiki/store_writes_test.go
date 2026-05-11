package wiki

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// newWritesTestStore returns a *Store rooted at t.TempDir() with a silent logger.
// Real signature (verified at internal/wiki/store.go:102 during the
// 2026-05-10 plan revision):
//
//	func NewStore(dir string, logger *slog.Logger) (*Store, error)
func newWritesTestStore(t *testing.T) *Store {
	t.Helper()
	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := NewStore(t.TempDir(), silent)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func writeFixturePage(t *testing.T, s *Store, title, body, updatedAt string) *Page {
	t.Helper()
	p := &Page{
		Title:         title,
		Body:          body,
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     updatedAt,
		UpdatedAt:     updatedAt,
	}
	if err := s.WritePage(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWritePage_BackwardsCompat_NoVariadic(t *testing.T) {
	// Existing callers (ingest, parser.Writer, RepairLink) call WritePage(ctx, page)
	// with no third arg. This MUST keep working as plain trust-caller update/create.
	s := newWritesTestStore(t)
	p := &Page{
		Title:         "Hello",
		Body:          "x",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-05-10T00:00:00Z",
		UpdatedAt:     "2026-05-10T00:00:00Z",
	}
	if err := s.WritePage(context.Background(), p); err != nil {
		t.Fatalf("create: %v", err)
	}
	p.Body = "y"
	p.UpdatedAt = "2026-05-10T00:01:00Z"
	if err := s.WritePage(context.Background(), p); err != nil {
		t.Fatalf("update without ETag (trust-caller): %v", err)
	}
}

func TestWritePage_CreateOnly_SentinelEmptyString(t *testing.T) {
	s := newWritesTestStore(t)
	// create succeeds when page absent + expected==""
	p := &Page{
		Title: "Fresh", Body: "x",
		SchemaVersion: CurrentSchemaVersion, PromptVersion: "v1",
		CreatedAt: "2026-05-10T00:00:00Z", UpdatedAt: "2026-05-10T00:00:00Z",
	}
	if err := s.WritePage(context.Background(), p, ""); err != nil {
		t.Fatalf("create-only on absent page: %v", err)
	}
	// create-only on existing page → ConflictError
	if err := s.WritePage(context.Background(), p, ""); err == nil {
		t.Fatal("create-only on existing page should error")
	} else {
		var conflict *ConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("expected *ConflictError, got %T: %v", err, err)
		}
		if conflict.Expected != "" {
			t.Fatalf("conflict.Expected = %q, want \"\"", conflict.Expected)
		}
	}
}

func TestWritePage_ETagMatch(t *testing.T) {
	s := newWritesTestStore(t)
	p := writeFixturePage(t, s, "ETag", "x", "2026-05-10T00:00:00Z")
	existing, err := s.ReadPage(Slug(p.Title))
	if err != nil {
		t.Fatal(err)
	}
	p.Body = "y"
	p.UpdatedAt = "2026-05-10T00:01:00Z"
	if err := s.WritePage(context.Background(), p, existing.UpdatedAt); err != nil {
		t.Fatalf("ETag match update: %v", err)
	}
}

func TestWritePage_ETagMismatch(t *testing.T) {
	s := newWritesTestStore(t)
	writeFixturePage(t, s, "ETag2", "x", "2026-05-10T00:00:00Z")
	stale := "2026-05-09T00:00:00Z"
	p := &Page{
		Title: "ETag2", Body: "y",
		SchemaVersion: CurrentSchemaVersion, PromptVersion: "v1",
		CreatedAt: "2026-05-10T00:00:00Z",
		UpdatedAt: "2026-05-10T00:01:00Z",
	}
	err := s.WritePage(context.Background(), p, stale)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected *ConflictError, got %T: %v", err, err)
	}
	if conflict.Expected != stale {
		t.Fatalf("conflict.Expected = %q, want %q", conflict.Expected, stale)
	}
	if conflict.Actual != "2026-05-10T00:00:00Z" {
		t.Fatalf("conflict.Actual = %q, want %q", conflict.Actual, "2026-05-10T00:00:00Z")
	}
}

func TestWritePage_ETagInsideMutex_NoTOCTOU(t *testing.T) {
	// Race detector + concurrent writes with a stale ETag MUST yield exactly one success.
	s := newWritesTestStore(t)
	writeFixturePage(t, s, "Race", "x", "2026-05-10T00:00:00Z")
	existing, _ := s.ReadPage(Slug("Race"))
	var wins, conflicts int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := &Page{
				Title: "Race", Body: fmt.Sprintf("y%d", i),
				SchemaVersion: CurrentSchemaVersion, PromptVersion: "v1",
				CreatedAt: existing.CreatedAt,
				UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			}
			err := s.WritePage(context.Background(), p, existing.UpdatedAt)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else {
				conflicts++
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins=%d, want exactly 1", wins)
	}
	if conflicts != 9 {
		t.Fatalf("conflicts=%d, want exactly 9", conflicts)
	}
}

func TestUnversionedRoundTrip_SetOnFailure(t *testing.T) {
	// Use the SetGitCommitFuncForTest exported test seam (see Plan 03 Task 3).
	t.Skip("integration test — depends on Task 3 plumbing landing")
}

func TestUnversionedRoundTrip_ClearOnNextSuccess(t *testing.T) {
	t.Skip("integration test — depends on Task 3 plumbing landing")
}

// TestUnversionedReWriteValidatesPage covers WARNING 14 from the 2026-05-10
// plan revision: the metadata-only re-write triggered by a gitCommit failure
// (or success-path Unversioned-clear) MUST run Validate(reread) BEFORE
// marshalling so a corrupted on-disk page does not propagate. The test
// intentionally writes a corrupted file under the slug and verifies that
// Validate fails the re-write rather than silently flipping Unversioned.
func TestUnversionedReWriteValidatesPage(t *testing.T) {
	t.Skip("integration test — depends on Task 3 plumbing landing")
}

// Compile-time check: ensure ConflictError implements the error interface.
var _ error = (*ConflictError)(nil)

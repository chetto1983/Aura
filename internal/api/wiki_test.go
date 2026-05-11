package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aura/aura/internal/wiki"
)

// newWikiPage returns a minimal valid wiki.Page with the given title.
func newWikiPage(title string) *wiki.Page {
	now := time.Now().UTC().Format(time.RFC3339)
	return &wiki.Page{
		Title:         title,
		Body:          "body text",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// TestWikiPage_UnversionedJSON_False asserts that a page written with a
// successful git commit (Unversioned stays false) returns
// frontmatter["unversioned"] = false on GET /wiki/page?slug=…
//
// Route verified at internal/api/router.go:156 during 2026-05-10 plan
// revision: slug arrives as a QUERY PARAMETER, not a path segment.
func TestWikiPage_UnversionedJSON_False(t *testing.T) {
	e := newTestEnv(t)

	// Write a normal page — git commit succeeds, so Unversioned stays false.
	p := newWikiPage("Normal Page")
	if err := e.wiki.WritePage(context.Background(), p); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	slug := wiki.Slug("Normal Page")
	rr := e.do("GET", "/wiki/page?slug="+slug)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	var got WikiPage
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	unv, ok := got.Frontmatter["unversioned"]
	if !ok {
		t.Fatalf("frontmatter missing \"unversioned\" key: %v", got.Frontmatter)
	}
	// JSON numbers decode to float64; booleans decode to bool.
	if unv != false {
		t.Fatalf("frontmatter[\"unversioned\"] = %v (%T), want false", unv, unv)
	}
}

// TestWikiPage_UnversionedJSON_True asserts that a page written while git
// commit is failing (Unversioned=true) returns frontmatter["unversioned"] =
// true on GET /wiki/page?slug=…
//
// Uses the EXPORTED test seam SetGitCommitFuncForTest from Plan 03
// (BLOCKER 7 of 2026-05-10 plan revision: lowercase gitCommitFunc field
// would be unreachable from package api; SetGitCommitFuncForTest is exported).
func TestWikiPage_UnversionedJSON_True(t *testing.T) {
	e := newTestEnv(t)

	// Install a failing git commit via the exported test seam.
	e.wiki.SetGitCommitFuncForTest(func(_ context.Context, _, _ string) error {
		return errors.New("simulated git failure")
	})

	p := newWikiPage("Bad Commit Page")
	// WritePage sets Unversioned=true when gitCommit fails (Plan 03 D-17/D-18).
	if err := e.wiki.WritePage(context.Background(), p); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	slug := wiki.Slug("Bad Commit Page")
	rr := e.do("GET", "/wiki/page?slug="+slug)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	var got WikiPage
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	unv, ok := got.Frontmatter["unversioned"]
	if !ok {
		t.Fatalf("frontmatter missing \"unversioned\" key: %v", got.Frontmatter)
	}
	if unv != true {
		t.Fatalf("frontmatter[\"unversioned\"] = %v (%T), want true", unv, unv)
	}
}

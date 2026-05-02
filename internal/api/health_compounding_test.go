package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeLogMD writes a synthetic log.md into wikiDir with the given auto-sum
// entries. Each entry is written as a log.md table row with the given timestamp.
func writeLogMD(t *testing.T, wikiDir string, entries []struct {
	ts     time.Time
	action string
}) {
	t.Helper()
	var content string
	content = "# Wiki Log\n\n| timestamp | action | page |\n|---|---|---|\n"
	for _, e := range entries {
		content += fmt.Sprintf("| %s | %s | [[page]] |\n", e.ts.UTC().Format(time.RFC3339), e.action)
	}
	if err := os.WriteFile(filepath.Join(wikiDir, "log.md"), []byte(content), 0644); err != nil {
		t.Fatalf("write log.md: %v", err)
	}
}

// writeFakePages creates N fake .md page files in wikiDir (skipping log/index).
func writeFakePages(t *testing.T, wikiDir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("page-%02d.md", i)
		content := fmt.Sprintf("---\ntitle: page %d\ncategory: test\nschema_version: 2\nprompt_version: v1\ncreated_at: 2026-01-01T00:00:00Z\nupdated_at: 2026-01-01T00:00:00Z\n---\n\nBody text.\n", i)
		if err := os.WriteFile(filepath.Join(wikiDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write page: %v", err)
		}
	}
}

func TestCompoundingRate_HappyPath(t *testing.T) {
	env := newTestEnv(t)

	now := time.Now().UTC()
	writeLogMD(t, env.wiki.Dir(), []struct {
		ts     time.Time
		action string
	}{
		{now.Add(-1 * 24 * time.Hour), "auto-sum new"},
		{now.Add(-2 * 24 * time.Hour), "auto-sum patch"},
		{now.Add(-3 * 24 * time.Hour), "auto-sum new"},
		// Older than 7 days — should be excluded.
		{now.Add(-8 * 24 * time.Hour), "auto-sum new"},
		// Non-auto-sum line — should be excluded.
		{now.Add(-1 * 24 * time.Hour), "nightly-maintenance"},
	})
	writeFakePages(t, env.wiki.Dir(), 12)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var rollup HealthRollup
	if err := json.NewDecoder(w.Body).Decode(&rollup); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rollup.CompoundingRate.AutoAdded7d != 3 {
		t.Fatalf("want auto_added_7d=3, got %d", rollup.CompoundingRate.AutoAdded7d)
	}
	if rollup.CompoundingRate.TotalPages != 12 {
		t.Fatalf("want total_pages=12, got %d", rollup.CompoundingRate.TotalPages)
	}
	wantRate := 3.0 / 12.0 * 100
	if rollup.CompoundingRate.RatePct != wantRate {
		t.Fatalf("want rate_pct=%.2f, got %.2f", wantRate, rollup.CompoundingRate.RatePct)
	}
}

func TestCompoundingRate_EmptyWiki(t *testing.T) {
	env := newTestEnv(t)
	// No pages, no log entries.
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var rollup HealthRollup
	json.NewDecoder(w.Body).Decode(&rollup)
	if rollup.CompoundingRate.RatePct != 0 {
		t.Fatalf("want rate_pct=0 for empty wiki, got %f", rollup.CompoundingRate.RatePct)
	}
}

func TestCompoundingRate_OldEntriesExcluded(t *testing.T) {
	env := newTestEnv(t)

	now := time.Now().UTC()
	// All entries older than 7 days.
	writeLogMD(t, env.wiki.Dir(), []struct {
		ts     time.Time
		action string
	}{
		{now.Add(-8 * 24 * time.Hour), "auto-sum new"},
		{now.Add(-10 * 24 * time.Hour), "auto-sum new"},
	})
	writeFakePages(t, env.wiki.Dir(), 5)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	var rollup HealthRollup
	json.NewDecoder(w.Body).Decode(&rollup)
	if rollup.CompoundingRate.AutoAdded7d != 0 {
		t.Fatalf("want 0 auto-added (all old), got %d", rollup.CompoundingRate.AutoAdded7d)
	}
	if rollup.CompoundingRate.RatePct != 0 {
		t.Fatalf("want rate_pct=0, got %f", rollup.CompoundingRate.RatePct)
	}
}

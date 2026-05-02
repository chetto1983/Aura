package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aura/aura/internal/scheduler"
)

func newIssuesTestStore(t *testing.T) *scheduler.IssuesStore {
	t.Helper()
	db := scheduler.NewTestDB(t)
	return scheduler.NewIssuesStore(db)
}

func seedIssue(t *testing.T, store *scheduler.IssuesStore, kind, severity, slug string) {
	t.Helper()
	if err := store.Enqueue(context.Background(), scheduler.Issue{
		Kind:       kind,
		Severity:   severity,
		Slug:       slug,
		BrokenLink: slug + "-link",
		Message:    "test issue",
	}); err != nil {
		t.Fatalf("seedIssue: %v", err)
	}
}

func TestHandleMaintenanceList_HappyPath(t *testing.T) {
	store := newIssuesTestStore(t)
	seedIssue(t, store, "broken_link", "high", "page-a")
	seedIssue(t, store, "missing_category", "low", "page-b")

	router := NewRouter(Deps{Issues: store})
	req := httptest.NewRequest("GET", "/maintenance/issues", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body []WikiIssue
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("want 2 rows, got %d", len(body))
	}
}

func TestHandleMaintenanceList_FiltersBySeverity(t *testing.T) {
	store := newIssuesTestStore(t)
	seedIssue(t, store, "broken_link", "high", "p1")
	seedIssue(t, store, "missing_category", "low", "p2")

	router := NewRouter(Deps{Issues: store})
	req := httptest.NewRequest("GET", "/maintenance/issues?severity=high", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body []WikiIssue
	json.NewDecoder(w.Body).Decode(&body)
	if len(body) != 1 || body[0].Severity != "high" {
		t.Fatalf("want 1 high-severity issue, got %d", len(body))
	}
}

func TestHandleMaintenanceList_Empty(t *testing.T) {
	store := newIssuesTestStore(t)
	router := NewRouter(Deps{Issues: store})
	req := httptest.NewRequest("GET", "/maintenance/issues", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body []WikiIssue
	json.NewDecoder(w.Body).Decode(&body)
	if len(body) != 0 {
		t.Fatalf("want empty, got %d", len(body))
	}
}

func TestHandleMaintenanceList_NilStore(t *testing.T) {
	router := NewRouter(Deps{Issues: nil})
	req := httptest.NewRequest("GET", "/maintenance/issues", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with nil store, got %d", w.Code)
	}
	var body []WikiIssue
	json.NewDecoder(w.Body).Decode(&body)
	if len(body) != 0 {
		t.Fatalf("want empty array, got %d", len(body))
	}
}

func TestHandleMaintenanceResolve_HappyPath(t *testing.T) {
	store := newIssuesTestStore(t)
	seedIssue(t, store, "broken_link", "high", "page-x")
	rows, _ := store.List(context.Background(), "open")
	id := rows[0].ID

	router := NewRouter(Deps{Issues: store})
	req := httptest.NewRequest("POST", fmt.Sprintf("/maintenance/issues/%d/resolve", id), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body WikiIssue
	json.NewDecoder(w.Body).Decode(&body)
	if body.Status != "resolved" {
		t.Fatalf("want status=resolved, got %q", body.Status)
	}
}

func TestHandleMaintenanceResolve_NotFound(t *testing.T) {
	store := newIssuesTestStore(t)
	router := NewRouter(Deps{Issues: store})
	req := httptest.NewRequest("POST", "/maintenance/issues/9999/resolve", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestHandleMaintenanceResolve_AlreadyResolved(t *testing.T) {
	store := newIssuesTestStore(t)
	seedIssue(t, store, "broken_link", "high", "page-y")
	rows, _ := store.List(context.Background(), "open")
	id := rows[0].ID
	// Resolve once.
	store.Resolve(context.Background(), id)

	router := NewRouter(Deps{Issues: store})
	req := httptest.NewRequest("POST", fmt.Sprintf("/maintenance/issues/%d/resolve", id), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409 on already-resolved, got %d", w.Code)
	}
}

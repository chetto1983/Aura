package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aura/aura/internal/cron"
)

type fakeIssueRepository struct {
	rows []cron.Issue
}

func (f *fakeIssueRepository) Enqueue(context.Context, cron.Issue) error { return nil }

func (f *fakeIssueRepository) List(_ context.Context, status string) ([]cron.Issue, error) {
	var out []cron.Issue
	for _, row := range f.rows {
		if status == "" || row.Status == status {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeIssueRepository) Get(_ context.Context, id int64) (cron.Issue, error) {
	for _, row := range f.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return cron.Issue{}, cron.ErrIssueNotFound
}

func (f *fakeIssueRepository) Resolve(_ context.Context, id int64) error {
	for i, row := range f.rows {
		if row.ID == id {
			if row.Status != "open" {
				return cron.ErrIssueAlreadyResolved
			}
			now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
			f.rows[i].Status = "resolved"
			f.rows[i].ResolvedAt = &now
			return nil
		}
	}
	return cron.ErrIssueNotFound
}

func TestMaintenanceHandlersAcceptIssueRepositoryInterface(t *testing.T) {
	repo := &fakeIssueRepository{rows: []cron.Issue{{
		ID:        5,
		Kind:      "broken_link",
		Severity:  "high",
		Slug:      "aura",
		Message:   "broken link: [[missing]]",
		Status:    "open",
		CreatedAt: time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC),
	}}}
	router := NewRouter(Deps{Issues: repo})

	req := httptest.NewRequest("GET", "/maintenance/issues?status=open", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/maintenance/issues/5/resolve", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve got %d: %s", w.Code, w.Body.String())
	}
	if repo.rows[0].Status != "resolved" {
		t.Fatalf("status = %q", repo.rows[0].Status)
	}
}

func newIssuesTestStore(t *testing.T) *cron.IssuesStore {
	t.Helper()
	db := cron.NewTestDB(t)
	return cron.NewIssuesStore(db)
}

func seedIssue(t *testing.T, store *cron.IssuesStore, kind, severity, slug string) {
	t.Helper()
	if err := store.Enqueue(context.Background(), cron.Issue{
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

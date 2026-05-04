package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/wiki"

	_ "modernc.org/sqlite"
)

func newSummariesDB(t *testing.T) (*sql.DB, *summarizer.SummariesStore) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "summ.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// Apply migration.
	const mig = `CREATE TABLE IF NOT EXISTS proposed_updates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_id INTEGER NOT NULL,
		fact TEXT NOT NULL,
		action TEXT NOT NULL,
		target_slug TEXT NOT NULL DEFAULT '',
		similarity REAL NOT NULL DEFAULT 0,
		source_turn_ids TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT '',
		related_slugs TEXT NOT NULL DEFAULT '',
		provenance_json TEXT NOT NULL DEFAULT '{}',
		status TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(mig); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, summarizer.NewSummariesStore(db)
}

func seedProposal(t *testing.T, db *sql.DB, action, status string) int64 {
	t.Helper()
	res, err := db.ExecContext(context.Background(),
		`INSERT INTO proposed_updates (chat_id, fact, action, target_slug, similarity, source_turn_ids, category, related_slugs, provenance_json, status)
		 VALUES (1, 'test fact', ?, 'slug', 0.5, '[1,2]', 'project', '["aura"]', '{"origin_tool":"search_memory","evidence":[{"kind":"source","id":"src_1","page":2}]}', ?)`,
		action, status)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestHandleSummariesList_HappyPath(t *testing.T) {
	db, store := newSummariesDB(t)
	seedProposal(t, db, "new", "pending")
	seedProposal(t, db, "patch", "pending")

	router := NewRouter(Deps{Summaries: store})
	req := httptest.NewRequest("GET", "/summaries", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body []ProposedUpdate
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("want 2 rows, got %d", len(body))
	}
	if body[0].Provenance.OriginTool != "search_memory" || len(body[0].Provenance.Evidence) != 1 {
		t.Fatalf("missing provenance in DTO: %#v", body[0].Provenance)
	}
}

func TestHandleSummariesList_Empty(t *testing.T) {
	_, store := newSummariesDB(t)
	router := NewRouter(Deps{Summaries: store})
	req := httptest.NewRequest("GET", "/summaries", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body []ProposedUpdate
	json.NewDecoder(w.Body).Decode(&body)
	if len(body) != 0 {
		t.Fatalf("want empty, got %d", len(body))
	}
}

func TestHandleSummariesList_NilStore(t *testing.T) {
	router := NewRouter(Deps{Summaries: nil})
	req := httptest.NewRequest("GET", "/summaries", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with nil store, got %d", w.Code)
	}
	var body []ProposedUpdate
	json.NewDecoder(w.Body).Decode(&body)
	if len(body) != 0 {
		t.Fatalf("want empty array, got %d", len(body))
	}
}

func TestHandleSummariesApprove_HappyPath(t *testing.T) {
	db, store := newSummariesDB(t)
	id := seedProposal(t, db, "new", "pending")
	ws := &fakeWikiStoreForSummaries{}

	router := NewRouter(Deps{Summaries: store, SummariesWiki: ws})
	req := httptest.NewRequest("POST", fmt.Sprintf("/summaries/%d/approve", id), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body ProposedUpdate
	json.NewDecoder(w.Body).Decode(&body)
	if body.Status != "approved" {
		t.Fatalf("want status=approved, got %q", body.Status)
	}
	if len(ws.written) == 0 {
		t.Fatal("want wiki mutation on approve new")
	}
	if got := ws.written[0].Category; got != "project" {
		t.Fatalf("written category = %q, want project", got)
	}
	if len(ws.written[0].Related) != 1 || ws.written[0].Related[0] != "aura" {
		t.Fatalf("written related = %#v, want [aura]", ws.written[0].Related)
	}
}

func TestHandleSummariesApprove_NotFound(t *testing.T) {
	_, store := newSummariesDB(t)
	router := NewRouter(Deps{Summaries: store, SummariesWiki: &fakeWikiStoreForSummaries{}})
	req := httptest.NewRequest("POST", "/summaries/9999/approve", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestHandleSummariesApprove_AlreadyApproved(t *testing.T) {
	db, store := newSummariesDB(t)
	id := seedProposal(t, db, "new", "approved")
	router := NewRouter(Deps{Summaries: store, SummariesWiki: &fakeWikiStoreForSummaries{}})
	req := httptest.NewRequest("POST", fmt.Sprintf("/summaries/%d/approve", id), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", w.Code)
	}
}

func TestHandleSummariesBatchApprove_Partial(t *testing.T) {
	db, store := newSummariesDB(t)
	first := seedProposal(t, db, "new", "pending")
	second := seedProposal(t, db, "patch", "pending")
	done := seedProposal(t, db, "new", "approved")
	ws := &fakeWikiStoreForSummaries{}

	router := NewRouter(Deps{Summaries: store, SummariesWiki: ws})
	body := []byte(fmt.Sprintf(`{"ids":[%d,%d,%d,9999]}`, first, second, done))
	req := httptest.NewRequest("POST", "/summaries/batch/approve", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp SummaryBatchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Updated) != 2 {
		t.Fatalf("updated = %d, want 2: %#v", len(resp.Updated), resp)
	}
	if len(resp.Failed) != 2 {
		t.Fatalf("failed = %d, want 2: %#v", len(resp.Failed), resp)
	}
	if len(ws.written) != 2 {
		t.Fatalf("wiki writes = %d, want 2", len(ws.written))
	}
}

func TestHandleSummariesBatchReject_HappyPath(t *testing.T) {
	db, store := newSummariesDB(t)
	first := seedProposal(t, db, "new", "pending")
	second := seedProposal(t, db, "patch", "pending")
	ws := &fakeWikiStoreForSummaries{}

	router := NewRouter(Deps{Summaries: store, SummariesWiki: ws})
	body := []byte(fmt.Sprintf(`{"ids":[%d,%d,%d]}`, first, second, first))
	req := httptest.NewRequest("POST", "/summaries/batch/reject", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp SummaryBatchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Updated) != 2 || len(resp.Failed) != 0 {
		t.Fatalf("response = %#v, want 2 updated and 0 failed", resp)
	}
	if len(ws.written) != 0 {
		t.Fatal("want no wiki mutation on batch reject")
	}
}

func TestHandleSummariesBatchReject_InvalidIDs(t *testing.T) {
	_, store := newSummariesDB(t)
	router := NewRouter(Deps{Summaries: store})
	req := httptest.NewRequest("POST", "/summaries/batch/reject", bytes.NewReader([]byte(`{"ids":[0]}`)))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSummariesReject_HappyPath(t *testing.T) {
	db, store := newSummariesDB(t)
	id := seedProposal(t, db, "patch", "pending")
	ws := &fakeWikiStoreForSummaries{}

	router := NewRouter(Deps{Summaries: store, SummariesWiki: ws})
	req := httptest.NewRequest("POST", fmt.Sprintf("/summaries/%d/reject", id), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body ProposedUpdate
	json.NewDecoder(w.Body).Decode(&body)
	if body.Status != "rejected" {
		t.Fatalf("want status=rejected, got %q", body.Status)
	}
	if len(ws.written) != 0 {
		t.Fatal("want no wiki mutation on reject")
	}
}

func TestHandleSummariesReject_NotFound(t *testing.T) {
	_, store := newSummariesDB(t)
	router := NewRouter(Deps{Summaries: store, SummariesWiki: &fakeWikiStoreForSummaries{}})
	req := httptest.NewRequest("POST", "/summaries/9999/reject", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

// fakeWikiStoreForSummaries satisfies summarizer.WikiWriter for approve tests.
type fakeWikiStoreForSummaries struct {
	written []*wiki.Page
}

func (f *fakeWikiStoreForSummaries) WritePage(_ context.Context, p *wiki.Page) error {
	f.written = append(f.written, p)
	return nil
}

func (f *fakeWikiStoreForSummaries) ReadPage(_ string) (*wiki.Page, error) {
	return &wiki.Page{
		Title: "Existing Page", Category: "fact", Body: "existing body",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (f *fakeWikiStoreForSummaries) AppendLog(_ context.Context, _, _ string) {}

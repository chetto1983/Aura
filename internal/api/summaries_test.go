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
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/storage/memoryindex"
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
	if err := migrations.Run(context.Background(), db); err != nil {
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

func seedSkillProposal(t *testing.T, db *sql.DB, action, status string) int64 {
	t.Helper()
	provenance := `{"origin_tool":"propose_skill_change","proposal_kind":"skill","skill":{"action":"create","name":"morning-brief","description":"Morning briefing flow","allowed_tools":["daily_briefing"],"smoke_prompt":"Brief me for today","content":"---\nname: morning-brief\ndescription: Morning briefing flow\n---\nUse daily_briefing."}}`
	res, err := db.ExecContext(context.Background(),
		`INSERT INTO proposed_updates (chat_id, fact, action, target_slug, similarity, source_turn_ids, category, related_slugs, provenance_json, status)
		 VALUES (1, 'skill draft', ?, '', 1, '[]', 'skill', '[]', ?, ?)`,
		action, provenance, status)
	if err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedOperationalQuarantine(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.ExecContext(context.Background(),
		`INSERT INTO proposed_updates
		  (chat_id, fact, action, target_slug, similarity, source_turn_ids, category, related_slugs, provenance_json, status, kind, signature_hash)
		 VALUES (0, 'Retry with a shorter query on timeout.', 'new', 'web_search', 1, '[]', 'timeout', '[]', '{"source_run_id":"run-1"}', 'quarantine', 'operational_memory', 'sig-quarantine')`)
	if err != nil {
		t.Fatalf("seed quarantine: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

type fakeSummaryReviewRepository struct {
	rows []summarizer.ProposedUpdate
}

func (f *fakeSummaryReviewRepository) List(_ context.Context, status string, limit int) ([]summarizer.ProposedUpdate, error) {
	out := make([]summarizer.ProposedUpdate, 0, len(f.rows))
	for _, row := range f.rows {
		if status == "" || row.Status == status {
			out = append(out, row)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeSummaryReviewRepository) Get(_ context.Context, id int64) (summarizer.ProposedUpdate, error) {
	for _, row := range f.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return summarizer.ProposedUpdate{}, summarizer.ErrProposalNotFound
}

func (f *fakeSummaryReviewRepository) SetStatus(_ context.Context, id int64, newStatus string) (summarizer.ProposedUpdate, error) {
	for i, row := range f.rows {
		if row.ID == id {
			if row.Status != "pending" {
				return summarizer.ProposedUpdate{}, summarizer.ErrProposalConflict
			}
			f.rows[i].Status = newStatus
			return f.rows[i], nil
		}
	}
	return summarizer.ProposedUpdate{}, summarizer.ErrProposalNotFound
}

func assertAPIProposalScalar(t *testing.T, db *sql.DB, query string, want any, args ...any) {
	t.Helper()
	switch w := want.(type) {
	case string:
		var got string
		if err := db.QueryRowContext(context.Background(), query, args...).Scan(&got); err != nil {
			t.Fatalf("query scalar: %v", err)
		}
		if got != w {
			t.Fatalf("scalar = %q, want %q", got, w)
		}
	case int64:
		var got int64
		if err := db.QueryRowContext(context.Background(), query, args...).Scan(&got); err != nil {
			t.Fatalf("query scalar: %v", err)
		}
		if got != w {
			t.Fatalf("scalar = %d, want %d", got, w)
		}
	default:
		t.Fatalf("unsupported scalar want type %T", want)
	}
}

func TestHandleSummariesAcceptsReviewRepositoryInterface(t *testing.T) {
	repo := &fakeSummaryReviewRepository{rows: []summarizer.ProposedUpdate{{
		ID:         7,
		ChatID:     42,
		Fact:       "Review repository boundary",
		Action:     "patch",
		TargetSlug: "aura",
		Similarity: 0.8,
		Status:     "pending",
		CreatedAt:  time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC),
	}}}
	router := NewRouter(Deps{Summaries: repo})

	req := httptest.NewRequest("GET", "/summaries", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/summaries/7/reject", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reject got %d: %s", w.Code, w.Body.String())
	}
	if repo.rows[0].Status != "rejected" {
		t.Fatalf("status = %q", repo.rows[0].Status)
	}
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

func TestHandleQuarantineListApproveDelete(t *testing.T) {
	db, store := newSummariesDB(t)
	id := seedOperationalQuarantine(t, db)
	opMemory, err := memoryindex.NewStore(db)
	if err != nil {
		t.Fatalf("memoryindex.NewStore: %v", err)
	}
	router := NewRouter(Deps{Summaries: store, OperationalMemory: opMemory})

	req := httptest.NewRequest("GET", "/quarantine", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list want 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []ProposedUpdate
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != id || list[0].Status != "quarantine" {
		t.Fatalf("unexpected quarantine list: %#v", list)
	}
	if list[0].Kind != "operational_memory" || list[0].SignatureHash != "sig-quarantine" {
		t.Fatalf("missing quarantine metadata: %#v", list[0])
	}

	req = httptest.NewRequest("POST", fmt.Sprintf("/quarantine/%d/approve", id), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approve want 200, got %d: %s", w.Code, w.Body.String())
	}
	var approved ProposedUpdate
	if err := json.NewDecoder(w.Body).Decode(&approved); err != nil {
		t.Fatalf("decode approve: %v", err)
	}
	if approved.Status != "accepted" {
		t.Fatalf("approved status = %q, want accepted", approved.Status)
	}
	assertAPIProposalScalar(t, db, `SELECT status FROM proposed_updates WHERE id = ?`, "accepted", id)
	assertAPIProposalScalar(t, db, `SELECT COUNT(*) FROM compact_memory_documents WHERE id = ? AND status = 'active'`, int64(1), "operational:sig-quarantine")

	deleteID := seedOperationalQuarantine(t, db)
	req = httptest.NewRequest("POST", fmt.Sprintf("/quarantine/%d/delete", deleteID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete want 200, got %d: %s", w.Code, w.Body.String())
	}
	assertAPIProposalScalar(t, db, `SELECT COUNT(*) FROM proposed_updates WHERE id = ?`, int64(0), deleteID)
}

func TestHandleSummariesList_SkillProposalLifecycle(t *testing.T) {
	db, store := newSummariesDB(t)
	seedSkillProposal(t, db, "skill_create", "pending")

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
	if len(body) != 1 || body[0].Provenance.Skill == nil {
		t.Fatalf("missing skill proposal DTO: %#v", body)
	}
	lifecycle := body[0].SkillLifecycle
	if lifecycle == nil {
		t.Fatalf("missing skill lifecycle: %#v", body[0])
	}
	if lifecycle.Mode != "review_only" || lifecycle.ReviewStatus != "pending_review" {
		t.Fatalf("unexpected lifecycle = %#v", lifecycle)
	}
	if lifecycle.InstallStatus != "not_installed_by_summary_approval" || lifecycle.SmokeStatus != "operator_required" {
		t.Fatalf("lifecycle should make install/smoke handoff explicit: %#v", lifecycle)
	}
	if !strings.Contains(lifecycle.NextStep, "morning-brief") {
		t.Fatalf("next step should name the skill: %#v", lifecycle)
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
	if len(ws.written[0].Related) != 1 || ws.written[0].Related[0].Slug != "aura" {
		t.Fatalf("written related = %#v, want [aura]", ws.written[0].Related)
	}
}

type fakeSkillProposalApplier struct {
	applied []SkillProposalApplyRequest
	err     error
}

func (f *fakeSkillProposalApplier) ApplySkillProposal(_ context.Context, proposal SkillProposalApplyRequest) error {
	if f.err != nil {
		return f.err
	}
	f.applied = append(f.applied, proposal)
	return nil
}

func TestHandleSummariesApprove_SkillProposalWithoutAdminMarksReviewedOnly(t *testing.T) {
	db, store := newSummariesDB(t)
	id := seedSkillProposal(t, db, "skill_create", "pending")
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
	if body.Status != "reviewed" || body.Action != "skill_create" {
		t.Fatalf("response = %+v", body)
	}
	if body.SkillLifecycle == nil || body.SkillLifecycle.ReviewStatus != "reviewed" {
		t.Fatalf("reviewed skill proposal should say reviewed: %#v", body.SkillLifecycle)
	}
	if body.SkillLifecycle.InstallStatus != "not_installed_by_summary_approval" {
		t.Fatalf("install status = %#v", body.SkillLifecycle)
	}
	if len(ws.written) != 0 {
		t.Fatalf("skill proposal approval must not write wiki pages, wrote %d", len(ws.written))
	}
}

func TestHandleSummariesApprove_SkillProposalWithAdminAppliesLocalSkill(t *testing.T) {
	db, store := newSummariesDB(t)
	id := seedSkillProposal(t, db, "skill_create", "pending")
	applier := &fakeSkillProposalApplier{}

	router := NewRouter(Deps{Summaries: store, SkillsAdmin: true, SkillProposals: applier})
	req := httptest.NewRequest("POST", fmt.Sprintf("/summaries/%d/approve", id), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body ProposedUpdate
	json.NewDecoder(w.Body).Decode(&body)
	if body.Status != "approved" {
		t.Fatalf("status = %q, want approved", body.Status)
	}
	if len(applier.applied) != 1 {
		t.Fatalf("applied = %+v", applier.applied)
	}
	got := applier.applied[0]
	if got.Action != "skill_create" || got.Name != "morning-brief" || !strings.Contains(got.Content, "daily_briefing") {
		t.Fatalf("applied proposal = %+v", got)
	}
	if body.SkillLifecycle == nil || body.SkillLifecycle.Mode != "admin_apply_requested" || body.SkillLifecycle.InstallStatus != "approval_applied_check_list_skills" {
		t.Fatalf("skill lifecycle = %#v", body.SkillLifecycle)
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

func (f *fakeWikiStoreForSummaries) WritePage(_ context.Context, p *wiki.Page, _ ...string) error {
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

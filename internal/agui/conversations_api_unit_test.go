package agui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// errConvStore is a ConversationStore double whose every method returns a configurable
// error, so the unit suite can exercise the 500/error-redaction and not-found branches
// of the CHAT-02 handlers without a live DB (the route→method MAPPING is asserted in
// the db_integration suite; THIS covers the error projections). A nil err makes a
// method a happy no-op.
type errConvStore struct {
	err error
	// ownsGate makes GetForIdentity return a conversation (caller owns it) so the Phase-36
	// branch owner-gate passes and the input-validation / fork-error paths under test run.
	// Default false preserves the "GetForIdentity returns err" behavior the not-found
	// projection suites rely on.
	ownsGate bool
}

func (e *errConvStore) Create(context.Context, conversations.CreateParams) (conversations.Conversation, error) {
	return conversations.Conversation{}, e.err
}

func (e *errConvStore) Get(context.Context, string) (conversations.Conversation, error) {
	return conversations.Conversation{}, e.err
}

func (e *errConvStore) LoadHistory(context.Context, string) ([]llm.Message, error) {
	return nil, e.err
}

func (e *errConvStore) ListTurnAttachments(context.Context, string) ([]conversations.TurnAttachments, error) {
	return nil, e.err
}

func (e *errConvStore) ListTurnReasoning(context.Context, string) ([]conversations.TurnReasoning, error) {
	return nil, e.err
}

func (e *errConvStore) List(context.Context, bool) ([]conversations.Conversation, error) {
	return nil, e.err
}

func (e *errConvStore) SearchConversationTurns(context.Context, string, int) ([]conversations.SearchResult, error) {
	return nil, e.err
}

func (e *errConvStore) UpdateStatus(context.Context, string, string) error { return e.err }

func (e *errConvStore) Rename(context.Context, string, string) error { return e.err }

func (e *errConvStore) SetTitleIfNull(context.Context, string, string) error { return e.err }

func (e *errConvStore) Delete(context.Context, string) error { return e.err }

func (e *errConvStore) ListContextRotEvents(context.Context, string) ([]conversations.RotEvent, error) {
	return nil, e.err
}

// D-09 branch surface — the error fake returns its injected err so the handler
// error-redaction paths (500 via sanitizeErr) are unit-coverable.
func (e *errConvStore) ListBranches(context.Context, string) ([]conversations.Branch, error) {
	return nil, e.err
}

func (e *errConvStore) ForkBranch(context.Context, string, int, string, string) (int, uuid.UUID, error) {
	return 0, uuid.UUID{}, e.err
}

func (e *errConvStore) CanonicalBranchLeaf(context.Context, string) (int, error) { return 0, e.err }

// Phase 36 owner-scoped surface — the error fake returns its injected err (or (0,err)
// for the rows-affected mutates) so the handler's error/404 projections stay unit-coverable.
// e.err is always non-nil in the suites that use this fake, so the affected==0 (403/404
// existence-probe) branch is never reached here; that split is covered by dedicated fakes.
func (e *errConvStore) GetForIdentity(_ context.Context, id, _ string) (conversations.Conversation, error) {
	if e.ownsGate {
		return conversations.Conversation{ID: id}, nil
	}
	return conversations.Conversation{}, e.err
}

func (e *errConvStore) ListForIdentity(context.Context, string, bool) ([]conversations.Conversation, error) {
	return nil, e.err
}

func (e *errConvStore) SearchConversationTurnsForIdentity(context.Context, string, string, int) ([]conversations.SearchResult, error) {
	return nil, e.err
}

func (e *errConvStore) DeleteForIdentity(context.Context, string, string) (int64, error) {
	return 0, e.err
}

func (e *errConvStore) UpdateStatusForIdentity(context.Context, string, string, string) (int64, error) {
	return 0, e.err
}

func (e *errConvStore) RenameForIdentity(context.Context, string, string, string) (int64, error) {
	return 0, e.err
}

func (e *errConvStore) UpdateReasoningEffortForIdentity(context.Context, string, string, string) (int64, error) {
	return 0, e.err
}

func convAPIServer(t *testing.T, store ConversationStore) *httptest.Server {
	t.Helper()
	// The DELETE route now goes through the runner's delete lifecycle (MUSR-05); wire the
	// scriptedRunner's conv to the SAME store so its delegating lifecycle exercises the
	// store's owner gate (the 403/404/204 assertions in owner_scoping_test.go).
	s := NewServer(&scriptedRunner{conv: store}, store, ServerConfig{})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return srv
}

func req(t *testing.T, srv *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	r, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

const goodID = "11111111-1111-1111-1111-111111111111"

func TestConversationsAPI_CreateUsesRunnerConversationLifecycle(t *testing.T) {
	run := &scriptedRunner{newConversationID: goodID}
	conv := &fakeConvStore{known: map[string]bool{goodID: true}}
	s := NewServer(run, conv, ServerConfig{})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)

	code, body := req(t, srv, http.MethodPost, "/api/conversations", `{"title":"  Deploy rollback plan?  "}`)
	if code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", code, body)
	}
	if run.newConversationCalls != 1 {
		t.Fatalf("NewConversation calls = %d, want 1", run.newConversationCalls)
	}
	var got conversations.Conversation
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode created conversation: %v (%s)", err, body)
	}
	if got.ID != goodID {
		t.Fatalf("created conversation ID = %q, want %q", got.ID, goodID)
	}
	if !got.TitleSet || got.Title != "Deploy rollback plan" {
		t.Fatalf("created conversation title = set:%v %q, want normalized title", got.TitleSet, got.Title)
	}
}

func TestNormalizeCreateTitle(t *testing.T) {
	got := normalizeCreateTitle("  `First line\nsecond line?!`  ")
	if got != "First line second line" {
		t.Fatalf("normalizeCreateTitle = %q, want compact punctuation-free title", got)
	}
	long := normalizeCreateTitle(strings.Repeat("a", createTitleMaxRunes+10))
	if len([]rune(long)) != createTitleMaxRunes {
		t.Fatalf("long title runes = %d, want %d", len([]rune(long)), createTitleMaxRunes)
	}
}

// TestConversationsAPI_StoreError500 proves every route maps a generic store failure
// to a 500 (not a 404 and not a panic), and the body is redacted by sanitizeErr — a
// store error embedding a DSN must not leak on the wire (T-12-10).
func TestConversationsAPI_StoreError500(t *testing.T) {
	leak := errors.New("dial postgres://aura_app:s3cr3t@db:5432/aura failed")
	srv := convAPIServer(t, &errConvStore{err: leak})

	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/api/conversations", ""},
		{http.MethodGet, "/api/conversations/search?q=foo", ""},
		{http.MethodGet, "/api/conversations/" + goodID, ""},
		{http.MethodGet, "/api/conversations/" + goodID + "/rot-events", ""},
		{http.MethodPost, "/api/conversations/" + goodID + "/rename", `{"title":"x"}`},
		{http.MethodPost, "/api/conversations/" + goodID + "/archive", ""},
		{http.MethodPost, "/api/conversations/" + goodID + "/unarchive", ""},
		{http.MethodDelete, "/api/conversations/" + goodID, ""},
	}
	for _, c := range cases {
		code, body := req(t, srv, c.method, c.path, c.body)
		if code != http.StatusInternalServerError {
			t.Errorf("%s %s: status = %d, want 500: %s", c.method, c.path, code, body)
		}
		if strings.Contains(body, "s3cr3t") {
			t.Errorf("%s %s: error body leaked the DSN password (not sanitized): %s", c.method, c.path, body)
		}
	}
}

// TestBranchAPI_StoreError500AndRedaction proves the D-09 branch handlers map a generic
// store failure to a 500 with a sanitized body (no DSN leak): GET /branches (ListBranches),
// POST /edit (ForkBranch), and POST /branches/{seq}/select (rerunBranch's Get fails).
func TestBranchAPI_StoreError500AndRedaction(t *testing.T) {
	leak := errors.New("dial postgres://aura_app:s3cr3t@db:5432/aura failed")
	srv := convAPIServer(t, &errConvStore{err: leak})

	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/api/conversations/" + goodID + "/branches", ""},
		{http.MethodPost, "/api/conversations/" + goodID + "/edit", `{"diverge_seq":2,"content":"x"}`},
		{http.MethodPost, "/api/conversations/" + goodID + "/branches/3/select", ""},
	}
	for _, c := range cases {
		code, body := req(t, srv, c.method, c.path, c.body)
		if code != http.StatusInternalServerError {
			t.Errorf("%s %s: status = %d, want 500: %s", c.method, c.path, code, body)
		}
		if strings.Contains(body, "s3cr3t") {
			t.Errorf("%s %s: branch error body leaked the DSN password: %s", c.method, c.path, body)
		}
	}
}

// TestBranchAPI_MalformedAndBadInput proves the parse guards: a non-UUID conv id → 404,
// a non-numeric branch seq → 404, and an empty/zero diverge_seq → 400 — all before any
// store round-trip (T-25-26).
func TestBranchAPI_MalformedAndBadInput(t *testing.T) {
	// ownsGate: the caller owns the conversation so the owner-gate passes; the branch
	// STORE methods (ListBranches/ForkBranch) still must-not-be-called — the input guards
	// reject before any branch round-trip.
	srv := convAPIServer(t, &errConvStore{err: errors.New("must-not-be-called"), ownsGate: true})

	if code, _ := req(t, srv, http.MethodGet, "/api/conversations/not-a-uuid/branches", ""); code != http.StatusNotFound {
		t.Errorf("non-UUID id /branches status = %d, want 404", code)
	}
	if code, _ := req(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/branches/not-a-seq/select", ""); code != http.StatusNotFound {
		t.Errorf("non-numeric branch seq status = %d, want 404", code)
	}
	if code, _ := req(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/edit", `{"diverge_seq":0}`); code != http.StatusBadRequest {
		t.Errorf("zero diverge_seq status = %d, want 400", code)
	}
	if code, _ := req(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/edit", `not json`); code != http.StatusBadRequest {
		t.Errorf("malformed edit body status = %d, want 400", code)
	}
}

// TestBranchAPI_ForkTurnNotFound404 proves ForkBranch's ErrTurnNotFound maps to 404 (not
// 500) — a clean "turn not found" when editing a non-existent seq.
func TestBranchAPI_ForkTurnNotFound404(t *testing.T) {
	// ownsGate: the caller owns the conversation (owner-gate passes) so ForkBranch runs and
	// its ErrTurnNotFound is the error under test (not the gate's).
	srv := convAPIServer(t, &errConvStore{err: conversations.ErrTurnNotFound, ownsGate: true})
	code, _ := req(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/edit", `{"diverge_seq":99,"content":"x"}`)
	if code != http.StatusNotFound {
		t.Errorf("fork unknown turn status = %d, want 404", code)
	}
}

// TestBranchSelect_ValidatesLeafMembership (WR-01): POST /branches/{seq}/select re-runs
// ONLY a leaf ListBranches reports for this conversation. A numeric-but-absent leaf is a
// clean 404 with NO re-run — a privileged mutating route must not drive a re-run over an
// empty walk for an arbitrary client integer.
func TestBranchSelect_ValidatesLeafMembership(t *testing.T) {
	branches := []conversations.Branch{{LeafSeq: 5}}

	t.Run("known leaf re-runs", func(t *testing.T) {
		run := &scriptedRunner{events: textTurn("ok")}
		srv := newTestServer(t, run, &fakeConvStore{known: map[string]bool{goodID: true}, branches: branches})
		code, body := req(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/branches/5/select", "")
		if code != http.StatusOK {
			t.Fatalf("select known leaf status = %d, want 200: %s", code, body)
		}
		if run.gotBranchLeaf != 5 {
			t.Errorf("TurnBranch leaf = %d, want 5 (the selected leaf)", run.gotBranchLeaf)
		}
	})

	t.Run("absent leaf is 404 with no re-run", func(t *testing.T) {
		run := &scriptedRunner{events: textTurn("ok")}
		srv := newTestServer(t, run, &fakeConvStore{known: map[string]bool{goodID: true}, branches: branches})
		code, _ := req(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/branches/999/select", "")
		if code != http.StatusNotFound {
			t.Fatalf("select absent leaf status = %d, want 404", code)
		}
		if run.gotBranchLeaf != 0 {
			t.Errorf("an absent-leaf 404 still drove a re-run (leaf=%d); want no re-run", run.gotBranchLeaf)
		}
	})
}

// TestConversationsAPI_NotFoundProjection proves a store ErrConversationNotFound maps
// to 404 (not 500) on every {id} route that does a store round-trip.
func TestConversationsAPI_NotFoundProjection(t *testing.T) {
	srv := convAPIServer(t, &errConvStore{err: conversations.ErrConversationNotFound})
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/api/conversations/" + goodID, ""},
		{http.MethodGet, "/api/conversations/" + goodID + "/rot-events", ""},
		{http.MethodPost, "/api/conversations/" + goodID + "/rename", `{"title":"x"}`},
		{http.MethodPost, "/api/conversations/" + goodID + "/archive", ""},
		{http.MethodDelete, "/api/conversations/" + goodID, ""},
	}
	for _, c := range cases {
		code, body := req(t, srv, c.method, c.path, c.body)
		if code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404 (ErrConversationNotFound): %s", c.method, c.path, code, body)
		}
	}
}

// TestConversationsAPI_BadRequests proves the input-validation 400s: a malformed
// rename body and an empty search query.
func TestConversationsAPI_BadRequests(t *testing.T) {
	srv := convAPIServer(t, &errConvStore{})

	code, body := req(t, srv, http.MethodPost, "/api/conversations/"+goodID+"/rename", `{not json`)
	if code != http.StatusBadRequest {
		t.Errorf("malformed rename body: status = %d, want 400: %s", code, body)
	}

	code, body = req(t, srv, http.MethodGet, "/api/conversations/search?q=", "")
	if code != http.StatusBadRequest {
		t.Errorf("empty search query: status = %d, want 400: %s", code, body)
	}
}

// TestParseLimit pins the ?limit= contract: absent/non-numeric/non-positive → default,
// a sane positive value passes through, and anything above maxListLimit clamps — the
// clamp is the guard against one authenticated ?limit=2e9 request OOMing the process
// via make([]T, 0, limit) pre-allocations (CodeQL #40).
func TestParseLimit(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", defaultSearchLimit},
		{"abc", defaultSearchLimit},
		{"0", defaultSearchLimit},
		{"-5", defaultSearchLimit},
		{"50", 50},
		{"200", maxListLimit},
		{"201", maxListLimit},
		{"2000000000", maxListLimit},
	} {
		if got := parseLimit(tc.in); got != tc.want {
			t.Errorf("parseLimit(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

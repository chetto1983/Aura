package agui

import (
	"context"
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
type errConvStore struct{ err error }

func (e *errConvStore) Get(context.Context, string) (conversations.Conversation, error) {
	return conversations.Conversation{}, e.err
}

func (e *errConvStore) LoadHistory(context.Context, string) ([]llm.Message, error) {
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

func convAPIServer(t *testing.T, store ConversationStore) *httptest.Server {
	t.Helper()
	s := NewServer(&scriptedRunner{}, store, ServerConfig{})
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
	srv := convAPIServer(t, &errConvStore{err: errors.New("must-not-be-called")})

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
	srv := convAPIServer(t, &errConvStore{err: conversations.ErrTurnNotFound})
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

// TestParseLimit pins the ?limit= fallback: absent/non-numeric/non-positive → default,
// a positive value passes through.
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
	} {
		if got := parseLimit(tc.in); got != tc.want {
			t.Errorf("parseLimit(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

//go:build db_integration

// Integration tests for the D-09 / CHAT-05 branch REST adapter
// (conversations_branch_api.go) against a REAL Postgres-backed conversations.Store. The
// branch list/fork go through the real store (so the full-tree topology is exercised);
// the re-run streams through a scripted Runner whose TurnBranch records the leaf it was
// driven with — proving the edit/select routes drive LoadManagedHistoryForBranch(selected)
// (no-Resume re-run-from-a-point).
//
// Run via (the live stack must be up; derive the DSNs from POSTGRES_PASSWORD):
//
//	go test -tags db_integration ./internal/agui/ -run TestBranch -race
//
// No-skip-as-green: migratedPool's envOrSkip t.Fatals under $CI when the DSN is unset.
package agui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// newBranchAPIServer builds an AG-UI server over the real store + a scripted runner whose
// TurnBranch records the selected leaf. Returns the server + the runner for assertions.
func newBranchAPIServer(t *testing.T, store ConversationStore) (*httptest.Server, *scriptedRunner) {
	t.Helper()
	run := &scriptedRunner{events: textTurn("re-run reply")}
	s := NewServer(run, store, ServerConfig{})
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return srv, run
}

// seedBranchConversation creates a canonical linear conversation (system+user+assistant)
// with chained parent pointers and returns its id.
func seedBranchConversation(t *testing.T, store *conversations.Store) string {
	t.Helper()
	ctx := ownerCtx()
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := store.Create(ctx, conversations.CreateParams{ID: id, IdentityID: localIdentityID, Model: "test-model"}); err != nil {
		t.Fatalf("Create conversation: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(ownerCtx(), id) })
	for _, tp := range []conversations.AppendTurnParams{
		{ConversationID: id, Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{ConversationID: id, Seq: 2, Role: llm.RoleUser, Content: "first question"},
		{ConversationID: id, Seq: 3, Role: llm.RoleAssistant, Content: "first answer"},
	} {
		if err := store.AppendTurn(ctx, tp); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", tp.Seq, err)
		}
		if err := store.SetBranchPointers(ctx, id, tp.Seq, conversations.CanonicalBranchID, tp.Seq-1); err != nil {
			t.Fatalf("SetBranchPointers seq %d: %v", tp.Seq, err)
		}
	}
	return id
}

// TestBranchList_NonBranchedHasOneBranch asserts GET /branches reports exactly one branch
// (the canonical leaf) for a non-branched conversation — the picker stays hidden.
func TestBranchList_NonBranchedHasOneBranch(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	id := seedBranchConversation(t, store)
	srv, _ := newBranchAPIServer(t, store)

	code, body := getJSON(t, srv, "/api/conversations/"+id+"/branches")
	if code != http.StatusOK {
		t.Fatalf("GET branches status = %d, want 200: %s", code, body)
	}
	var items []branchItem
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode branches: %v (%s)", err, body)
	}
	if len(items) != 1 {
		t.Fatalf("branches = %d, want 1 (canonical leaf, picker hidden): %s", len(items), body)
	}
	if items[0].BranchSeq != 3 {
		t.Errorf("canonical leaf seq = %d, want 3", items[0].BranchSeq)
	}
}

// TestBranchEdit_ForksAndReRunsOverSelectedBranch asserts POST /edit forks a new sibling
// branch (the old branch stays queryable, full tree) AND re-runs over the FORKED leaf
// (TurnBranch driven with the new leaf — the re-run-from-a-point over the selected path).
func TestBranchEdit_ForksAndReRunsOverSelectedBranch(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	id := seedBranchConversation(t, store)
	srv, run := newBranchAPIServer(t, store)

	// Edit the user turn (seq 2) → fork a sibling + re-run; the response streams the turn.
	code, body := doReq(t, srv, http.MethodPost, "/api/conversations/"+id+"/edit",
		`{"diverge_seq":2,"role":"user","content":"edited question"}`)
	if code != http.StatusOK {
		t.Fatalf("POST edit status = %d, want 200 (SSE re-run): %s", code, body)
	}
	if !strings.Contains(body, "re-run reply") {
		t.Errorf("edit SSE body missing the re-run reply: %s", body)
	}

	// The re-run was driven over the FORKED branch leaf (not the canonical leaf 3).
	if run.gotBranchLeaf <= 3 {
		t.Fatalf("TurnBranch leaf = %d, want the forked leaf > 3 (re-run over the selected branch)", run.gotBranchLeaf)
	}

	// Full tree: BOTH branches are now queryable (original canonical leaf + forked sibling).
	branches, err := store.ListBranches(ownerCtx(), id)
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("post-edit branches = %d, want 2 (original + forked sibling)", len(branches))
	}
}

// TestBranchSelect_ReRunsOverSelectedLeaf asserts POST /branches/{seq}/select re-runs over
// the SELECTED leaf (TurnBranch driven with that leaf) and a non-numeric branch seq → 404.
func TestBranchSelect_ReRunsOverSelectedLeaf(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	id := seedBranchConversation(t, store)
	srv, run := newBranchAPIServer(t, store)

	code, body := doReq(t, srv, http.MethodPost, "/api/conversations/"+id+"/branches/3/select", "")
	if code != http.StatusOK {
		t.Fatalf("POST select status = %d, want 200 (SSE re-run): %s", code, body)
	}
	if run.gotBranchLeaf != 3 {
		t.Errorf("TurnBranch leaf = %d, want 3 (the selected leaf)", run.gotBranchLeaf)
	}

	// A non-numeric branch seq is a clean 404 (never a 500).
	code, _ = doReq(t, srv, http.MethodPost, "/api/conversations/"+id+"/branches/not-a-seq/select", "")
	if code != http.StatusNotFound {
		t.Errorf("non-numeric branch seq status = %d, want 404", code)
	}
}

// TestBranchEdit_UnknownTurnIs404 asserts editing a non-existent turn seq → 404 (no re-run).
func TestBranchEdit_UnknownTurnIs404(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	id := seedBranchConversation(t, store)
	srv, run := newBranchAPIServer(t, store)

	code, _ := doReq(t, srv, http.MethodPost, "/api/conversations/"+id+"/edit",
		`{"diverge_seq":999,"role":"user","content":"ghost"}`)
	if code != http.StatusNotFound {
		t.Fatalf("edit unknown turn status = %d, want 404", code)
	}
	if run.gotBranchLeaf != 0 {
		t.Errorf("a 404 edit still drove a re-run (leaf=%d); want no re-run", run.gotBranchLeaf)
	}

	// A malformed conversation id → 404 before the store round-trip.
	code, _ = doReq(t, srv, http.MethodPost, "/api/conversations/not-a-uuid/edit",
		`{"diverge_seq":2,"content":"x"}`)
	if code != http.StatusNotFound {
		t.Errorf("malformed conv id status = %d, want 404", code)
	}
}

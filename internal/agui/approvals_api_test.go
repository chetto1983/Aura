//go:build db_integration

// Integration tests for the APRV-01 cross-thread approval READ
// (approvals_api.go handleListApprovals) against a REAL Postgres-backed
// askuser.Store. The thin adapter has no business logic, so the integration value
// is the real cross-thread aggregation round-trip: pendings from MULTIPLE
// conversations surface in one queue, and the surfaced question is sanitized (V7).
// The resolve verb→action mapping + edge cases are covered DB-free in
// approvals_api_unit_test.go via the scriptedRunner double.
//
//	go test -tags db_integration ./internal/agui/ -run TestApprovalsAPI -race
//
// No-skip-as-green: migratedPool's envOrSkip t.Fatals under $CI when the DSN is unset.
package agui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/redact"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newApprovalsAPIServer builds an AG-UI server over a scripted runner + the real
// askuser store wired as the ApprovalStore, serving its Mux (which carries the
// /api/approvals routes). The conversation store is the real one (the approvals read
// does not touch it, but NewServer requires a ConversationStore).
func newApprovalsAPIServer(t *testing.T, pool *pgxpool.Pool, run Runner, approvals ApprovalStore) *httptest.Server {
	t.Helper()
	conv := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	s := NewServer(run, conv, ServerConfig{})
	s.SetApprovalStore(approvals)
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return srv
}

// seedConversationRow inserts a throwaway conversation (the paused_states FK parent)
// via direct SQL and registers cascade cleanup. Returns its UUID.
func seedConversationRow(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	// aura.conversations is fail-closed (migration 0089): the seed must carry the owner, and
	// so must the cleanup — a DELETE with no identity matches zero rows and reports success,
	// which would leave the row behind without failing anything.
	seedAsOwner(t, pool, localIdentityID,
		"INSERT INTO aura.conversations (id, identity_id, model) VALUES ($1, $2, 'test-model')",
		id, localIdentityID)
	t.Cleanup(func() {
		_ = db.WithIdentityTxRaw(ownerCtx(), pool, localIdentityID, func(tx pgx.Tx) error {
			_, e := tx.Exec(ownerCtx(), "DELETE FROM aura.conversations WHERE id = $1", id)
			return e
		})
	})
	return id
}

// seedPause inserts one conversation + one pending pause and returns the pause token.
func seedPause(t *testing.T, store *askuser.Store, pool *pgxpool.Pool, kind, question string, priority int) (convID, token string) {
	t.Helper()
	convID = seedConversationRow(t, pool)
	token = uuid.Must(uuid.NewV7()).String()
	if err := store.Insert(ownerCtx(), askuser.InsertParams{
		Token:          token,
		ConversationID: convID,
		Kind:           kind,
		Question:       question,
		Priority:       priority,
		ToolCallID:     "tc-" + token[:8],
	}); err != nil {
		t.Fatalf("Insert pause: %v", err)
	}
	return convID, token
}

// TestApprovalsAPI_ListCrossThread asserts GET /api/approvals returns the cross-thread
// pending queue (pendings from MULTIPLE conversations) projected to JSON, and that the
// surfaced question is run through SanitizeString (V7 / T-25-06).
func TestApprovalsAPI_ListCrossThread(t *testing.T) {
	pool := migratedPool(t)
	store := askuser.New(pool)

	convA, tokA := seedPause(t, store, pool, "approval", "approve from A?", 5)
	convB, tokB := seedPause(t, store, pool, "clarification", "clarify from B?", 5)

	// A question echoing a DSN must be redacted in the list projection (V7).
	convC := seedConversationRow(t, pool)
	tokC := uuid.Must(uuid.NewV7()).String()
	if err := store.Insert(ownerCtx(), askuser.InsertParams{
		Token:          tokC,
		ConversationID: convC,
		Kind:           "approval",
		Question:       "connect to postgres://user:supersecret@db:5432/aura ?",
		Priority:       5,
		ToolCallID:     "tc-secret",
	}); err != nil {
		t.Fatalf("Insert secret pause: %v", err)
	}

	srv := newApprovalsAPIServer(t, pool, &scriptedRunner{}, store)

	resp, err := http.Get(srv.URL + "/api/approvals")
	if err != nil {
		t.Fatalf("GET /api/approvals: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var items []struct {
		Token          string `json:"token"`
		ConversationID string `json:"conversation_id"`
		Kind           string `json:"kind"`
		Question       string `json:"question"`
		Priority       int    `json:"priority"`
		Source         string `json:"source"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode items: %v", err)
	}

	byToken := map[string]string{} // token -> conversation_id
	question := map[string]string{}
	for _, it := range items {
		byToken[it.Token] = it.ConversationID
		question[it.Token] = it.Question
	}
	if byToken[tokA] == "" || byToken[tokB] == "" {
		t.Fatalf("cross-thread list missing seeded pendings: A=%q B=%q", byToken[tokA], byToken[tokB])
	}
	if byToken[tokA] != convA || byToken[tokB] != convB {
		t.Errorf("conversation ids mismatch: A=%s (want %s), B=%s (want %s)", byToken[tokA], convA, byToken[tokB], convB)
	}
	if byToken[tokA] == byToken[tokB] {
		t.Errorf("list did not aggregate across conversations (both = %s)", byToken[tokA])
	}
	// V7: the secret-bearing question is redacted on the wire.
	if strings.Contains(question[tokC], "supersecret") {
		t.Errorf("question leaked a secret unredacted: %q", question[tokC])
	}
	if !strings.Contains(question[tokC], redact.Placeholder) {
		t.Errorf("question was not sanitized (no %s marker): %q", redact.Placeholder, question[tokC])
	}
}

// Unit tier (no build tag) for the D-09 / CHAT-05 branch path walk (store_branch.go),
// driven through the in-memory sqlc.DBTX fake (store_fakedbtx_test.go) so the
// projection + error-classification + sidecar-rehydration branches run under plain
// `go test` without a live Postgres.
package conversations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// sqlcBranchPathRowFixture builds a ListTurnsByBranchPathRow with every column non-zero
// so branchPathRowAsSeqRow's field copy is fully exercised.
func sqlcBranchPathRowFixture(conv pgtype.UUID) sqlc.ListTurnsByBranchPathRow {
	return sqlc.ListTurnsByBranchPathRow{
		ConversationID:     conv,
		Seq:                2,
		Role:               "assistant",
		Content:            pgtype.Text{String: "body", Valid: true},
		ContentSidecarPath: pgtype.Text{String: "/tmp/x.content", Valid: true},
		ToolCallID:         pgtype.Text{String: "call_1", Valid: true},
		ToolCalls:          []byte(`[]`),
		InputTokens:        3,
		OutputTokens:       4,
		CachedTokens:       1,
	}
}

// TestCanonicalBranchLeaf_FakeSuccess projects the scalar leaf seq.
func TestCanonicalBranchLeaf_FakeSuccess(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7()).String()
	fake := &fakeDBTX{queryRowVal: &fakeRow{values: []any{int32(7)}}}
	s := fakeStore(t, fake)
	leaf, err := s.CanonicalBranchLeaf(context.Background(), id)
	if err != nil {
		t.Fatalf("CanonicalBranchLeaf: %v", err)
	}
	if leaf != 7 {
		t.Errorf("leaf: want 7, got %d", leaf)
	}
}

// TestCanonicalBranchLeaf_FakeBadID rejects a malformed conversation id at the boundary.
func TestCanonicalBranchLeaf_FakeBadID(t *testing.T) {
	t.Parallel()
	s := fakeStore(t, &fakeDBTX{})
	if _, err := s.CanonicalBranchLeaf(context.Background(), "not-a-uuid"); err == nil {
		t.Fatal("CanonicalBranchLeaf with bad id: want error, got nil")
	}
}

// TestCanonicalBranchLeaf_FakeDBError wraps a failing scan.
func TestCanonicalBranchLeaf_FakeDBError(t *testing.T) {
	t.Parallel()
	boom := errors.New("leaf query failed")
	fake := &fakeDBTX{queryRowVal: &fakeRow{scanErr: boom}}
	s := fakeStore(t, fake)
	if _, err := s.CanonicalBranchLeaf(context.Background(), uuid.Must(uuid.NewV7()).String()); !errors.Is(err, boom) {
		t.Errorf("CanonicalBranchLeaf DB error must propagate: %v", err)
	}
}

// TestLoadBranchHistory_FakeInlinePath reconstructs llm.Messages from an inline
// branch-path result set (root->leaf order).
func TestLoadBranchHistory_FakeInlinePath(t *testing.T) {
	t.Parallel()
	_, conv := mustUUID(t)
	calls := []byte(`[{"id":"call_1","type":"function","function":{"name":"text_response","arguments":"{}"}}]`)
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		turnRowValues(conv, 1, "system", "you are aura", "", "", nil),
		turnRowValues(conv, 2, "user", "hi", "", "", nil),
		turnRowValues(conv, 3, "assistant", "", "", "", calls),
		turnRowValues(conv, 4, "tool", "ok", "", "call_1", nil),
	}}}
	s := fakeStore(t, fake)
	hist, err := s.LoadBranchHistory(context.Background(), uuid.UUID(conv.Bytes).String(), 4)
	if err != nil {
		t.Fatalf("LoadBranchHistory: %v", err)
	}
	if len(hist) != 4 {
		t.Fatalf("want 4 messages, got %d", len(hist))
	}
	if hist[0].Role != llm.RoleSystem || hist[3].Role != llm.RoleTool || hist[3].ToolCallID != "call_1" {
		t.Errorf("branch reconstruction wrong: %+v", hist)
	}
}

// TestLoadBranchHistory_FakeSidecarRehydration reads spilled content back from disk.
func TestLoadBranchHistory_FakeSidecarRehydration(t *testing.T) {
	t.Parallel()
	convStr, conv := mustUUID(t)
	s := fakeStore(t, nil) // queryRows set below after we know the sidecar path
	dir := filepath.Join(s.runDir, "conversations", convStr)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir sidecar: %v", err)
	}
	sidecar := filepath.Join(dir, "2.content")
	if err := os.WriteFile(sidecar, []byte("spilled body"), 0o600); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	s.q = fakeStore(t, &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		turnRowValues(conv, 1, "system", "you are aura", "", "", nil),
		turnRowValues(conv, 2, "user", "", sidecar, "", nil), // content NULL, sidecar set
	}}}).q

	hist, err := s.LoadBranchHistory(context.Background(), convStr, 2)
	if err != nil {
		t.Fatalf("LoadBranchHistory: %v", err)
	}
	if len(hist) != 2 || hist[1].Content != "spilled body" {
		t.Errorf("sidecar rehydration failed: %+v", hist)
	}
}

// TestLoadBranchHistory_FakeSidecarMissing surfaces a read error for an absent sidecar.
func TestLoadBranchHistory_FakeSidecarMissing(t *testing.T) {
	t.Parallel()
	convStr, conv := mustUUID(t)
	missing := filepath.Join(t.TempDir(), "nope.content")
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		turnRowValues(conv, 1, "user", "", missing, "", nil),
	}}}
	s := fakeStore(t, fake)
	if _, err := s.LoadBranchHistory(context.Background(), convStr, 1); err == nil || !strings.Contains(err.Error(), "read sidecar") {
		t.Errorf("want read-sidecar error, got %v", err)
	}
}

// TestLoadBranchHistory_FakeBadToolCalls surfaces a tool_calls decode failure.
func TestLoadBranchHistory_FakeBadToolCalls(t *testing.T) {
	t.Parallel()
	convStr, conv := mustUUID(t)
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		turnRowValues(conv, 1, "assistant", "", "", "", []byte("{not json")),
	}}}
	s := fakeStore(t, fake)
	if _, err := s.LoadBranchHistory(context.Background(), convStr, 1); err == nil {
		t.Fatal("LoadBranchHistory with bad tool_calls: want error, got nil")
	}
}

// TestLoadBranchHistory_FakeBadID rejects a malformed id before the query.
func TestLoadBranchHistory_FakeBadID(t *testing.T) {
	t.Parallel()
	s := fakeStore(t, &fakeDBTX{})
	if _, err := s.LoadBranchHistory(context.Background(), "bad", 1); err == nil {
		t.Fatal("LoadBranchHistory with bad id: want error, got nil")
	}
}

// TestLoadBranchHistory_FakeQueryError wraps a failing Query.
func TestLoadBranchHistory_FakeQueryError(t *testing.T) {
	t.Parallel()
	boom := errors.New("path query failed")
	fake := &fakeDBTX{queryErr: boom}
	s := fakeStore(t, fake)
	if _, err := s.LoadBranchHistory(context.Background(), uuid.Must(uuid.NewV7()).String(), 1); !errors.Is(err, boom) {
		t.Errorf("LoadBranchHistory query error must propagate: %v", err)
	}
}

// TestSetBranchPointers_FakeSuccess sets a branch turn with a real parent.
func TestSetBranchPointers_FakeSuccess(t *testing.T) {
	t.Parallel()
	s := fakeStore(t, &fakeDBTX{})
	if err := s.SetBranchPointers(context.Background(), uuid.Must(uuid.NewV7()).String(), 3, uuid.Must(uuid.NewV7()), 2); err != nil {
		t.Errorf("SetBranchPointers: %v", err)
	}
}

// TestSetBranchPointers_FakeRootNullParent sets a branch root (parentSeq <= 0 => NULL).
func TestSetBranchPointers_FakeRootNullParent(t *testing.T) {
	t.Parallel()
	s := fakeStore(t, &fakeDBTX{})
	if err := s.SetBranchPointers(context.Background(), uuid.Must(uuid.NewV7()).String(), 1, CanonicalBranchID, 0); err != nil {
		t.Errorf("SetBranchPointers root: %v", err)
	}
}

// TestSetBranchPointers_FakeBadID rejects a malformed id.
func TestSetBranchPointers_FakeBadID(t *testing.T) {
	t.Parallel()
	s := fakeStore(t, &fakeDBTX{})
	if err := s.SetBranchPointers(context.Background(), "bad", 1, CanonicalBranchID, 0); err == nil {
		t.Fatal("SetBranchPointers with bad id: want error, got nil")
	}
}

// TestSetBranchPointers_FakeDBError wraps a failing Exec.
func TestSetBranchPointers_FakeDBError(t *testing.T) {
	t.Parallel()
	boom := errors.New("update failed")
	s := fakeStore(t, &fakeDBTX{execErr: boom})
	if err := s.SetBranchPointers(context.Background(), uuid.Must(uuid.NewV7()).String(), 1, CanonicalBranchID, 0); !errors.Is(err, boom) {
		t.Errorf("SetBranchPointers DB error must propagate: %v", err)
	}
}

// TestListBranches_FakeProjection projects the leaf rows onto []Branch (seq + branch +
// parent), exercising the valid/invalid pgtype branches.
func TestListBranches_FakeProjection(t *testing.T) {
	t.Parallel()
	_, conv := mustUUID(t)
	bid := uuid.Must(uuid.NewV7())
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		// canonical leaf: all-zero branch, root parent (NULL).
		{int32(3), pgtype.UUID{Bytes: CanonicalBranchID, Valid: true}, pgtype.Int4{}},
		// forked sibling: fresh branch, parent seq 1.
		{int32(4), pgtype.UUID{Bytes: bid, Valid: true}, pgtype.Int4{Int32: 1, Valid: true}},
	}}}
	s := fakeStore(t, fake)
	branches, err := s.ListBranches(context.Background(), uuid.UUID(conv.Bytes).String())
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("want 2 branches, got %d", len(branches))
	}
	if branches[0].LeafSeq != 3 || branches[0].BranchID != CanonicalBranchID || branches[0].ParentSeq != 0 {
		t.Errorf("canonical branch projection wrong: %+v", branches[0])
	}
	if branches[1].LeafSeq != 4 || branches[1].BranchID != bid || branches[1].ParentSeq != 1 {
		t.Errorf("forked branch projection wrong: %+v", branches[1])
	}
}

// TestListBranches_FakeBadID rejects a malformed id before the query.
func TestListBranches_FakeBadID(t *testing.T) {
	t.Parallel()
	s := fakeStore(t, &fakeDBTX{})
	if _, err := s.ListBranches(context.Background(), "bad"); err == nil {
		t.Fatal("ListBranches with bad id: want error, got nil")
	}
}

// TestListBranches_FakeQueryError wraps a failing Query.
func TestListBranches_FakeQueryError(t *testing.T) {
	t.Parallel()
	boom := errors.New("leaves query failed")
	s := fakeStore(t, &fakeDBTX{queryErr: boom})
	if _, err := s.ListBranches(context.Background(), uuid.Must(uuid.NewV7()).String()); !errors.Is(err, boom) {
		t.Errorf("ListBranches query error must propagate: %v", err)
	}
}

// TestForkBranch_FakeBadID rejects a malformed id at the boundary (before any tx).
func TestForkBranch_FakeBadID(t *testing.T) {
	t.Parallel()
	s := fakeStore(t, &fakeDBTX{})
	if _, _, err := s.ForkBranch(context.Background(), "bad", 2, llm.RoleUser, "x"); err == nil {
		t.Fatal("ForkBranch with bad id: want error, got nil")
	}
}

// TestBranchPathRowAsSeqRow asserts the field-identical adapter copies all 11 columns.
func TestBranchPathRowAsSeqRow(t *testing.T) {
	t.Parallel()
	_, conv := mustUUID(t)
	src := sqlcBranchPathRowFixture(conv)
	got := branchPathRowAsSeqRow(src)
	if got.ConversationID != src.ConversationID || got.Seq != src.Seq || got.Role != src.Role ||
		got.Content != src.Content || got.ContentSidecarPath != src.ContentSidecarPath ||
		got.ToolCallID != src.ToolCallID || got.InputTokens != src.InputTokens ||
		got.OutputTokens != src.OutputTokens || got.CachedTokens != src.CachedTokens {
		t.Errorf("adapter lost a field: src=%+v got=%+v", src, got)
	}
}

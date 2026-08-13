//go:build db_integration

// Integration tests for the D-09 / CHAT-05 branch path walk (store_branch.go).
// Requires the live Postgres stack with migrations applied through 0017:
//
//	make db-up && aura db migrate
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/conversations -run TestBranch -count=1
//
// No-skip-as-green: envOrSkip (store_test.go) t.Fatals under $CI when the DSN is unset.
package conversations

import (
	"fmt"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// chainCanonical sets parent_seq = seq-1 on every turn of conv through SetBranchPointers,
// modelling what migration 0017 backfilled onto the rows that pre-dated it — a state the
// live database still holds for those rows.
//
// It is no longer the ONLY way to reach that shape: AppendTurn now derives the same chain
// in its INSERT. Until it did, this helper was the only thing producing it, and every
// branch test seeding through it therefore asserted against a state production never
// wrote — which is how a total context loss on edit/regenerate stayed green. The test
// that seeds WITHOUT it is TestBranchFork_ProductionAppendChainsParentSeq.
func chainCanonical(t *testing.T, s *Store, convID string, seqs ...int) {
	t.Helper()
	for _, seq := range seqs {
		parent := seq - 1 // 0 for the root => NULL parent
		if err := s.SetBranchPointers(ownerCtx(), convID, seq, CanonicalBranchID, parent); err != nil {
			t.Fatalf("chain canonical seq %d: %v", seq, err)
		}
	}
}

// TestBranchPath_DeterministicWalk persists a canonical linear chain and asserts the
// leaf->root walk returns the root->leaf ordered list AND two consecutive calls are
// byte-identical (the LoadHistory byte-identity contract extended to the path walk).
func TestBranchPath_DeterministicWalk(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := ownerCtx()

	turns := []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "hello"},
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, Content: "hi there"},
	}
	for _, tp := range turns {
		if err := s.AppendTurn(ctx, tp); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", tp.Seq, err)
		}
	}
	chainCanonical(t, s, convID, 1, 2, 3)

	leaf, err := s.CanonicalBranchLeaf(ctx, convID)
	if err != nil {
		t.Fatalf("CanonicalBranchLeaf: %v", err)
	}
	if leaf != 3 {
		t.Fatalf("canonical leaf: want 3, got %d", leaf)
	}

	h1, err := s.LoadBranchHistory(ctx, convID, leaf)
	if err != nil {
		t.Fatalf("LoadBranchHistory#1: %v", err)
	}
	h2, err := s.LoadBranchHistory(ctx, convID, leaf)
	if err != nil {
		t.Fatalf("LoadBranchHistory#2: %v", err)
	}
	if len(h1) != 3 {
		t.Fatalf("branch history: want 3 turns, got %d", len(h1))
	}
	// root->leaf order
	if h1[0].Role != llm.RoleSystem || h1[0].Content != "you are aura" {
		t.Errorf("branch[0] not the root system turn: %+v", h1[0])
	}
	if h1[2].Role != llm.RoleAssistant || h1[2].Content != "hi there" {
		t.Errorf("branch[2] not the leaf assistant turn: %+v", h1[2])
	}
	if fmt.Sprintf("%#v", h1) != fmt.Sprintf("%#v", h2) {
		t.Errorf("branch walk not byte-identical across calls:\n#1 %#v\n#2 %#v", h1, h2)
	}
}

// TestBranchPath_NonBranchedByteIdenticalToLinear is the regression guard: for a
// canonical-chained (non-branched) conversation, LoadBranchHistory(canonical leaf) is
// byte-identical to LoadHistory (the pre-0017 linear path).
func TestBranchPath_NonBranchedByteIdenticalToLinear(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := ownerCtx()

	calls := []byte(`[{"id":"call_1","type":"function","function":{"name":"text_response","arguments":"{}"}}]`)
	turns := []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "do the thing"},
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, ToolCalls: calls},
		{ConversationID: convID, Seq: 4, Role: llm.RoleTool, Content: "tool ok", ToolCallID: "call_1"},
		{ConversationID: convID, Seq: 5, Role: llm.RoleAssistant, Content: "done"},
	}
	for _, tp := range turns {
		if err := s.AppendTurn(ctx, tp); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", tp.Seq, err)
		}
	}
	chainCanonical(t, s, convID, 1, 2, 3, 4, 5)

	linear, err := s.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	leaf, err := s.CanonicalBranchLeaf(ctx, convID)
	if err != nil {
		t.Fatalf("CanonicalBranchLeaf: %v", err)
	}
	branch, err := s.LoadBranchHistory(ctx, convID, leaf)
	if err != nil {
		t.Fatalf("LoadBranchHistory: %v", err)
	}
	if fmt.Sprintf("%#v", linear) != fmt.Sprintf("%#v", branch) {
		t.Errorf("non-branched branch walk != linear LoadHistory:\nlinear %#v\nbranch %#v", linear, branch)
	}
	// repairToolMessagePairs preserved the valid assistant(tool_calls)->tool group.
	if len(branch) != 5 {
		t.Fatalf("branch history: want 5 messages (valid tool group preserved), got %d", len(branch))
	}
	if branch[3].Role != llm.RoleTool || branch[3].ToolCallID != "call_1" {
		t.Errorf("tool pairing broken along the branch path: %+v", branch[3])
	}
}

// TestBranchPath_ProtectedHeadByteIdenticalAcrossBranches is the Pitfall 3 invariant:
// two sibling branches sharing the same head (system seq=1 + the first user turn)
// produce byte-identical leading messages — only the body diverges per branch.
func TestBranchPath_ProtectedHeadByteIdenticalAcrossBranches(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := ownerCtx()

	// Shared head: seq 1 (system) -> seq 2 (user). Two assistant branches fork off the
	// user turn (seq 2): branch A leaf = seq 3, branch B leaf = seq 4, both parent=2.
	head := []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "answer me"},
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, Content: "answer A"},
		{ConversationID: convID, Seq: 4, Role: llm.RoleAssistant, Content: "answer B"},
	}
	for _, tp := range head {
		if err := s.AppendTurn(ctx, tp); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", tp.Seq, err)
		}
	}
	// seq 1 root, seq 2 parent=1 on the canonical branch.
	chainCanonical(t, s, convID, 1, 2)
	branchA := uuid.Must(uuid.NewV7())
	branchB := uuid.Must(uuid.NewV7())
	if err := s.SetBranchPointers(ctx, convID, 3, branchA, 2); err != nil {
		t.Fatalf("set branch A pointers: %v", err)
	}
	if err := s.SetBranchPointers(ctx, convID, 4, branchB, 2); err != nil {
		t.Fatalf("set branch B pointers: %v", err)
	}

	histA, err := s.LoadBranchHistory(ctx, convID, 3)
	if err != nil {
		t.Fatalf("LoadBranchHistory A: %v", err)
	}
	histB, err := s.LoadBranchHistory(ctx, convID, 4)
	if err != nil {
		t.Fatalf("LoadBranchHistory B: %v", err)
	}
	if len(histA) != 3 || len(histB) != 3 {
		t.Fatalf("branch lengths: A=%d B=%d, want 3 each", len(histA), len(histB))
	}
	// Protected head (messages[0] system + messages[1] user) byte-identical across branches.
	if fmt.Sprintf("%#v", histA[0]) != fmt.Sprintf("%#v", histB[0]) {
		t.Errorf("messages[0] differs across branches:\nA %#v\nB %#v", histA[0], histB[0])
	}
	if fmt.Sprintf("%#v", histA[1]) != fmt.Sprintf("%#v", histB[1]) {
		t.Errorf("messages[1] differs across branches:\nA %#v\nB %#v", histA[1], histB[1])
	}
	// Bodies diverge.
	if histA[2].Content == histB[2].Content {
		t.Errorf("branch bodies must differ: both %q", histA[2].Content)
	}
	if histA[2].Content != "answer A" || histB[2].Content != "answer B" {
		t.Errorf("wrong branch leaves: A=%q B=%q", histA[2].Content, histB[2].Content)
	}
}

// TestBranchPath_EmptyLeafYieldsEmpty asserts a leaf seq of 0 (no canonical leaf /
// empty conversation) yields an empty path rather than an error.
func TestBranchPath_EmptyLeafYieldsEmpty(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := ownerCtx()

	leaf, err := s.CanonicalBranchLeaf(ctx, convID)
	if err != nil {
		t.Fatalf("CanonicalBranchLeaf on empty: %v", err)
	}
	if leaf != 0 {
		t.Fatalf("empty conversation canonical leaf: want 0, got %d", leaf)
	}
	hist, err := s.LoadBranchHistory(ctx, convID, leaf)
	if err != nil {
		t.Fatalf("LoadBranchHistory on empty: %v", err)
	}
	if len(hist) != 0 {
		t.Errorf("empty conversation branch history: want 0 messages, got %d", len(hist))
	}
}

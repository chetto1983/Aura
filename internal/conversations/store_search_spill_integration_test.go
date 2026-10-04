//go:build db_integration

package conversations

import (
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// TestSearchSpilledContentExcluded proves the LOOP-10 / D-10 boundary against live
// pg_trgm: a >cap turn spills (content column NULL + sidecar path set) and is
// therefore ABSENT from SearchConversationTurns, while an inline control turn carrying
// a distinct token IS found.
func TestSearchSpilledContentExcluded(t *testing.T) {
	pool := migratedPool(t)
	runDir := t.TempDir()
	s := New(pool, Config{RunDir: runDir, TurnCapBytes: 64}) // tiny cap → force a spill
	convID := newConversation(t, s)
	ctx := ownerCtx()

	// Random (v4) UUIDs, NOT v7: the search is a pg_trgm word-similarity match, and v7 is
	// time-ordered — two tokens minted microseconds apart share the same ~48-bit timestamp
	// prefix (e.g. "019faa3d2dcb…"). That shared run of hex once made spilltok<ts…> and
	// ctrltok<ts…> similar enough to match, so a search for the spilled token
	// intermittently returned the inline control turn (flaky LOOP-10 failure observed
	// 2026-07-28). v4 has no shared prefix, so the two markers stay well under threshold.
	uniq := func(prefix string) string {
		return prefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	spilledToken := uniq("spilltok")
	controlToken := uniq("ctrltok")

	// A >cap turn carrying spilledToken → spills to a sidecar, content=NULL.
	spilledBody := spilledToken + " " + strings.Repeat("x", 256)
	if err := s.AppendTurn(ctx, AppendTurnParams{
		ConversationID: convID, Seq: 1, Role: llm.RoleUser, Content: spilledBody,
	}); err != nil {
		t.Fatalf("AppendTurn spilled: %v", err)
	}
	// An inline control turn carrying controlToken → content stored, searchable.
	if err := s.AppendTurn(ctx, AppendTurnParams{
		ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: controlToken,
	}); err != nil {
		t.Fatalf("AppendTurn control: %v", err)
	}

	// Precondition: seq 1 actually spilled (content NULL, sidecar path + file present).
	var content, path *string
	if err := pool.QueryRow(ctx,
		"SELECT content, content_sidecar_path FROM aura.conversation_turns WHERE conversation_id=$1 AND seq=1",
		convID,
	).Scan(&content, &path); err != nil {
		t.Fatalf("read spilled turn: %v", err)
	}
	if content != nil {
		t.Fatalf("precondition: seq 1 must have content NULL (spilled), got %q", *content)
	}
	if path == nil {
		t.Fatal("precondition: seq 1 must have content_sidecar_path set (spilled)")
	}
	if _, err := os.Stat(*path); err != nil {
		t.Fatalf("precondition: spilled sidecar file must exist: %v", err)
	}

	// The spilled token is NOT searchable — content=NULL excludes it from the search.
	spilledHits, err := s.SearchConversationTurns(ctx, spilledToken, 10)
	if err != nil {
		t.Fatalf("search spilled: %v", err)
	}
	if len(spilledHits) != 0 {
		t.Errorf("LOOP-10: spilled content must be excluded from search, got %d hits: %+v",
			len(spilledHits), spilledHits)
	}

	// The inline control token IS searchable and returns the control turn (seq 2).
	controlHits, err := s.SearchConversationTurns(ctx, controlToken, 10)
	if err != nil {
		t.Fatalf("search control: %v", err)
	}
	if !containsSearchResult(controlHits, convID) {
		t.Errorf("inline control turn must be found by search: %+v", controlHits)
	}
	for _, h := range controlHits {
		if h.ConversationID == convID && h.Seq == 1 {
			t.Errorf("spilled turn (seq 1) must never surface in search: %+v", h)
		}
	}

}

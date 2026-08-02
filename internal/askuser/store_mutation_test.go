//go:build db_integration

// Adversarial boundary/wrap-pinning tests added for the Phase-4 mutation gate
// (CLAUDE.md ≥0.70). These pin behaviors that store_test.go's coverage left
// unmeasured: the ListRecent limit clamp, the ResumedAnswer round-trip on a
// resolved row, ListRecent row accumulation, the MarkResumedBatch already-resumed
// and encode-fail branches, and the DB-error wrap message of each method. They
// share the migratedPool/newConversation/insertPause harness from store_test.go.
package askuser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestListRecent_LimitClamp pins the `if limit <= 0 { limit = 50 }` default. With
// rows present, ListRecent(0) and ListRecent(-5) must return them (the clamp turns
// the non-positive limit into 50). The surviving mutants are (a) removing
// `limit = 50` (leaves limit=0 → SQL LIMIT 0 → zero rows) and (b) `<=`→`<` (limit=0
// stays 0 → zero rows). Asserting a non-empty result on a 0 / negative limit kills
// both. An explicit small limit then proves the clamp does NOT override a real cap.
func TestListRecent_LimitClamp(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)
	ctx := ownerCtx()

	mine := map[string]bool{}
	for i := 0; i < 3; i++ {
		mine[insertPause(t, s, convID, "approval", "q", 0)] = true
	}

	countMine := func(limit int) int {
		recs, err := s.ListRecent(ctx, limit)
		if err != nil {
			t.Fatalf("ListRecent(%d): %v", limit, err)
		}
		n := 0
		for _, r := range recs {
			if mine[r.Token] {
				n++
			}
		}
		return n
	}

	if got := countMine(0); got != 3 {
		t.Fatalf("ListRecent(0) must clamp to 50 and return all 3 seeded rows, got %d", got)
	}
	if got := countMine(-5); got != 3 {
		t.Fatalf("ListRecent(-5) must clamp to 50 and return all 3 seeded rows, got %d", got)
	}

	// An explicit positive limit must NOT be clamped: limit=1 returns at most 1 row
	// total (newest-first), proving the branch only fires on a non-positive limit.
	recs, err := s.ListRecent(ctx, 1)
	if err != nil {
		t.Fatalf("ListRecent(1): %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("ListRecent(1) must honor the explicit limit, got %d rows", len(recs))
	}
}

// TestListRecent_ResolvedAnswerRoundTrip pins two ListRecent survivors: removing
// `out = append(out, rec)` (would return zero rows) and removing
// `rec.ResumedAnswer = ans.Content` (a resolved row would lose its content). It
// resolves a pause with a known content, then asserts ListRecent surfaces that
// exact content on the matching record and marks it Resumed, while a still-pending
// row carries an empty ResumedAnswer.
func TestListRecent_ResolvedAnswerRoundTrip(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)
	ctx := ownerCtx()

	const content = "shipped-it-2026"
	resolvedTok := insertPause(t, s, convID, "approval", "deploy?", 0)
	if err := s.MarkResumed(ctx, resolvedTok, ResumeAnswer{Action: ActionAccept, Content: content}); err != nil {
		t.Fatalf("MarkResumed: %v", err)
	}
	pendingTok := insertPause(t, s, convID, "clarification", "still?", 0)

	recs, err := s.ListRecent(ctx, 50)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}

	var sawResolved, sawPending bool
	for _, r := range recs {
		switch r.Token {
		case resolvedTok:
			sawResolved = true
			if !r.Resumed {
				t.Errorf("resolved row must report Resumed=true")
			}
			if r.ResumedAnswer != content {
				t.Errorf("resolved row must carry the persisted answer content %q, got %q", content, r.ResumedAnswer)
			}
		case pendingTok:
			sawPending = true
			if r.Resumed {
				t.Errorf("pending row must report Resumed=false")
			}
			if r.ResumedAnswer != "" {
				t.Errorf("pending row must carry an empty ResumedAnswer, got %q", r.ResumedAnswer)
			}
		}
	}
	if !sawResolved || !sawPending {
		t.Fatalf("ListRecent must return both seeded rows (resolved=%v pending=%v)", sawResolved, sawPending)
	}
}

func TestListRecent_InvalidResumedAnswerErrors(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)
	ctx := ownerCtx()

	tok := insertPause(t, s, convID, "approval", "deploy?", 0)
	if err := s.MarkResumed(ctx, tok, ResumeAnswer{Action: ActionAccept, Content: "ok"}); err != nil {
		t.Fatalf("MarkResumed: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE aura.paused_states SET resumed_answer = '42'::jsonb WHERE token = $1", tok); err != nil {
		t.Fatalf("corrupt resumed_answer: %v", err)
	}

	_, err := s.ListRecent(ctx, 50)
	if err == nil {
		t.Fatal("ListRecent with non-object resumed_answer: want error, got nil")
	}
	if !strings.Contains(err.Error(), "decode resumed_answer") {
		t.Fatalf("ListRecent error should name resumed_answer decode, got %v", err)
	}
}

// TestMarkResumedBatch_InvalidActionWrap pins the batch encodeAnswer wrap: a bad
// action inside a batch fails encoding (ErrInvalidAnswer) with the
// "mark resumed batch <token>:" context, before any row is written, and rolls back.
func TestMarkResumedBatch_InvalidActionWrap(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)
	ctx := ownerCtx()

	good := insertPause(t, s, convID, "approval", "a", 0)

	err := s.MarkResumedBatch(ctx, map[string]ResumeAnswer{good: {Action: "bogus"}})
	if !errors.Is(err, ErrInvalidAnswer) {
		t.Fatalf("batch with a bad action: want ErrInvalidAnswer, got %v", err)
	}
	if !strings.HasPrefix(err.Error(), "mark resumed batch "+good+":") {
		t.Fatalf("batch encode-fail wrap: want prefix %q, got %q", "mark resumed batch "+good+":", err.Error())
	}
	if n := countPending(t, pool, convID); n != 1 {
		t.Errorf("encode failure must roll back, leaving the row pending, got %d", n)
	}
}

// TestMarkResumedBatch_BadTokenWrap pins the batch parseUUID wrap inside the tx: a
// malformed token surfaces "mark resumed batch: invalid token" (it needs a live
// pool because the parse runs inside db.WithTx after Begin).
func TestMarkResumedBatch_BadTokenWrap(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)

	err := s.MarkResumedBatch(ownerCtx(), map[string]ResumeAnswer{"not-a-uuid": {Action: ActionAccept}})
	if err == nil {
		t.Fatal("batch with a malformed token: want error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "mark resumed batch: invalid token") {
		t.Fatalf("batch bad-token wrap: want prefix %q, got %q", "mark resumed batch: invalid token", err.Error())
	}
}

// TestStore_DBErrorWrapMessages pins the DISTINCT context prefix each DB-touching
// method stamps onto a real connection error (a canceled context). It kills the
// `return fmt.Errorf("<context>: %w", err)` wrap survivors that a bare err!=nil
// check leaves alive — the wrap removal changes the message, this asserts it.
func TestStore_DBErrorWrapMessages(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	convID := newConversation(t, pool)

	canceled, cancel := context.WithCancel(ownerCtx())
	cancel()

	valid := uuid.Must(uuid.NewV7()).String()
	cases := []struct {
		name   string
		run    func() error
		prefix string
	}{
		{"insert", func() error {
			return s.Insert(canceled, InsertParams{Token: valid, ConversationID: convID, Kind: "approval", Question: "q", ToolCallID: "tc"})
		}, "insert paused state " + valid + ":"},
		{"get", func() error {
			_, err := s.GetByToken(canceled, valid)
			return err
		}, "get paused state " + valid + ":"},
		{"listpending", func() error {
			_, err := s.ListPending(canceled, convID)
			return err
		}, "list pending for " + convID + ":"},
		{"listrecent", func() error {
			_, err := s.ListRecent(canceled, 10)
			return err
		}, "list recent paused states:"},
		{"markresumed", func() error {
			return s.MarkResumed(canceled, valid, ResumeAnswer{Action: ActionAccept})
		}, "mark resumed " + valid + ":"},
		{"cleanup", func() error {
			return s.CleanupResumedOlderThan(canceled, pgTimestamp(time.Now().Add(time.Hour)))
		}, "cleanup resumed:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil {
				t.Fatalf("%s(canceled): want error, got nil", tc.name)
			}
			if !strings.HasPrefix(err.Error(), tc.prefix) {
				t.Fatalf("%s: want message prefix %q, got %q", tc.name, tc.prefix, err.Error())
			}
		})
	}
}

//go:build db_integration

package conversations

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/jackc/pgx/v5/pgtype"
)

func appendSeq(t *testing.T, s *Store, convID, role, content string) int {
	t.Helper()
	seq, err := s.AppendTurnSeq(ownerCtx(), AppendTurnParams{ConversationID: convID, Role: role, Content: content})
	if err != nil {
		t.Fatalf("AppendTurnSeq %s: %v", role, err)
	}
	return seq
}

func storedDecision(t *testing.T, s *Store, convID string, seq int) (TurnDecision, bool) {
	t.Helper()
	var key, effort, requested, source, route, policy, origin pgtype.Text
	if err := s.pool.QueryRow(ownerCtx(), `SELECT recall_context_key, reasoning_effort,
		reasoning_effort_requested, reasoning_effort_source, reasoning_effort_route_key,
		reasoning_effort_policy_version, reasoning_effort_origin_ref
		FROM aura.conversation_turns WHERE conversation_id = $1 AND seq = $2`, convID, seq).
		Scan(&key, &effort, &requested, &source, &route, &policy, &origin); err != nil {
		t.Fatalf("read decision of seq %d: %v", seq, err)
	}
	anySet := key.Valid || effort.Valid || requested.Valid || source.Valid || route.Valid || policy.Valid || origin.Valid
	return TurnDecision{
		ContextKey: key.String, Effort: effort.String, EffortRequested: requested.String,
		EffortSource: source.String, RouteKey: route.String, PolicyVersion: policy.String, OriginRef: origin.String,
	}, anySet
}

func TestAppendTurnSeqReturnsTheSeqItStored(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	first := appendSeq(t, s, convID, llm.RoleUser, "first question")
	second := appendSeq(t, s, convID, llm.RoleAssistant, "first answer")
	if second != first+1 {
		t.Fatalf("seqs = %d then %d, want consecutive", first, second)
	}
	var role string
	if err := s.pool.QueryRow(ownerCtx(), `SELECT role FROM aura.conversation_turns
		WHERE conversation_id = $1 AND seq = $2`, convID, first).Scan(&role); err != nil || role != llm.RoleUser {
		t.Fatalf("row at returned seq %d = %q, %v; want the user turn", first, role, err)
	}
}

// The runner writes the decision on the row it dispatched, never on whatever user turn is
// newest by the time the answer commits (spec, "The write").
func TestRecordTurnDecisionWritesOnlyTheAddressedUserRow(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	older := appendSeq(t, s, convID, llm.RoleUser, "che tempo fa domani?")
	appendSeq(t, s, convID, llm.RoleAssistant, "Sereno.")
	newer := appendSeq(t, s, convID, llm.RoleUser, "e dopodomani?")

	decision := TurnDecision{
		ContextKey: "ctx1:aa", Effort: "none", EffortRequested: "none", EffortSource: "teacher",
		RouteKey: "route1:bb", PolicyVersion: "policy1:cc",
	}
	if err := s.RecordTurnDecision(ownerCtx(), convID, older, decision); err != nil {
		t.Fatalf("RecordTurnDecision: %v", err)
	}
	got, _ := storedDecision(t, s, convID, older)
	if got != decision {
		t.Fatalf("stored decision = %+v, want %+v (the explicit effort none must be stored as 'none')", got, decision)
	}
	if _, anySet := storedDecision(t, s, convID, newer); anySet {
		t.Fatal("the newer user row was labelled by a decision addressed to the older one")
	}
}

func TestRecordTurnDecisionKeepsTheContextWithoutAnEffort(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	seq := appendSeq(t, s, convID, llm.RoleUser, "manda un messaggio a Luca")
	if err := s.RecordTurnDecision(ownerCtx(), convID, seq, TurnDecision{ContextKey: "ctx1:dd"}); err != nil {
		t.Fatalf("RecordTurnDecision: %v", err)
	}
	var effort pgtype.Text
	if err := s.pool.QueryRow(ownerCtx(), `SELECT reasoning_effort FROM aura.conversation_turns
		WHERE conversation_id = $1 AND seq = $2`, convID, seq).Scan(&effort); err != nil {
		t.Fatal(err)
	}
	if effort.Valid {
		t.Fatalf("reasoning_effort = %q, want NULL when no effort field was decided", effort.String)
	}
	if got, _ := storedDecision(t, s, convID, seq); got.ContextKey != "ctx1:dd" {
		t.Fatalf("recall_context_key = %q, want the context kept for tool examples", got.ContextKey)
	}
}

func TestRecordTurnDecisionRefusesAnAssistantRowAndAnUnknownSource(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	user := appendSeq(t, s, convID, llm.RoleUser, "ciao")
	assistant := appendSeq(t, s, convID, llm.RoleAssistant, "Ciao!")
	err := s.RecordTurnDecision(ownerCtx(), convID, assistant, TurnDecision{EffortSource: "seeds"})
	if !errors.Is(err, ErrTurnDecisionTarget) {
		t.Fatalf("decision on an assistant row = %v, want ErrTurnDecisionTarget", err)
	}
	if err := s.RecordTurnDecision(ownerCtx(), convID, user, TurnDecision{EffortSource: "guess"}); err == nil {
		t.Fatal("an unknown source passed the CHECK")
	}
}

func TestProjectionAndDumpCarryTheDecision(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	user := appendSeq(t, s, convID, llm.RoleUser, "scrivi uno script che ruota i log")
	appendSeq(t, s, convID, llm.RoleAssistant, "Ecco lo script.")
	decision := TurnDecision{
		ContextKey: "ctx1:ee", Effort: "high", EffortRequested: "high", EffortSource: "seeds",
		RouteKey: "route1:ff", PolicyVersion: "policy1:gg",
	}
	if err := s.RecordTurnDecision(ownerCtx(), convID, user, decision); err != nil {
		t.Fatal(err)
	}

	turns, _, err := s.ListProjectionTurns(ownerCtx(), localID, ProjectionCursor{}, 1000)
	if err != nil {
		t.Fatalf("ListProjectionTurns: %v", err)
	}
	found := 0
	for _, turn := range turns {
		if turn.ConversationID != convID {
			continue
		}
		found++
		switch turn.Role {
		case llm.RoleUser:
			if turn.Decision != decision {
				t.Errorf("projected user decision = %+v, want %+v", turn.Decision, decision)
			}
		default:
			if turn.Decision != (TurnDecision{}) {
				t.Errorf("projected assistant decision = %+v, want none", turn.Decision)
			}
		}
	}
	if found != 2 {
		t.Fatalf("projected %d turns of the conversation, want 2", found)
	}

	dump, err := s.LoadDump(ownerCtx(), convID)
	if err != nil {
		t.Fatalf("LoadDump: %v", err)
	}
	for _, turn := range dump.Turns {
		if turn.Seq == user && turn.Decision != decision {
			t.Fatalf("dumped decision = %+v, want %+v", turn.Decision, decision)
		}
	}
}

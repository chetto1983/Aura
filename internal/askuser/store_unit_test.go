package askuser

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// TestEncodeAnswer_ValidActions accepts the three MCP actions and round-trips the
// {action, content} jsonb (AM-02).
func TestEncodeAnswer_ValidActions(t *testing.T) {
	for _, action := range []string{ActionAccept, ActionDecline, ActionCancel} {
		b, err := encodeAnswer(ResumeAnswer{Action: action, Content: "hi"})
		if err != nil {
			t.Fatalf("encodeAnswer(%q): %v", action, err)
		}
		var got ResumeAnswer
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Action != action || got.Content != "hi" {
			t.Errorf("round-trip: want {%s hi}, got %+v", action, got)
		}
	}
}

// TestEncodeAnswer_RejectsUnknownAction classifies a bad action as ErrInvalidAnswer.
func TestEncodeAnswer_RejectsUnknownAction(t *testing.T) {
	for _, bad := range []string{"", "approve", "yes", "ACCEPT"} {
		if _, err := encodeAnswer(ResumeAnswer{Action: bad}); !errors.Is(err, ErrInvalidAnswer) {
			t.Errorf("encodeAnswer(%q): want ErrInvalidAnswer, got %v", bad, err)
		}
	}
}

// TestParseUUID_RejectsMalformed covers the boundary parse failure each Store
// method funnels through before any DB round-trip.
func TestParseUUID_RejectsMalformed(t *testing.T) {
	if _, err := parseUUID("token", "not-a-uuid"); err == nil {
		t.Fatal("parseUUID(bad): want error, got nil")
	}
	good := "00000000-0000-0000-0000-000000000001"
	pg, err := parseUUID("token", good)
	if err != nil {
		t.Fatalf("parseUUID(good): %v", err)
	}
	if !pg.Valid {
		t.Error("parsed pgtype.UUID must be Valid")
	}
}

// TestStore_BadUUID_FailsBeforeDB drives every UUID-taking method with a malformed
// id so the parse branch returns before any pool call — exercised with a nil-pool
// Store to prove no DB access happens on the error path.
func TestStore_BadUUID_FailsBeforeDB(t *testing.T) {
	s := New(nil) // nil pool: a DB touch would panic, proving parse short-circuits
	ctx := context.Background()
	bad := "nope"

	if err := s.Insert(ctx, InsertParams{Token: bad, ConversationID: bad, Kind: "approval"}); err == nil {
		t.Error("Insert(bad token): want error, got nil")
	}
	if err := s.Insert(ctx, InsertParams{Token: "00000000-0000-0000-0000-000000000001", ConversationID: bad}); err == nil {
		t.Error("Insert(bad conv): want error, got nil")
	}
	if _, err := s.GetByToken(ctx, bad); err == nil {
		t.Error("GetByToken(bad): want error, got nil")
	}
	if _, err := s.ListPending(ctx, bad); err == nil {
		t.Error("ListPending(bad): want error, got nil")
	}
	if err := s.MarkResumed(ctx, bad, ResumeAnswer{Action: ActionAccept}); err == nil {
		t.Error("MarkResumed(bad): want error, got nil")
	}
	if err := s.AutoResolveForConversation(ctx, bad); err == nil {
		t.Error("AutoResolveForConversation(bad): want error, got nil")
	}
}

// TestMarkResumed_RejectsInvalidAnswerBeforeDB: an invalid action fails encoding
// before any DB call (nil-pool Store proves it).
func TestMarkResumed_RejectsInvalidAnswerBeforeDB(t *testing.T) {
	s := New(nil)
	tok := "11111111-1111-1111-1111-111111111111"
	if err := s.MarkResumed(context.Background(), tok, ResumeAnswer{Action: "bogus"}); !errors.Is(err, ErrInvalidAnswer) {
		t.Errorf("MarkResumed(bad action): want ErrInvalidAnswer, got %v", err)
	}
}

// TestMarkResumedBatch_EmptyIsNoOp: an empty map returns nil without touching the
// DB (nil-pool Store proves it).
func TestMarkResumedBatch_EmptyIsNoOp(t *testing.T) {
	s := New(nil)
	if err := s.MarkResumedBatch(context.Background(), nil); err != nil {
		t.Errorf("empty batch must be a no-op, got %v", err)
	}
}

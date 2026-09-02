package arcadedb

import (
	"strings"
	"testing"
	"time"
)

func validConversationProjection() ConversationProjection {
	content := "Remember the blue notebook"
	return ConversationProjection{
		IdentityID: "identity-a", ConversationID: "conversation-1",
		Turns: []ConversationTurnProjection{{
			IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 1,
			Role: "user", Content: content, ContentHash: conversationContentHash(content),
			OccurredAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
			SourceRef:  "postgres://conversation/conversation-1/turn/1",
		}},
	}
}

// Postgres owns conversation turns; the graph carries a projection of them. This validator
// is what stops a turn belonging to someone else, or disagreeing with its own content, from
// being written under a parent it does not belong to. The live tier reported every refusal
// below uncovered on 2026-09-02.
func TestValidateConversationProjectionRefusesAnInconsistentTurn(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		mutate func(*ConversationProjection)
		reason string
	}{
		"blank identity": {
			func(p *ConversationProjection) { p.IdentityID = "  " },
			"identity must be non-empty",
		},
		"blank conversation id": {
			func(p *ConversationProjection) { p.ConversationID = "" },
			"id must be non-empty",
		},
		"turn belongs to another identity": {
			func(p *ConversationProjection) { p.Turns[0].IdentityID = "identity-b" },
			"foreign identity",
		},
		"turn belongs to another conversation": {
			func(p *ConversationProjection) { p.Turns[0].ConversationID = "conversation-2" },
			"foreign conversation",
		},
		"sequence is not positive": {
			func(p *ConversationProjection) { p.Turns[0].Seq = 0 },
			"sequence must be positive",
		},
		"role is neither user nor assistant": {
			func(p *ConversationProjection) { p.Turns[0].Role = "tool" },
			"ineligible role",
		},
		"blank content": {
			func(p *ConversationProjection) { p.Turns[0].Content = "   " },
			"content must be non-empty",
		},
		"blank source_ref": {
			func(p *ConversationProjection) { p.Turns[0].SourceRef = "" },
			"source_ref must be non-empty",
		},
		"no occurred_at": {
			func(p *ConversationProjection) { p.Turns[0].OccurredAt = time.Time{} },
			"occurred_at must be set",
		},
		"content hash disagrees with the content": {
			func(p *ConversationProjection) { p.Turns[0].Content = "a different turn entirely" },
			"content_hash does not match",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			projection := validConversationProjection()
			testCase.mutate(&projection)
			err := validateConversationProjection(projection)
			if err == nil {
				t.Fatalf("validateConversationProjection accepted %s", name)
			}
			if !strings.Contains(err.Error(), testCase.reason) {
				t.Fatalf("error does not name the refusal: %v", err)
			}
		})
	}
}

func TestValidateConversationProjectionAcceptsBothEligibleRoles(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"user", "assistant"} {
		projection := validConversationProjection()
		projection.Turns[0].Role = role
		if err := validateConversationProjection(projection); err != nil {
			t.Fatalf("validateConversationProjection rejected role %q: %v", role, err)
		}
	}
	empty := validConversationProjection()
	empty.Turns = nil
	if err := validateConversationProjection(empty); err != nil {
		t.Fatalf("validateConversationProjection rejected a turnless projection: %v", err)
	}
}

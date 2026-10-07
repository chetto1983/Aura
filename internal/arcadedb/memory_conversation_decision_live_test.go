//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"testing"
	"time"
)

// A decision reaches Postgres after its turn was first projected, so the update that carries
// it changes no content: it must still land, keep the vector, and clear what Postgres clears.
func TestConversationProjectionLiveUpdatesTheDecisionOverUnchangedContent(t *testing.T) {
	ctx := context.Background()
	client := disposableMemoryClient(t).WithEmbedder(constantEmbedder{value: 1, space: "es1-decision-live"})
	content := "che tempo fa domani a Cuneo?"
	turn := ConversationTurnProjection{
		IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 1, Role: "user",
		Content: content, ContentHash: conversationContentHash(content), OccurredAt: time.Now().UTC(),
		SourceRef: "postgres://aura/conversations/conversation-1/turns/1",
	}
	apply := func(decision TurnDecision) map[string]any {
		t.Helper()
		turn.Decision = decision
		if err := client.ApplyConversationProjection(ctx, ConversationProjection{
			IdentityID: "identity-a", ConversationID: "conversation-1", Turns: []ConversationTurnProjection{turn},
		}); err != nil {
			t.Fatalf("ApplyConversationProjection: %v", err)
		}
		rows, err := client.Query(ctx, "SELECT recall_context_key, effort, effort_requested, effort_source,"+
			" effort_route_key, effort_policy_version, effort_origin_ref, embed_space FROM "+conversationTurnType+
			" WHERE identity_id = 'identity-a' AND conversation_id = 'conversation-1' AND turn_seq = 1", map[string]any{})
		if err != nil || len(rows) != 1 {
			t.Fatalf("read the projected turn = %v, %v", rows, err)
		}
		return rows[0]
	}

	if got := apply(TurnDecision{}); got["effort_source"] != nil {
		t.Fatalf("an undecided turn carries effort_source %v", got["effort_source"])
	}
	decided := TurnDecision{
		ContextKey: "ctx1:aa", Effort: "low", EffortRequested: "low", EffortSource: "teacher",
		RouteKey: "route1:bb", PolicyVersion: "policy1:cc", OriginRef: "postgres://aura/conversations/c0/turns/1",
	}
	got := apply(decided)
	for property, want := range map[string]any{
		"recall_context_key": "ctx1:aa", "effort": "low", "effort_requested": "low", "effort_source": "teacher",
		"effort_route_key": "route1:bb", "effort_policy_version": "policy1:cc",
		"effort_origin_ref": "postgres://aura/conversations/c0/turns/1",
	} {
		if got[property] != want {
			t.Errorf("%s = %v after the decision update, want %v", property, got[property], want)
		}
	}
	if got["embed_space"] != "es1-decision-live" {
		t.Fatalf("embed_space = %v: the decision-only update lost the turn's vector", got["embed_space"])
	}
	got = apply(TurnDecision{})
	for _, property := range turnDecisionProperties {
		if got[property] != nil {
			t.Errorf("%s = %v after Postgres cleared it", property, got[property])
		}
	}
}

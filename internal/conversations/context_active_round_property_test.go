package conversations

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"pgregory.net/rapid"
)

func TestDropOldestPairs_ActiveRoundByteIdentityProperty(t *testing.T) {
	enc := mustEncoderRaw(t)
	rapid.Check(t, func(t *rapid.T) {
		historicalRounds := rapid.IntRange(0, 20).Draw(t, "historical_rounds")
		turns := []Turn{{Seq: 1, Role: llm.RoleSystem, Content: "stable-system"}}
		seq := 2
		for i := 0; i < historicalRounds; i++ {
			turns = append(turns,
				Turn{Seq: seq, Role: llm.RoleUser, Content: rapid.StringMatching(`[a-z]{1,80}`).Draw(t, "old_user")},
				Turn{Seq: seq + 1, Role: llm.RoleAssistant, Content: rapid.StringMatching(`[a-z]{1,80}`).Draw(t, "old_assistant")},
			)
			seq += 2
		}
		active := []Turn{
			{Seq: seq, Role: llm.RoleUser, Content: rapid.StringMatching(`[a-z]{1,120}`).Draw(t, "active_user")},
			{Seq: seq + 1, Role: llm.RoleAssistant, Content: rapid.StringMatching(`[a-z]{1,120}`).Draw(t, "active_assistant")},
			{Seq: seq + 2, Role: llm.RoleTool, Content: rapid.StringMatching(`[a-z]{1,120}`).Draw(t, "active_tool"), ToolCallID: "active-call"},
		}
		turns = append(turns, active...)

		protected := append([]Turn{turns[0]}, active...)
		hardCap := totalTokens(enc, protected)
		reduced, _ := dropOldestPairs(enc, turns, hardCap)
		if len(reduced) < len(protected) {
			t.Fatalf("reduced history lost protected turns: got=%d protected=%d", len(reduced), len(protected))
		}
		gotActive := reduced[len(reduced)-len(active):]
		if !reflect.DeepEqual(gotActive, active) {
			t.Fatalf("active round changed:\ngot  %+v\nwant %+v", gotActive, active)
		}
	})
}

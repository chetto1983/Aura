package conversations

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestHistoryRebuild_StructurallyReasoningFree pins amendment #91 point 2 at the
// unit level: the LLM context stays STRUCTURALLY reasoning-free. Neither
// llm.Message (the wire `messages` element — wireRequest embeds []llm.Message
// verbatim) nor Turn (the loadTurns projection turnToMessage rehydrates from) may
// ever gain a reasoning-shaped field, and a rehydrated assistant answer marshals
// with no reasoning key. The db_integration twin
// (TestTurnReasoning_PersistedForDisplayNeverInHistory) proves the same over a
// real reasoning-bearing row; this pin fails fast on the struct change itself.
func TestHistoryRebuild_StructurallyReasoningFree(t *testing.T) {
	for _, v := range []any{llm.Message{}, Turn{}} {
		rt := reflect.TypeOf(v)
		for f := range rt.Fields() {
			if strings.Contains(strings.ToLower(f.Name), "reasoning") ||
				strings.Contains(strings.ToLower(f.Tag.Get("json")), "reasoning") {
				t.Fatalf("%s.%s: a reasoning-shaped field on the history path breaks amendment #91 point 2 (persisted CoT must never re-enter the LLM context)", rt.Name(), f.Name)
			}
		}
	}

	// A rehydrated assistant answer turn — the row that CARRIES reasoning in the DB —
	// projects to a message whose JSON has zero reasoning content, because the
	// projection input (Turn ← ListTurnsBySeq) never selects the column.
	msg, err := turnToMessage(Turn{Seq: 3, Role: llm.RoleAssistant, Content: "the answer"})
	if err != nil {
		t.Fatalf("turnToMessage: %v", err)
	}
	raw, err := json.Marshal([]llm.Message{msg})
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "reasoning") {
		t.Fatalf("history messages JSON carries a reasoning key: %s", raw)
	}
}

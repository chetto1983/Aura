// Unit tier: the messages[1] always-block (D-07) survival through the L1/L2.5 ladder
// (Pitfall 3 regression) + the messages[0] CAP-04 byte-stability guarantee. Shares
// fakeRotEmitter/mustEncoderRaw from context_unit_test.go.
package conversations

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestAlwaysBlockSurvivesL25Reduction is the Pitfall 3 regression: a 20+-turn
// conversation that forces L2.5 to drop most oldest pairs MUST keep the messages[1]
// always-block (it is protected like the system L0 turn) AND messages[0] must stay
// byte-identical (CAP-04). It asserts both the survival and the byte-stability.
func TestAlwaysBlockSurvivesL25Reduction(t *testing.T) {
	enc := mustEncoderRaw(t)

	const systemContent = "SYSTEM-PROMPT"
	const alwaysContent = "Active skill instructions (always-on):\n\nALWAYS-BODY-MARKER"

	// [system] + 20 user/assistant turns (10 pairs), each body ~50 tokens so the
	// total blows well past a tight hard cap and L2.5 must drop most pairs.
	body := strings.Repeat("word ", 50)
	turns := []Turn{{Seq: 1, Role: llm.RoleSystem, Content: systemContent}}
	for i := 0; i < 10; i++ {
		turns = append(turns,
			Turn{Seq: len(turns) + 1, Role: llm.RoleUser, Content: body},
			Turn{Seq: len(turns) + 2, Role: llm.RoleAssistant, Content: body},
		)
	}
	if len(turns) < 21 {
		t.Fatalf("want >=21 turns (system + 20), got %d", len(turns))
	}

	cfg := windowFor(120) // a tight hard cap so L2.5 fires hard
	cfg.AlwaysBlock = alwaysContent

	emit := &fakeRotEmitter{}
	msgs, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, emit)
	if err != nil {
		t.Fatalf("ladder must reduce successfully (L2.5), got %v", err)
	}
	if len(emit.calls) == 0 {
		t.Fatal("the scenario must exercise L2.5 (a rot event), but none was written")
	}

	// messages[0] is the byte-stable system prompt — unchanged.
	if len(msgs) == 0 || msgs[0].Role != llm.RoleSystem || msgs[0].Content != systemContent {
		t.Fatalf("messages[0] must be the untouched system turn, got %+v", msgs[0])
	}
	sysSHA := sha256.Sum256([]byte(msgs[0].Content))
	wantSHA := sha256.Sum256([]byte(systemContent))
	if sysSHA != wantSHA {
		t.Errorf("messages[0] SHA changed (CAP-04 violated): %x vs %x", sysSHA, wantSHA)
	}

	// messages[1] is the always-block — it SURVIVED the reduction (Pitfall 3).
	if len(msgs) < 2 || msgs[1].Role != llm.RoleUser || msgs[1].Content != alwaysContent {
		t.Fatalf("messages[1] must be the surviving always-block, got %+v", msgs[1])
	}
	// The always-block must not carry the internal protection marker on the wire.
	if msgs[1].ToolCallID != "" {
		t.Errorf("always-block wire message must not carry a tool_call_id, got %q", msgs[1].ToolCallID)
	}
	// Confirm the always-body literal is present somewhere (defensive).
	var sawBody bool
	for _, m := range msgs {
		if strings.Contains(m.Content, "ALWAYS-BODY-MARKER") {
			sawBody = true
		}
	}
	if !sawBody {
		t.Error("the always-block body did not survive the reduction (Pitfall 3 regression)")
	}
}

// TestAlwaysBlockOmittedWhenEmpty proves an empty AlwaysBlock injects no extra turn:
// the ladder output is exactly the system + body turns (no phantom messages[1]).
func TestAlwaysBlockOmittedWhenEmpty(t *testing.T) {
	enc := mustEncoderRaw(t)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "s"},
		{Seq: 2, Role: llm.RoleUser, Content: "hi"},
		{Seq: 3, Role: llm.RoleAssistant, Content: "yo"},
	}
	cfg := windowFor(1_000_000) // no reduction
	cfg.AlwaysBlock = ""        // no always skills

	msgs, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, &fakeRotEmitter{})
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("empty always-block must inject no turn, got %d messages", len(msgs))
	}
	if msgs[1].Role != llm.RoleUser || msgs[1].Content != "hi" {
		t.Errorf("messages[1] should be the first user turn, got %+v", msgs[1])
	}
}

// TestAlwaysBlockInjectedAfterSystem proves the always-block lands at messages[1]
// (right after the system L0 turn) when a system turn leads the history, and at the
// head when none does (the agent prepends its own system message in that case).
func TestAlwaysBlockInjectedAfterSystem(t *testing.T) {
	enc := mustEncoderRaw(t)
	cfg := windowFor(1_000_000)
	cfg.AlwaysBlock = "ALWAYS"

	// With a leading system turn → [system, always, ...].
	withSys := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "s"},
		{Seq: 2, Role: llm.RoleUser, Content: "u"},
	}
	msgs, err := applyContextLadder(context.Background(), "conv", withSys, cfg, enc, &fakeRotEmitter{})
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if len(msgs) != 3 || msgs[0].Role != llm.RoleSystem || msgs[1].Content != "ALWAYS" {
		t.Fatalf("with system: want [system, always, user], got %+v", msgs)
	}

	// Without a leading system turn → [always, ...] (agent prepends system later).
	noSys := []Turn{{Seq: 1, Role: llm.RoleUser, Content: "u"}}
	msgs2, err := applyContextLadder(context.Background(), "conv", noSys, cfg, enc, &fakeRotEmitter{})
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if len(msgs2) != 2 || msgs2[0].Content != "ALWAYS" || msgs2[0].Role != llm.RoleUser {
		t.Fatalf("without system: want [always, user], got %+v", msgs2)
	}
}

package conversations

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// The messages[1] always-block markers (formerly internal/profile constants; the
// package was retired with Agent.md in Amendment #87). These tests only need a
// realistic, byte-stable always-block fixture to prove the L1/L2.5 ladder protects it.
const (
	testBlockStart = "<profile:Agent.md>"
	testBlockEnd   = "</profile:Agent.md>"
)

func TestProfileSkillsBlockSurvivesL25Reduction(t *testing.T) {
	enc := mustEncoderRaw(t)

	const systemContent = "SYSTEM-PROMPT"
	block := testBlockStart + "\n# Agent.md\n\n## Identity\n- Name: Davide\n" + testBlockEnd +
		"\n\nActive skill instructions (always-on):\n\nPROFILE-SKILL-MARKER"

	body := strings.Repeat("word ", 50)
	turns := []Turn{{Seq: 1, Role: llm.RoleSystem, Content: systemContent}}
	for range 10 {
		turns = append(turns,
			Turn{Seq: len(turns) + 1, Role: llm.RoleUser, Content: body},
			Turn{Seq: len(turns) + 2, Role: llm.RoleAssistant, Content: body},
		)
	}

	injected := injectAlwaysBlock(turns, block)
	protected := []Turn{
		injected[0],
		injected[1],
		injected[len(injected)-2],
		injected[len(injected)-1],
	}
	cfg := windowFor(totalTokens(enc, protected))
	cfg.AlwaysBlock = block
	emit := &fakeRotEmitter{}

	msgs, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, emit)
	if err != nil {
		t.Fatalf("ladder must reduce successfully: %v", err)
	}
	if len(emit.calls) == 0 {
		t.Fatal("scenario must exercise L2.5 hard-drop reduction")
	}
	if len(msgs) < 2 {
		t.Fatalf("want at least system + profile/skills block, got %+v", msgs)
	}
	if msgs[0].Role != llm.RoleSystem || msgs[0].Content != systemContent {
		t.Fatalf("messages[0] must remain the system prompt, got %+v", msgs[0])
	}
	if msgs[1].Role != llm.RoleUser || msgs[1].Content != block {
		t.Fatalf("messages[1] must be the surviving profile/skills block, got %+v", msgs[1])
	}
	if msgs[1].ToolCallID != "" {
		t.Fatalf("profile/skills block must not leak its protection marker, got %q", msgs[1].ToolCallID)
	}
	if !strings.Contains(msgs[1].Content, testBlockStart) ||
		!strings.Contains(msgs[1].Content, testBlockEnd) ||
		!strings.Contains(msgs[1].Content, "PROFILE-SKILL-MARKER") {
		t.Fatalf("messages[1] lost profile-first content:\n%s", msgs[1].Content)
	}
}

func TestProfileBlockOmittedWhenContextBlockEmpty(t *testing.T) {
	enc := mustEncoderRaw(t)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "s"},
		{Seq: 2, Role: llm.RoleUser, Content: "hi"},
		{Seq: 3, Role: llm.RoleAssistant, Content: "yo"},
	}
	cfg := windowFor(1_000_000)
	cfg.AlwaysBlock = ""

	msgs, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, &fakeRotEmitter{})
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("empty context block must inject no synthetic turn, got %d messages", len(msgs))
	}
	for _, msg := range msgs {
		if strings.Contains(msg.Content, testBlockStart) {
			t.Fatalf("empty context block leaked profile marker in %+v", msg)
		}
	}
	if msgs[1].Role != llm.RoleUser || msgs[1].Content != "hi" {
		t.Fatalf("messages[1] should remain the first real user turn, got %+v", msgs[1])
	}
}

func TestProfileBlockSurvivesL1ToolEviction(t *testing.T) {
	enc := mustEncoderRaw(t)
	block := testBlockStart + "\n# Agent.md\n\n## Identity\n- Keep exact profile block\n" + testBlockEnd
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "s"},
		{Seq: 2, Role: llm.RoleUser, Content: "run tool"},
		{Seq: 3, Role: llm.RoleAssistant, Content: "calling"},
		{Seq: 4, Role: llm.RoleTool, Content: "old tool output", ToolCallID: "call_old", ContentSidecarPath: "/run/sidecar/call_old"},
		{Seq: 20, Role: llm.RoleUser, Content: "next"},
	}
	cfg := windowFor(1_000_000)
	cfg.ToolEvictAfterTurns = 1
	cfg.AlwaysBlock = block

	msgs, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, &fakeRotEmitter{})
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if len(msgs) < 2 || msgs[1].Content != block {
		t.Fatalf("L1 must leave the profile block byte-identical, got %+v", msgs)
	}
	if strings.Contains(msgs[1].Content, "read_tool_output") {
		t.Fatalf("L1 pointer rewrite leaked into profile block:\n%s", msgs[1].Content)
	}
	var sawPointer bool
	for _, msg := range msgs {
		if strings.Contains(msg.Content, "read_tool_output(tool_call_id=\"call_old\")") {
			sawPointer = true
		}
	}
	if !sawPointer {
		t.Fatalf("test setup did not exercise L1 tool eviction, got %+v", msgs)
	}
}

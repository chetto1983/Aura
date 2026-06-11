package runner

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

func TestProfileContextBlockUsesConversationIdentityAndPrecedesSkills(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("call-1", "ok")))
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	var seenIdentity string
	r.contextBlock = func(_ context.Context, identityID string) string {
		seenIdentity = identityID
		return "<profile:Agent.md>\nName: Davide\n</profile:Agent.md>"
	}
	r.alwaysBlock = func() string {
		return "<skills always>\nSkill body\n</skills always>"
	}

	if _, err := drain(r.Turn(ctx, convID, userPtr("hello"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if seenIdentity != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("context provider identity = %q", seenIdentity)
	}
	block := conv.lastCfg.AlwaysBlock
	profileAt := strings.Index(block, "<profile:Agent.md>")
	skillsAt := strings.Index(block, "<skills always>")
	if profileAt < 0 || skillsAt < 0 || profileAt > skillsAt {
		t.Fatalf("AlwaysBlock should be profile-first then skills, got:\n%s", block)
	}
}

func TestProfileContextMissingFallsBackToSkillsOnly(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("call-1", "ok")))
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	r.contextBlock = func(context.Context, string) string { return "" }
	r.alwaysBlock = func() string { return "SKILLS-ONLY" }

	if _, err := drain(r.Turn(ctx, convID, userPtr("hello"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if conv.lastCfg.AlwaysBlock != "SKILLS-ONLY" {
		t.Fatalf("AlwaysBlock = %q, want skills-only", conv.lastCfg.AlwaysBlock)
	}
}

func TestProfileContextDoesNotChangeSystemPromptHash(t *testing.T) {
	noProfile := []llm.Message{{Role: llm.RoleSystem, Content: "SYSTEM"}, {Role: llm.RoleUser, Content: "hi"}}
	withProfile := []llm.Message{
		{Role: llm.RoleSystem, Content: "SYSTEM"},
		{Role: llm.RoleUser, Content: "<profile:Agent.md>\nName: Davide\n</profile:Agent.md>"},
		{Role: llm.RoleUser, Content: "hi"},
	}

	noProfileHash := sha256.Sum256([]byte(noProfile[0].Content))
	withProfileHash := sha256.Sum256([]byte(withProfile[0].Content))
	if noProfileHash != withProfileHash {
		t.Fatalf("messages[0] hash changed: %x vs %x", noProfileHash, withProfileHash)
	}
	if strings.Contains(withProfile[0].Content, "Agent.md") {
		t.Fatal("Agent.md content leaked into messages[0]")
	}
	if !strings.Contains(withProfile[1].Content, "Agent.md") {
		t.Fatal("profile marker should be in messages[1]")
	}
}

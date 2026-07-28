package main

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/runner"
)

// skills_cli_surface_test.go guards the two surfaces amendment #97 removed from the CLI
// and the resume chain. Neither had a single test before it was deleted — which is the
// finding, not an aside: the approve command and the skill resume hook could both be
// removed without reddening anything.

// TestSkillsUsageDropsApprove pins the operator-facing usage line. `aura skills approve`
// is gone (a skill is live when written), and a usage string that still advertises it
// would send the operator to a command that exits 1. The surviving verbs are listed
// explicitly so a future removal has to be deliberate.
func TestSkillsUsageDropsApprove(t *testing.T) {
	t.Parallel()
	if strings.Contains(skillsUsage, "approve") {
		t.Fatalf("skills usage still advertises approve:\n%s", skillsUsage)
	}
	for _, verb := range []string{"list", "info", "create", "update", "delete", "always", "snippet", "audit"} {
		if !strings.Contains(skillsUsage, verb) {
			t.Errorf("skills usage lost the %q verb:\n%s", verb, skillsUsage)
		}
	}
}

// TestResumeChainIgnoresSkillApproval proves a leftover skill_approval pause — one minted
// by a pre-#97 daemon and still sitting in paused_states — resolves to a clean no-op
// instead of an error. The skill hook is gone; the surviving hooks must not claim a
// resume context that is not theirs, and must not fail the resume for the operator.
func TestResumeChainIgnoresSkillApproval(t *testing.T) {
	t.Parallel()
	chain := chainResumeHooks(
		newShellResumeHook(tools.NewShellApprovals()),
		newGatewayResumeHook(nil),
		newScheduledTaskResumeHook(nil),
	)
	err := chain(context.Background(), askuser.Pending{
		ConversationID: "conv-legacy",
		Kind:           tools.KindApproval,
		Question:       "Approve skill create \"ghost\"?",
		ResumeContext:  rawJSON(t, map[string]string{"type": "skill_approval", "skill_name": "ghost"}),
	}, runner.ResponseInput{Action: askuser.ActionAccept})
	if err != nil {
		t.Fatalf("a stale skill_approval resume must be a no-op, got %v", err)
	}
}

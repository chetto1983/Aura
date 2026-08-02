package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/idempotency"
)

func TestCLIMutationCoverageHasCompleteIdempotencyMetadata(t *testing.T) {
	t.Parallel()

	for command, meta := range cliMutationCommands {
		if meta.Scope != idempotency.ScopeCLICommand || meta.Normalize == "" || meta.KeyPolicy != cliExplicitOrGeneratedKey || meta.Execute == nil {
			t.Errorf("mutating command %q has incomplete idempotency metadata: %+v", command, meta)
		}
	}
	for _, command := range []string{
		"chat new", "chat resume", "chat archive", "chat delete", "chat rename",
		"task schedule", "task cancel", "task run_now", "task approve",
		"identity grant", "identity revoke", "mcp install", "mcp remove", "skills create", "skills delete",
	} {
		if _, ok := cliMutationCommands[command]; !ok {
			t.Errorf("mutating command %q is absent from the idempotency inventory", command)
		}
	}
}

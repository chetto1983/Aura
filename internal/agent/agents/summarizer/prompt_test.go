package summarizer

import (
	"testing"

	"github.com/aura/aura/internal/agent/agentdef"
)

func TestSummarizerArchetype_AppliesViaShim(t *testing.T) {
	if Prompt != agentdef.MustBuiltinPrompt(ID) {
		t.Fatal("legacy summarizer.Prompt no longer matches AGENTDEF prompt")
	}
}

package conversation

import (
	"strings"
	"testing"
)

func TestWikiProposalPromptMentionsReviewGate(t *testing.T) {
	got := WikiProposalPrompt()
	for _, want := range []string{"propose_wiki_change", "automatic post-turn memory capture", "reviewable", "provenance", "evidence refs"} {
		if !strings.Contains(got, want) {
			t.Fatalf("WikiProposalPrompt() missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Use write_wiki directly") {
		t.Fatalf("WikiProposalPrompt() should not steer current-turn memory to direct writes:\n%s", got)
	}
}

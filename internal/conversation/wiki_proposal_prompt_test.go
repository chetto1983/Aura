package conversation

import (
	"strings"
	"testing"
)

func TestWikiProposalPromptMentionsReviewGate(t *testing.T) {
	got := WikiProposalPrompt()
	for _, want := range []string{"propose_wiki_change", "without writing directly", "reviewable"} {
		if !strings.Contains(got, want) {
			t.Fatalf("WikiProposalPrompt() missing %q:\n%s", want, got)
		}
	}
}

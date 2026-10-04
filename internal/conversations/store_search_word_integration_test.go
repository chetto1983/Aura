//go:build db_integration

package conversations

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/llm"
)

// The message measured on 2026-10-04 (prd.md §7), with a marker word in its middle so a
// shared test database cannot crowd the hit out of the limit.
func longMessageWith(marker string) string {
	return "Ciao Aura, ti scrivo per organizzare la settimana prossima: lunedì devo mandare la " +
		"fattura " + marker + " a Bianchi per il lavoro di ristrutturazione del bagno, martedì " +
		"c'è la riunione con il commercialista per la dichiarazione dei redditi, mercoledì porto " +
		"la macchina dal meccanico per il tagliando e giovedì sera c'è la cena di compleanno di " +
		"Giulia al ristorante sul lago. Ricordami tutto e preparami una lista."
}

func searchMarker() string {
	return "zefiro" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
}

func appendUserTurn(t *testing.T, s *Store, convID, content string) {
	t.Helper()
	if err := s.AppendTurn(ownerCtx(), AppendTurnParams{
		ConversationID: convID, Seq: 1, Role: llm.RoleUser, Content: content,
	}); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
}

func searchHits(t *testing.T, s *Store, query string) []SearchResult {
	t.Helper()
	hits, err := s.SearchConversationTurns(ownerCtx(), query, 50)
	if err != nil {
		t.Fatalf("SearchConversationTurns(%q): %v", query, err)
	}
	return hits
}

// TestSearchFindsAWordInsideALongMessage pins the fix: whole-message similarity scored
// the marker word at about 0.03 in this message and never found it; the word, the prefix
// typed so far and a one-letter typo are all found now, and a word the message lacks is not.
func TestSearchFindsAWordInsideALongMessage(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	marker := searchMarker()
	appendUserTurn(t, s, convID, longMessageWith(marker))

	typo := marker[:8] + "q" + marker[9:]
	for name, query := range map[string]string{
		"the word":        marker,
		"a prefix":        marker[:len(marker)-2],
		"a one-char typo": typo,
	} {
		if !containsSearchResult(searchHits(t, s, query), convID) {
			t.Errorf("%s (%q) did not find the message", name, query)
		}
	}
	if containsSearchResult(searchHits(t, s, searchMarker()), convID) {
		t.Error("a word the message does not contain found it")
	}
}

// TestSearchRanksTheBetterMatchThenTheNewer: an exact word outranks a misspelt one, and
// between equal matches the newer conversation comes first.
func TestSearchRanksTheBetterMatchThenTheNewer(t *testing.T) {
	s := newStore(t, migratedPool(t))
	marker := searchMarker()
	older, misspelt, newer := newConversation(t, s), newConversation(t, s), newConversation(t, s)
	appendUserTurn(t, s, older, longMessageWith(marker))
	appendUserTurn(t, s, misspelt, longMessageWith(marker[:8]+"q"+marker[9:]))
	appendUserTurn(t, s, newer, longMessageWith(marker))

	var order []string
	for _, hit := range searchHits(t, s, marker) {
		switch hit.ConversationID {
		case older, misspelt, newer:
			order = append(order, hit.ConversationID)
		}
	}
	want := []string{newer, older, misspelt}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want newer exact, older exact, then misspelt %v", order, want)
	}
}

// TestSearchFindsOnlyTheTurnsTheThreadShows: the thread shows user and assistant turns as
// messages, folds a tool result into the assistant's tool card and hides system turns, so
// a word in all four is found in the first two only.
func TestSearchFindsOnlyTheTurnsTheThreadShows(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	marker := searchMarker()
	turns := []AppendTurnParams{
		{Role: llm.RoleSystem, Content: "Follow these skill instructions: " + marker},
		{Role: llm.RoleUser, Content: longMessageWith(marker)},
		{Role: llm.RoleAssistant, Content: "Ho segnato la fattura " + marker + " per lunedì."},
		{Role: llm.RoleTool, ToolCallID: "call-1", Content: `{"task":"` + marker + `","status":"active"}`},
	}
	for i, turn := range turns {
		turn.ConversationID, turn.Seq = convID, i+1
		if err := s.AppendTurn(ownerCtx(), turn); err != nil {
			t.Fatalf("AppendTurn %s: %v", turn.Role, err)
		}
	}

	var seqs []int
	for _, hit := range searchHits(t, s, marker) {
		if hit.ConversationID == convID {
			seqs = append(seqs, hit.Seq)
		}
	}
	if got := fmt.Sprint(seqs); got != "[2 3]" && got != "[3 2]" {
		t.Fatalf("hits in the conversation = %s, want the user (2) and assistant (3) turns only", got)
	}
}

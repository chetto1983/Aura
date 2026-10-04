//go:build db_integration

package conversations

import (
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

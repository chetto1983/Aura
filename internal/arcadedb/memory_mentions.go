package arcadedb

import (
	"sort"
	"strings"
	"unicode"
)

// Mentions are the graph's missing middle.
//
// Measured 2026-09-02 on a live 107-fact memory: 209 entities, of which 199 had
// degree 1. Every fact carried a subject and an object no other fact named, so
// FACT edges formed a set of near-disjoint pairs and there was nothing to
// traverse. The connectivity was there all along, but only in the prose: 75 of
// 106 facts named, inside their own statement text, an entity that was the
// subject or object of a DIFFERENT fact.
//
// A mention hangs off the fact's ENDPOINTS, not off the fact. A fact is an EDGE
// in this model (memory.go) and an edge cannot be the endpoint of another edge,
// so `MENTIONS` runs Entity -> Entity and carries the fact_key that caused it.
//
// Both endpoints, not just the subject. Measured on the same corpus: linking
// from the subject alone gave a second hop to 35 entities, from both endpoints
// to 64, with the neighbourhood no larger (p90 of 9 against 12). Subject-only
// would also have reintroduced the exact asymmetry documented at memory.go:436,
// where matching outV() alone hid every entity that is only ever spoken about.

const mentionsEdgeType = "MENTIONS"

func mentionSchemaStatements() []string {
	return []string{
		"CREATE EDGE TYPE " + mentionsEdgeType + " IF NOT EXISTS",
		"CREATE PROPERTY " + mentionsEdgeType + ".fact_key IF NOT EXISTS STRING",
	}
}

// nameShaped keeps names and drops phrases.
//
// Every token must look like a name: capitalised, or carrying a digit, or
// carrying one of the punctuation marks that identifiers use. `ArcadeDB`,
// `Claude Code`, `Neo4j`, `ralph.sh` and `golangci-lint` pass; `il container`,
// `un gate` and `inglese` do not.
//
// The rule earns its keep by what it removes. Measured on the same corpus,
// linking on EVERY entity produced 16 bridges and 37 linked facts; restricting
// to name-shaped entities produced 6 bridges and 31 linked facts. The four facts
// given up were bridged by generic subject phrases -- an accident of wording, not
// a shared subject -- and each such phrase is a false edge the second hop would
// have to carry forever.
//
// It is deliberately not a language model and not a stopword list. Aura's memory
// bans stopword lists (they are regex in disguise, and they are per-language);
// this rule reads shape, so it behaves the same in Italian and in English.
func nameShaped(name string) bool {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		if !tokenNameShaped(field) {
			return false
		}
	}
	return true
}

func tokenNameShaped(token string) bool {
	for index, symbol := range token {
		switch {
		case index == 0 && unicode.IsUpper(symbol):
			return true
		case unicode.IsDigit(symbol):
			return true
		case strings.ContainsRune("._-/:", symbol):
			return true
		}
	}
	return false
}

// mentionScanner finds which of a fixed set of names occur in a text.
//
// The names are lower-cased once at construction because the scan runs them all
// against every statement in the corpus; the text is lower-cased once per
// statement for the same reason.
type mentionScanner struct {
	names  []string
	folded []string
}

// newMentionScanner keeps only the name-shaped entities, longest first so that a
// containing name is offered before the name it contains. Order is otherwise
// alphabetical, which makes the emitted edge set reproducible.
func newMentionScanner(entities []string) *mentionScanner {
	kept := make([]string, 0, len(entities))
	for _, entity := range entities {
		if entity = strings.TrimSpace(entity); entity != "" && nameShaped(entity) {
			kept = append(kept, entity)
		}
	}
	sort.Slice(kept, func(i, j int) bool {
		if len(kept[i]) != len(kept[j]) {
			return len(kept[i]) > len(kept[j])
		}
		return kept[i] < kept[j]
	})
	scanner := &mentionScanner{names: kept, folded: make([]string, len(kept))}
	for index, name := range kept {
		scanner.folded[index] = strings.ToLower(name)
	}
	return scanner
}

// namesIn returns every scanned name occurring in text, excluding the ones the
// caller already owns. `Claude` and `Claude Code` are independent: each is
// matched on its own boundaries, so a statement naming the longer one yields
// both, and neither suppresses the other.
func (s *mentionScanner) namesIn(text string, owned ...string) []string {
	if s == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	folded := strings.ToLower(text)
	var found []string
	for index, name := range s.names {
		if containsAnyFold(owned, name) {
			continue
		}
		if occursOnWordBoundary(folded, s.folded[index]) {
			found = append(found, name)
		}
	}
	return found
}

func containsAnyFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

// occursOnWordBoundary is why this is not a substring test: an entity named
// `gate` must not match inside `mitigate`, and one named `Neo4j` must match
// `neo4j`. Both arguments are already folded.
//
// ArcadeDB CAN do this in SQL and it was checked before this was written.
// `MATCHES` is a full Java regex operator -- verified live on 26.9.1, `mitigate`
// against `(?i).*\bgate\b.*` returns 0 and `neo4j` against `(?i).*\bNeo4j\b.*`
// returns 1. It is not used here because of the access pattern, not the
// capability: reference/sql/sql-where.adoc states that "LIKE, ILIKE, MATCHES are
// not using full-text indexes", so every evaluation is a full scan of FACT, and
// no function in the engine takes a LIST of needles (the whole text.* namespace
// and sql-functions.adoc were enumerated -- there is no containsAny). Finding
// WHICH names occur therefore costs one unindexed scan per candidate name, where
// this costs one pass over the corpus and no round trip at all.
//
// The engine did settle one detail: Java's default `\b` uses ASCII \w, so
// `società` fails its own boundary test without the `(?U)` flag. Comparing bytes
// rather than runes gives that behaviour for free, since every continuation byte
// of a UTF-8 sequence reads as a word byte.
func occursOnWordBoundary(text, name string) bool {
	if name == "" || len(name) > len(text) {
		return false
	}
	for start := 0; start+len(name) <= len(text); {
		offset := strings.Index(text[start:], name)
		if offset < 0 {
			return false
		}
		at := start + offset
		if !wordByteAt(text, at-1) && !wordByteAt(text, at+len(name)) {
			return true
		}
		start = at + 1
	}
	return false
}

// wordByteAt reports whether the byte at index continues a word. Bytes rather
// than runes: every byte of a multi-byte UTF-8 sequence has its high bit set, so
// treating those as word bytes keeps an accented letter adjacent to a name from
// being read as a boundary -- which is the behaviour we want and the one a
// rune-wise scan would have to walk backwards to reproduce.
func wordByteAt(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return false
	}
	symbol := text[index]
	switch {
	case symbol >= 'a' && symbol <= 'z', symbol >= 'A' && symbol <= 'Z':
		return true
	case symbol >= '0' && symbol <= '9', symbol == '_':
		return true
	default:
		return symbol >= utf8Continuation
	}
}

const utf8Continuation = 0x80

// hubCap is how many facts an entity may be mentioned by before it stops
// linking anything at all.
//
// Without one the graph is a clique. Measured on the same 107-fact corpus:
// uncapped, `Aura` alone was mentioned by 57 facts and the projected graph
// reached a maximum neighbour count of 64 with a median of 57 over linked facts
// -- an average fact linked to half the corpus, which is as useless as linking
// to none of it.
//
// The default share is 20%, and it is not a knife edge. Sweeping the cap showed
// the outcome is bimodal: every share between 10% and 50% produced the identical
// graph (31 linked facts, 6 bridges, maximum 13), while 5% collapsed it to 4
// linked facts and 100% exploded it. 20% is the middle of that plateau.
//
// The comparison is inclusive -- an entity mentioned by exactly the cap number of
// facts still links -- so a cap of 0 means "only entities nothing mentions",
// which is the empty graph, and a corpus of zero facts divides by nothing.
func hubCap(facts int, share float64) int {
	if facts <= 0 || share <= 0 {
		return 0
	}
	return int(float64(facts) * share)
}

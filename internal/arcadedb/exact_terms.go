package arcadedb

import (
	"strings"
	"unicode"
)

// ExactTerm is a term the query asks for verbatim (spec 2026-10-25-document-exactness-and-judge,
// decision 1). Raw is the term as the caller wrote it: the full-text index analyses it the way it
// analysed the documents. Key is its lowered tokens, which a text must carry in order.
//
// Only structured signals make one: a phrase the caller quoted, an identifier, a file name. Names
// are never guessed from capitals, sentence position or word lists. Measured while planning, a
// capitalised-run rule turned "Mostra Ubuntu Mono?" into the term "mostra ubuntu mono", which no
// document carries, and the caller, an LLM, already knows which words are names.
type ExactTerm struct {
	Raw string
	Key string
}

// quotePairs are the quotation marks a caller may write a verbatim phrase in.
var quotePairs = []struct{ open, close rune }{{'"', '"'}, {'“', '”'}, {'«', '»'}}

// QueryExactTerms returns the query's exact terms: its quoted phrases, then its file names, then
// its identifiers, each once.
func QueryExactTerms(query string) []ExactTerm {
	var terms []ExactTerm
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		key := strings.Join(identifierTokens(raw), " ")
		if key == "" {
			return
		}
		for _, term := range terms {
			if term.Key == key {
				return
			}
		}
		terms = append(terms, ExactTerm{Raw: raw, Key: key})
	}
	for _, phrase := range quotedPhrases(query) {
		add(phrase)
	}
	for word := range strings.FieldsSeq(query) {
		if name := strings.TrimRight(strings.TrimFunc(word, isWrapping), "."); isFileName(name) {
			add(name)
		}
	}
	for _, identifier := range QueryIdentifiers(query) {
		add(identifier)
	}
	return terms
}

// quotedPhrases is every closed quotation in query, in order. An unclosed one quotes nothing.
func quotedPhrases(query string) []string {
	var phrases []string
	runes := []rune(query)
	for i := 0; i < len(runes); i++ {
		for _, pair := range quotePairs {
			if runes[i] != pair.open {
				continue
			}
			end := i + 1
			for end < len(runes) && runes[end] != pair.close {
				end++
			}
			if end < len(runes) {
				phrases = append(phrases, string(runes[i+1:end]))
				i = end
			}
			break
		}
	}
	return phrases
}

func isWrapping(r rune) bool {
	return strings.ContainsRune(`,;:!?()[]{}'"“”«»`, r)
}

// isFileName reports a word ending in "." and an extension of 2 to 5 letters or digits, at least
// one of them a letter, after a stem of letters, digits, "_", "-" and ".": a structured format.
func isFileName(word string) bool {
	dot := strings.LastIndexByte(word, '.')
	if dot <= 0 {
		return false
	}
	stem, extension := word[:dot], []rune(word[dot+1:])
	if len(extension) < 2 || len(extension) > 5 {
		return false
	}
	lettered := false
	for _, r := range extension {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
		lettered = lettered || unicode.IsLetter(r)
	}
	for _, r := range stem {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return lettered
}

// CarriedBy reports whether one of texts contains the term's tokens in order, as whole tokens.
func (t ExactTerm) CarriedBy(texts ...string) bool {
	want := strings.Fields(t.Key)
	if len(want) == 0 {
		return false
	}
	for _, text := range texts {
		tokens := identifierTokens(text)
		for start := 0; start+len(want) <= len(tokens); start++ {
			if equalTokens(tokens[start:start+len(want)], want) {
				return true
			}
		}
	}
	return false
}

func equalTokens(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// lucenePhrase quotes Raw as one Lucene phrase (arcadedb-docs full-text-index.adoc, "Phrase
// Queries"). Inside a phrase only the quote and the backslash need escaping.
func (t ExactTerm) lucenePhrase() string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(t.Raw)
	return `"` + escaped + `"`
}

package arcadedb

import (
	"regexp"
	"strings"
)

// Entity extraction is the one step of Mem0's retrieval algorithm that is not
// arithmetic. Their pipeline pulls entities out of both the stored text and the
// query, links them, and boosts a candidate when the two share one; everything
// around it -- the sigmoid, the additive fusion, the hub penalty -- is cheap
// arithmetic over whatever this returns. So the quality of the whole thing rests
// here.
//
// Two implementations, chosen at build time:
//
//	default        capitalised runs and quoted spans (this file)
//	-tags spacy    spaCy through go-spacy: named entities AND noun chunks
//
// The default is deliberately kept rather than made an error, because the
// difference is measurable and worth being able to measure: a regex over capital
// letters cannot see that "a sunrise" or "her education" is a thing a question is
// about, which needs a POS tagger. The build tag keeps the ordinary `go build
// ./...` free of cgo and an embedded Python.

// EntityExtractor names the things a piece of text is about.
type EntityExtractor interface {
	Entities(text string) []string
}

var (
	properNounRE = regexp.MustCompile(`\b([A-Z][a-z]{2,}(?:\s+[A-Z][a-z]{2,})*)\b`)
	quotedRE     = regexp.MustCompile(`["']([^"']{3,40})["']`)
)

// genericHeads are the heads mem0/utils/entity_extraction.py drops for naming
// nothing. Kept in the shared file because both implementations need them.
var genericHeads = map[string]bool{
	"thing": true, "things": true, "stuff": true, "way": true, "ways": true,
	"time": true, "times": true, "experience": true, "situation": true,
	"case": true, "fact": true, "matter": true, "issue": true, "idea": true,
	"ideas": true, "thought": true, "thoughts": true, "feeling": true,
	"feelings": true, "place": true, "area": true, "part": true, "kind": true,
	"type": true, "sort": true, "lot": true, "bit": true, "day": true,
	"days": true, "year": true, "years": true, "week": true, "weeks": true,
	"month": true, "months": true, "one": true, "ones": true,
	"something": true, "anything": true, "everything": true, "nothing": true,
	"someone": true, "anyone": true, "everyone": true,
}

// sentenceStarters are words the capital-letter rule picks up because they open a
// sentence, not because they name anything. spaCy discards them by knowing their
// part of speech; the regex needs the list.
var sentenceStarters = map[string]bool{
	"The": true, "This": true, "That": true, "What": true, "When": true,
	"Where": true, "Which": true, "Who": true, "How": true, "Why": true,
	"Did": true, "Does": true, "Was": true, "Were": true, "Has": true,
	"Have": true, "Had": true, "Are": true, "And": true, "But": true,
	"For": true, "Not": true, "You": true, "Your": true, "She": true,
	"They": true, "His": true, "Her": true, "Their": true, "Its": true,
	"There": true, "Then": true, "Also": true, "After": true, "Before": true,
	"Hey": true, "Hi": true, "Yeah": true, "Yes": true, "Oh": true, "Wow": true,
	"Thanks": true, "Sorry": true, "Sure": true, "Good": true, "Great": true,
	"Would": true, "Could": true, "Should": true, "Will": true, "Can": true,
}

// RegexExtractor is the dependency-free fallback.
type RegexExtractor struct{}

// Entities returns proper-noun runs and quoted spans.
func (RegexExtractor) Entities(text string) []string {
	collector := newEntitySet()
	for _, match := range quotedRE.FindAllStringSubmatch(text, -1) {
		collector.add(match[1])
	}
	for _, match := range properNounRE.FindAllStringSubmatch(text, -1) {
		// A run opening with a sentence starter still names what follows it:
		// "When Melanie" is not an entity, "Melanie" is.
		parts := strings.Fields(match[1])
		for len(parts) > 0 && sentenceStarters[parts[0]] {
			parts = parts[1:]
		}
		collector.add(strings.Join(parts, " "))
	}
	return collector.items
}

// entitySet keeps insertion order while rejecting duplicates and noise.
type entitySet struct {
	seen  map[string]bool
	items []string
}

func newEntitySet() *entitySet {
	return &entitySet{seen: map[string]bool{}}
}

func (s *entitySet) add(candidate string) {
	candidate = strings.TrimSpace(candidate)
	if len(candidate) < 3 {
		return
	}
	folded := strings.ToLower(candidate)
	if genericHeads[folded] || s.seen[folded] {
		return
	}
	s.seen[folded] = true
	s.items = append(s.items, candidate)
}

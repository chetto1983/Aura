package docfields

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/docfields/rules"
)

// The pass (spec decision 3): one matcher, in Go, over each newly indexed document and, when the
// operator saves a rule, over every document. What it finds are suggestions: suggest never
// overwrites a held value and never brings back a rejected one.

// maxPassText bounds the text one document is matched against. A pass over that much text with
// any rule a label can hold stays well under a second (TestPassNeverStallsOnPathologicalPattern).
const maxPassText = 1 << 20

// The header keys the extractor maps (filecard HeaderPair, as an office invoice writes them),
// compared as NameKey. Any other key goes to the custom field of that name, when there is one.
var (
	dateKeys         = []string{"data emissione"}
	counterpartyKeys = []string{"cliente", "fornitore"}
	dateLayouts      = []string{time.DateOnly, "02/01/2006", "2/1/2006"}
)

// DocumentText is what the pass reads of one indexed document.
type DocumentText struct {
	SourceKey   string
	IndexedAt   string
	FileName    string
	Card        string
	HeaderPairs []string
	// Text is the document's passages, in order.
	Text string
}

// PassResult counts what a pass did.
type PassResult struct {
	Documents int `json:"documents"`
	Suggested int `json:"suggested"`
}

type rule struct {
	label   Label
	matcher rules.Matcher
}

// passLists is what a document is matched against: the labels that carry a rule, the
// counterparties by name and the custom fields by name.
type passLists struct {
	rules          []rule
	counterparties map[string]string
	customFields   map[string]string
}

func compileLists(labels []Label, customFields map[string]string) (passLists, error) {
	l := passLists{counterparties: map[string]string{}, customFields: customFields}
	for _, label := range labels {
		if label.Kind == KindCounterparty {
			l.counterparties[NameKey(label.Name)] = label.ID
		}
		if label.Algorithm == "" || label.Algorithm == rules.None {
			continue
		}
		matcher, err := rules.Compile(label.Algorithm, label.Pattern, label.CaseInsensitive)
		if err != nil {
			return passLists{}, fmt.Errorf("label %s: %w", label.ID, err)
		}
		l.rules = append(l.rules, rule{label: label, matcher: matcher})
	}
	return l, nil
}

// suggestions is every value the document suggests: the extractor's first, from its header pairs
// in order, then the rules'. For a single-valued field the first one wins, in suggest.
func (l passLists) suggestions(doc DocumentText) []Value {
	var out []Value
	for _, pair := range doc.HeaderPairs {
		if v, ok := l.extract(pair); ok {
			out = append(out, v)
		}
	}
	text := rules.NewText(matchText(doc))
	for _, r := range l.rules {
		if r.matcher.Match(text) {
			out = append(out, Value{Field: fieldOf(r.label.Kind), ValueKey: r.label.ID, SetBy: setByRulePrefix + r.label.ID,
				Evidence: fmt.Sprintf("%s %q", r.label.Algorithm, r.label.Pattern)})
		}
	}
	return out
}

func (l passLists) extract(pair string) (Value, bool) {
	key, value, ok := strings.Cut(pair, ": ")
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		return Value{}, false
	}
	v := Value{SetBy: SetByExtractor, Evidence: pair}
	switch name := NameKey(key); {
	case slices.Contains(dateKeys, name):
		date, ok := isoDate(value)
		if !ok {
			return Value{}, false
		}
		v.Field, v.ValueKey = FieldDocumentDate, date
	case slices.Contains(counterpartyKeys, name):
		v.Field = FieldCounterparty
		if id, known := l.counterparties[NameKey(value)]; known {
			v.ValueKey = id
		} else {
			v.ValueKey, v.Display = proposedPrefix+value, value
		}
	case l.customFields[name] != "":
		v.Field, v.ValueKey, v.Display = customFieldPrefix+l.customFields[name], value, value
	default:
		return Value{}, false
	}
	return v, true
}

func fieldOf(kind Kind) string {
	switch kind {
	case KindType:
		return FieldType
	case KindCounterparty:
		return FieldCounterparty
	default:
		return FieldTag
	}
}

func isoDate(value string) (string, bool) {
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.Format(time.DateOnly), true
		}
	}
	return "", false
}

// matchText is the text the rules read: the name, the card, the pairs and the passages, bounded.
func matchText(doc DocumentText) string {
	text := strings.Join(append([]string{doc.FileName, doc.Card}, append(doc.HeaderPairs, doc.Text)...), "\n")
	if len(text) > maxPassText {
		text = text[:maxPassText]
	}
	return text
}

// RunPass suggests values for the documents no pass has read at their current indexed_at, a batch
// at a time, and marks them read. An identity with nothing indexed costs one schema read.
func (s *Store) RunPass(ctx context.Context, identityID string) (PassResult, error) {
	c, err := s.tenants.For(ctx, identityID)
	if err != nil {
		return PassResult{}, fmt.Errorf("document fields pass for %s: %w", identityID, err)
	}
	indexed, err := hasIndexedDocuments(ctx, c)
	if err != nil || !indexed {
		return PassResult{}, err
	}
	docs, err := documentsDue(ctx, c, passBatch)
	if err != nil || len(docs) == 0 {
		return PassResult{}, err
	}
	lists, err := s.passLists(ctx, identityID)
	if err != nil {
		return PassResult{}, err
	}
	result, err := s.suggestAll(ctx, identityID, docs, lists)
	if err != nil {
		return result, err
	}
	return result, markPassed(ctx, c, docs)
}

// RunRule runs one label's rule over every indexed document: what saving a rule does.
func (s *Store) RunRule(ctx context.Context, identityID, labelID string) (PassResult, error) {
	label, err := s.label(ctx, identityID, labelID)
	if err != nil {
		return PassResult{}, err
	}
	lists, err := compileLists([]Label{label}, nil)
	if err != nil || len(lists.rules) == 0 {
		return PassResult{}, err
	}
	c, err := s.client(ctx, identityID)
	if err != nil {
		return PassResult{}, err
	}
	var total PassResult
	for after := ""; ; {
		docs, err := documentsFrom(ctx, c, after, passBatch)
		if err != nil {
			return total, err
		}
		if len(docs) == 0 {
			return total, nil
		}
		result, err := s.suggestAll(ctx, identityID, docs, lists)
		total.Documents += result.Documents
		total.Suggested += result.Suggested
		if err != nil {
			return total, err
		}
		after = docs[len(docs)-1].SourceKey
	}
}

func (s *Store) suggestAll(ctx context.Context, identityID string, docs []DocumentText, lists passLists) (PassResult, error) {
	result := PassResult{Documents: len(docs)}
	for _, doc := range docs {
		vs := lists.suggestions(doc)
		if len(vs) == 0 {
			continue
		}
		record, err := s.Record(ctx, identityID, doc.SourceKey)
		if err != nil {
			return result, err
		}
		n, err := s.Suggest(ctx, identityID, record.RecordID, vs)
		if err != nil {
			return result, fmt.Errorf("suggest for %s: %w", doc.SourceKey, err)
		}
		result.Suggested += n
	}
	return result, nil
}

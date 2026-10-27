package docfields

import (
	"slices"
	"strings"
	"time"
)

// The fields a value can sit on. A custom field is "custom:<definition id>".
const (
	FieldType         = "type"
	FieldCounterparty = "counterparty"
	FieldDocumentDate = "document_date"
	FieldTag          = "tag"
	customFieldPrefix = "custom:"
)

// Who set a value. A rule is "rule:<label id>". The CHECK-equivalent lives in validSetBy;
// classifier and llm are reserved for phases 2 and 3 and nothing writes them yet.
const (
	SetByOperator   = "operator"
	SetByExtractor  = "extractor"
	SetByCopied     = "copied"
	SetByClassifier = "classifier"
	SetByLLM        = "llm"
	setByRulePrefix = "rule:"
)

// Action names one entry of a document's history.
type Action string

// The actions a history entry records.
const (
	ActionSet      Action = "set"
	ActionAccepted Action = "accepted"
	ActionRejected Action = "rejected"
	ActionRemoved  Action = "removed"
	ActionReviewed Action = "reviewed"
	ActionMoved    Action = "moved"
)

// Value is one field value on a document, with where it came from. ValueKey is the label id, the
// ISO date, or the value's canonical text; Display is what a custom field holds when the key is
// not enough to show it (a monetary amount with its currency).
type Value struct {
	Field    string    `json:"field"`
	ValueKey string    `json:"value_key"`
	Display  any       `json:"value,omitempty"`
	SetBy    string    `json:"set_by"`
	Accepted bool      `json:"accepted"`
	Evidence string    `json:"evidence,omitempty"`
	SetAt    time.Time `json:"set_at"`
}

// ValueRef names a value without its content: the unit of removal and of rejection.
type ValueRef struct {
	Field    string `json:"field"`
	ValueKey string `json:"value_key"`
}

// Diff is a change by difference: what to set and what to remove, nothing else. A bulk edit
// sends exactly this, so documents it does not mention keep their values.
type Diff struct {
	Set    []Value    `json:"set"`
	Remove []ValueRef `json:"remove"`
}

// Event is one entry of a document's history, and phase 2's training data.
type Event struct {
	RecordID string    `json:"record_id"`
	Field    string    `json:"field"`
	ValueKey string    `json:"value_key"`
	Action   Action    `json:"action"`
	Actor    string    `json:"actor"`
	Before   *Value    `json:"before,omitempty"`
	After    *Value    `json:"after,omitempty"`
	At       time.Time `json:"at"`
}

// Fields is one document's fields, as stored in DocumentFields.
type Fields struct {
	RecordID   string     `json:"record_id"`
	SourceKey  string     `json:"source_key"`
	Values     []Value    `json:"values"`
	Rejections []ValueRef `json:"rejections"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
}

// Projection is what the triggers copy onto the indexed rows. The OK columns hold only what the
// operator set or accepted, which is what include_unreviewed=false filters on.
type Projection struct {
	Type, TypeOK                 string
	Counterparty, CounterpartyOK string
	DocumentDate, DocumentDateOK string
	Tags, TagsOK                 []string
	// NeedsReview is a document holding a value the operator has not set or accepted: the
	// Documenti view's "da rivedere".
	NeedsReview bool
}

func singleValued(field string) bool { return field != FieldTag }

func (v Value) ref() ValueRef { return ValueRef{Field: v.Field, ValueKey: v.ValueKey} }

func (v Value) reviewed() bool { return v.SetBy == SetByOperator || v.Accepted }

// value returns the value a single-valued field holds, or the zero Value.
func (f *Fields) value(field string) Value {
	for _, v := range f.Values {
		if v.Field == field {
			return v
		}
	}
	return Value{}
}

func (f *Fields) index(ref ValueRef) int {
	return slices.IndexFunc(f.Values, func(v Value) bool { return v.ref() == ref })
}

func (f *Fields) rejected(ref ValueRef) bool { return slices.Contains(f.Rejections, ref) }

// apply changes the fields by d on behalf of actor and returns the history it wrote. Nothing
// below the operator changes an operator value; a value the operator removes and did not set
// is rejected, and never suggested again.
func (f *Fields) apply(d Diff, actor string, now time.Time) []Event {
	var events []Event
	for _, v := range d.Set {
		events = append(events, f.set(v, actor, now)...)
	}
	for _, ref := range d.Remove {
		events = append(events, f.remove(ref, actor, now)...)
	}
	return events
}

func (f *Fields) set(v Value, actor string, now time.Time) []Event {
	v.SetBy, v.SetAt = actor, now
	byOperator := actor == SetByOperator
	if byOperator {
		f.Rejections = slices.DeleteFunc(f.Rejections, func(r ValueRef) bool { return r == v.ref() })
	} else if f.rejected(v.ref()) {
		return nil
	}
	if i := f.index(v.ref()); i >= 0 {
		held := f.Values[i]
		if byOperator && !held.reviewed() {
			f.Values[i].Accepted = true
			return []Event{f.event(held.ref(), ActionAccepted, actor, &held, &f.Values[i], now)}
		}
		return nil
	}
	if singleValued(v.Field) {
		if i := slices.IndexFunc(f.Values, func(h Value) bool { return h.Field == v.Field }); i >= 0 {
			held := f.Values[i]
			if !byOperator && held.SetBy == SetByOperator {
				return nil
			}
			f.Values[i] = v
			return []Event{f.event(v.ref(), ActionSet, actor, &held, &f.Values[i], now)}
		}
	}
	f.Values = append(f.Values, v)
	return []Event{f.event(v.ref(), ActionSet, actor, nil, &f.Values[len(f.Values)-1], now)}
}

func (f *Fields) remove(ref ValueRef, actor string, now time.Time) []Event {
	i := f.index(ref)
	if i < 0 {
		return nil
	}
	held := f.Values[i]
	if actor != SetByOperator && held.SetBy == SetByOperator {
		return nil
	}
	f.Values = slices.Delete(f.Values, i, i+1)
	events := []Event{f.event(ref, ActionRemoved, actor, &held, nil, now)}
	if actor == SetByOperator && !f.rejected(ref) {
		f.Rejections = append(f.Rejections, ref)
		events = append(events, f.event(ref, ActionRejected, actor, &held, nil, now))
	}
	return events
}

// suggest adds the values a rule or the extractor proposes, into empty single fields and as
// absent tags only, never over a held value and never a rejected one. It returns the history of
// what it added.
func (f *Fields) suggest(vs []Value, now time.Time) []Event {
	var events []Event
	for _, v := range vs {
		if v.SetBy == SetByOperator || f.rejected(v.ref()) || f.index(v.ref()) >= 0 {
			continue
		}
		if singleValued(v.Field) && f.value(v.Field).Field != "" {
			continue
		}
		v.SetAt = now
		f.Values = append(f.Values, v)
		events = append(events, f.event(v.ref(), ActionSet, v.SetBy, nil, &f.Values[len(f.Values)-1], now))
	}
	return events
}

// markReviewed accepts every value: a reviewed document's suggestions weigh as the operator's.
func (f *Fields) markReviewed(now time.Time) []Event {
	for i := range f.Values {
		f.Values[i].Accepted = true
	}
	f.ReviewedAt = &now
	return []Event{f.event(ValueRef{}, ActionReviewed, SetByOperator, nil, nil, now)}
}

func (f *Fields) projection() Projection {
	var p Projection
	for _, v := range f.Values {
		p.NeedsReview = p.NeedsReview || !v.reviewed()
		if strings.HasPrefix(v.ValueKey, proposedPrefix) {
			continue
		}
		ok := ""
		if v.reviewed() {
			ok = v.ValueKey
		}
		switch v.Field {
		case FieldType:
			p.Type, p.TypeOK = v.ValueKey, ok
		case FieldCounterparty:
			p.Counterparty, p.CounterpartyOK = v.ValueKey, ok
		case FieldDocumentDate:
			p.DocumentDate, p.DocumentDateOK = v.ValueKey, ok
		case FieldTag:
			p.Tags = append(p.Tags, v.ValueKey)
			if ok != "" {
				p.TagsOK = append(p.TagsOK, v.ValueKey)
			}
		}
	}
	return p
}

func (f *Fields) event(ref ValueRef, action Action, actor string, before, after *Value, now time.Time) Event {
	return Event{RecordID: f.RecordID, Field: ref.Field, ValueKey: ref.ValueKey, Action: action,
		Actor: actor, Before: copyValue(before), After: copyValue(after), At: now}
}

func copyValue(v *Value) *Value {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// validSetBy is the provenance contract: a known source, or a rule with its label id.
func validSetBy(setBy string) bool {
	switch setBy {
	case SetByOperator, SetByExtractor, SetByCopied, SetByClassifier, SetByLLM:
		return true
	}
	return strings.HasPrefix(setBy, setByRulePrefix) && len(setBy) > len(setByRulePrefix)
}

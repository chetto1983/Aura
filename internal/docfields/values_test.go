package docfields

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// keys returns the value keys a field holds, in order.
func (f *Fields) keys(field string) []string {
	var out []string
	for _, v := range f.Values {
		if v.Field == field {
			out = append(out, v.ValueKey)
		}
	}
	return out
}

var at = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func suggestion(field, key, setBy string) Value {
	return Value{Field: field, ValueKey: key, SetBy: setBy, Evidence: "Cliente: Gialli Import"}
}

func operator(field, key string) Value {
	return Value{Field: field, ValueKey: key, SetBy: SetByOperator}
}

func TestApplyOperatorReplacesASuggestion(t *testing.T) {
	f := Fields{Values: []Value{suggestion(FieldCounterparty, "lbl-rossi", "rule:lbl-rossi")}}
	events := f.apply(Diff{Set: []Value{operator(FieldCounterparty, "lbl-gialli")}}, SetByOperator, at)
	if got := f.value(FieldCounterparty); got.ValueKey != "lbl-gialli" || got.SetBy != SetByOperator {
		t.Fatalf("counterparty = %+v, want the operator's lbl-gialli", got)
	}
	if len(events) != 1 || events[0].Action != ActionSet || events[0].Before == nil || events[0].Before.ValueKey != "lbl-rossi" {
		t.Fatalf("events = %+v, want one set with the replaced value as before", events)
	}
}

// Nothing below the operator changes an operator value (Global Constraints).
func TestApplyNeverOverwritesAnOperatorValue(t *testing.T) {
	f := Fields{Values: []Value{operator(FieldType, "lbl-fattura")}}
	for _, actor := range []string{SetByExtractor, "rule:lbl-contratto", SetByCopied} {
		events := f.apply(Diff{Set: []Value{suggestion(FieldType, "lbl-contratto", actor)}}, actor, at)
		if got := f.value(FieldType); got.ValueKey != "lbl-fattura" || len(events) != 0 {
			t.Fatalf("actor %s changed the operator's type: %+v, events %+v", actor, got, events)
		}
	}
}

// The operator removing a value they did not set records a rejection, and that value never
// comes back from a rule or the extractor (Review Focus 4).
func TestApplyRemovalRejectsAndTheValueNeverReturns(t *testing.T) {
	f := Fields{Values: []Value{suggestion(FieldTag, "lbl-urgente", "rule:lbl-urgente")}}
	events := f.apply(Diff{Remove: []ValueRef{{Field: FieldTag, ValueKey: "lbl-urgente"}}}, SetByOperator, at)
	if len(f.Values) != 0 || !slices.Contains(f.Rejections, ValueRef{Field: FieldTag, ValueKey: "lbl-urgente"}) {
		t.Fatalf("values %+v rejections %+v, want the tag gone and rejected", f.Values, f.Rejections)
	}
	if actions(events) != "removed,rejected" {
		t.Fatalf("events = %s, want removed,rejected", actions(events))
	}
	if added := len(f.suggest([]Value{suggestion(FieldTag, "lbl-urgente", "rule:lbl-urgente")}, at)); added != 0 || len(f.Values) != 0 {
		t.Fatalf("a rejected tag came back: added %d, values %+v", added, f.Values)
	}
}

// The operator removing a value they set themselves is a rejection too (spec decision 3.1): a rule
// on the same label must not bring back the tag they just took off. Setting it again lifts it.
func TestApplyOperatorRemovingTheirOwnValueRejectsIt(t *testing.T) {
	f := Fields{Values: []Value{operator(FieldTag, "lbl-urgente")}}
	events := f.apply(Diff{Remove: []ValueRef{{Field: FieldTag, ValueKey: "lbl-urgente"}}}, SetByOperator, at)
	if actions(events) != "removed,rejected" {
		t.Fatalf("events = %s, want removed,rejected", actions(events))
	}
	if added := len(f.suggest([]Value{suggestion(FieldTag, "lbl-urgente", "rule:lbl-urgente")}, at)); added != 0 {
		t.Fatalf("a rule brought back the tag the operator removed: values %+v", f.Values)
	}
}

// Confirming a suggestion accepts it and keeps where it came from.
func TestApplyOperatorConfirmingASuggestionAcceptsIt(t *testing.T) {
	f := Fields{Values: []Value{suggestion(FieldDocumentDate, "2026-03-27", SetByExtractor)}}
	events := f.apply(Diff{Set: []Value{operator(FieldDocumentDate, "2026-03-27")}}, SetByOperator, at)
	got := f.value(FieldDocumentDate)
	if got.SetBy != SetByExtractor || !got.Accepted || got.Evidence == "" {
		t.Fatalf("date = %+v, want the extractor's value accepted with its evidence", got)
	}
	if actions(events) != "accepted" {
		t.Fatalf("events = %s, want accepted", actions(events))
	}
}

func TestApplyTagsAreASet(t *testing.T) {
	f := Fields{}
	f.apply(Diff{Set: []Value{operator(FieldTag, "lbl-a"), operator(FieldTag, "lbl-b"), operator(FieldTag, "lbl-a")}}, SetByOperator, at)
	if got := f.keys(FieldTag); !slices.Equal(got, []string{"lbl-a", "lbl-b"}) {
		t.Fatalf("tags = %v, want [lbl-a lbl-b]", got)
	}
}

// An operator setting a value they once rejected lifts the rejection.
func TestApplyOperatorSettingARejectedValueLiftsTheRejection(t *testing.T) {
	f := Fields{Rejections: []ValueRef{{Field: FieldTag, ValueKey: "lbl-urgente"}}}
	f.apply(Diff{Set: []Value{operator(FieldTag, "lbl-urgente")}}, SetByOperator, at)
	if len(f.Rejections) != 0 {
		t.Fatalf("rejections = %+v, want lifted", f.Rejections)
	}
}

func TestSuggestFillsOnlyEmptyFieldsAndAbsentTags(t *testing.T) {
	f := Fields{Values: []Value{
		operator(FieldType, "lbl-fattura"),
		suggestion(FieldCounterparty, "lbl-rossi", "rule:lbl-rossi"),
		operator(FieldTag, "lbl-a"),
	}}
	added := len(f.suggest([]Value{
		suggestion(FieldType, "lbl-contratto", SetByExtractor),
		suggestion(FieldCounterparty, "lbl-gialli", SetByExtractor),
		suggestion(FieldDocumentDate, "2026-03-27", SetByExtractor),
		suggestion(FieldTag, "lbl-a", "rule:lbl-a"),
		suggestion(FieldTag, "lbl-b", "rule:lbl-b"),
	}, at))
	if added != 2 {
		t.Fatalf("added = %d, want 2 (the date and tag lbl-b)", added)
	}
	if f.value(FieldType).ValueKey != "lbl-fattura" || f.value(FieldCounterparty).ValueKey != "lbl-rossi" {
		t.Fatalf("suggest replaced a held value: %+v", f.Values)
	}
}

func TestMarkReviewedAcceptsEveryValue(t *testing.T) {
	f := Fields{Values: []Value{suggestion(FieldType, "lbl-fattura", "rule:lbl-fattura"), operator(FieldTag, "lbl-a")}}
	events := f.markReviewed(at)
	if f.ReviewedAt == nil || !f.Values[0].Accepted {
		t.Fatalf("fields = %+v, want reviewed and the suggestion accepted", f)
	}
	if actions(events) != "reviewed" {
		t.Fatalf("events = %s, want one reviewed", actions(events))
	}
}

// A proposed name the operator takes becomes the label adopted for it, accepted; a tag whose
// label the document already holds leaves only the label.
func TestAdoptReplacesAProposedNameByItsLabel(t *testing.T) {
	f := Fields{Values: []Value{
		suggestion(FieldCounterparty, proposedPrefix+"Verdi SpA", SetByExtractor),
		suggestion(FieldTag, proposedPrefix+"urgente", SetByExtractor),
		operator(FieldTag, "lbl-urgente"),
	}}
	events := f.adopt(map[ValueRef]string{
		{Field: FieldCounterparty, ValueKey: proposedPrefix + "Verdi SpA"}: "lbl-verdi",
		{Field: FieldTag, ValueKey: proposedPrefix + "urgente"}:            "lbl-urgente",
	}, at)
	if got := f.value(FieldCounterparty); got.ValueKey != "lbl-verdi" || !got.Accepted || got.SetBy != SetByExtractor {
		t.Fatalf("counterparty = %+v, want lbl-verdi accepted, still the extractor's", got)
	}
	if tags := f.keys(FieldTag); !slices.Equal(tags, []string{"lbl-urgente"}) {
		t.Fatalf("tags = %v, want the label once", tags)
	}
	if actions(events) != "accepted,removed" || events[0].Before.ValueKey != proposedPrefix+"Verdi SpA" {
		t.Fatalf("events = %s (%+v)", actions(events), events)
	}
}

// The projection is what the triggers copy onto the indexed rows: the _ok columns hold only
// what the operator set or accepted, so include_unreviewed=false filters on them.
func TestProjection(t *testing.T) {
	f := Fields{Values: []Value{
		suggestion(FieldType, "lbl-fattura", "rule:lbl-fattura"),
		operator(FieldCounterparty, "lbl-gialli"),
		{Field: FieldDocumentDate, ValueKey: "2026-03-27", SetBy: SetByExtractor, Accepted: true},
		operator(FieldTag, "lbl-a"),
		suggestion(FieldTag, "lbl-b", "rule:lbl-b"),
		{Field: "custom:cf-1", ValueKey: "2026_007", SetBy: SetByExtractor},
	}}
	got := f.projection()
	want := Projection{
		Type: "lbl-fattura", TypeOK: "",
		Counterparty: "lbl-gialli", CounterpartyOK: "lbl-gialli",
		DocumentDate: "2026-03-27", DocumentDateOK: "2026-03-27",
		Tags: []string{"lbl-a", "lbl-b"}, TagsOK: []string{"lbl-a"},
	}
	if got.Type != want.Type || got.TypeOK != want.TypeOK || got.Counterparty != want.Counterparty ||
		got.CounterpartyOK != want.CounterpartyOK || got.DocumentDate != want.DocumentDate ||
		got.DocumentDateOK != want.DocumentDateOK || !slices.Equal(got.Tags, want.Tags) || !slices.Equal(got.TagsOK, want.TagsOK) {
		t.Fatalf("projection = %+v, want %+v", got, want)
	}
	if !got.NeedsReview {
		t.Fatal("a document holding suggestions does not need review")
	}
	reviewed := Fields{Values: []Value{operator(FieldType, "lbl-fattura"), {Field: FieldTag, ValueKey: "lbl-b", SetBy: "rule:lbl-b", Accepted: true}}}
	if reviewed.projection().NeedsReview || (&Fields{}).projection().NeedsReview {
		t.Fatal("a document with every value reviewed, or none, needs review")
	}
	proposed := Fields{Values: []Value{{Field: FieldCounterparty, ValueKey: proposedPrefix + "Rossi SRL", SetBy: SetByExtractor}}}
	if !proposed.projection().NeedsReview {
		t.Fatal("a proposed name awaits the operator, so the document needs review")
	}
}

func actions(events []Event) string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, string(e.Action))
	}
	return strings.Join(out, ",")
}

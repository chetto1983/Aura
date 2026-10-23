package docfields

import (
	"slices"
	"testing"
)

// A document's fields by name: reviewed is what the operator set or accepted, and what no filter
// can match (a proposed new name, a label since deleted, a custom field) is left out.
func TestNameFieldsByLabelAndReview(t *testing.T) {
	names := map[string]string{"l-fattura": "Fattura", "l-acme": "ACME SpA", "l-interno": "interno", "l-bozza": "bozza"}
	got := nameFields([]Value{
		{Field: FieldType, ValueKey: "l-fattura", SetBy: SetByOperator},
		{Field: FieldCounterparty, ValueKey: "l-acme", SetBy: SetByExtractor},
		{Field: FieldDocumentDate, ValueKey: "2026-01-12", SetBy: SetByExtractor, Accepted: true},
		{Field: FieldTag, ValueKey: "l-interno", SetBy: "rule:l-interno"},
		{Field: FieldTag, ValueKey: "l-bozza", SetBy: "rule:l-bozza", Accepted: true},
		{Field: FieldTag, ValueKey: "l-gone", SetBy: SetByOperator},
		{Field: FieldTag, ValueKey: proposedPrefix + "Riservato", SetBy: SetByExtractor},
		{Field: customFieldPrefix + "cf-stato", ValueKey: "aperto", SetBy: SetByOperator},
	}, names)
	if got.Type == nil || *got.Type != (NamedValue{Name: "Fattura", Reviewed: true}) {
		t.Fatalf("type = %+v", got.Type)
	}
	if got.Counterparty == nil || *got.Counterparty != (NamedValue{Name: "ACME SpA"}) {
		t.Fatalf("counterparty = %+v", got.Counterparty)
	}
	if got.DocumentDate == nil || *got.DocumentDate != (NamedValue{Name: "2026-01-12", Reviewed: true}) {
		t.Fatalf("document date = %+v", got.DocumentDate)
	}
	if want := []NamedValue{{Name: "interno"}, {Name: "bozza", Reviewed: true}}; !slices.Equal(got.Tags, want) {
		t.Fatalf("tags = %+v, want %+v", got.Tags, want)
	}
}

func TestNameFieldsOfAnUnlabelledDocumentIsEmpty(t *testing.T) {
	if got := nameFields(nil, map[string]string{}); got.Type != nil || got.Counterparty != nil || got.DocumentDate != nil || got.Tags != nil {
		t.Fatalf("fields = %+v", got)
	}
}

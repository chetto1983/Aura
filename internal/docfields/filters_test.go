package docfields

import (
	"slices"
	"testing"
)

var lists = []Label{
	{ID: "l-gialli", Kind: KindCounterparty, Name: "Gialli Import"},
	{ID: "l-acme", Kind: KindCounterparty, Name: "ACME SpA"},
	{ID: "l-rossi", Kind: KindCounterparty, Name: "Rossi SRL"},
	{ID: "l-rossi-snc", Kind: KindCounterparty, Name: "Rossi Snc"},
	{ID: "l-bianchi", Kind: KindCounterparty, Name: "Bianchi & Figli"},
}

// Names match trimmed and case-folded, exactly: no accent folding, no prefix match (Review
// Focus 1), and a name given twice is one id.
func TestResolveNamesTrimsAndFolds(t *testing.T) {
	ids, unknown := resolve(lists, []string{"  gialli IMPORT ", "acme spa", "ACME SpA"})
	if !slices.Equal(ids, []string{"l-gialli", "l-acme"}) || len(unknown) != 0 {
		t.Fatalf("ids %v, unknown %v", ids, unknown)
	}
	if ids, unknown := resolve(lists, []string{"Gialli"}); len(ids) != 0 || len(unknown) != 1 {
		t.Fatalf("a prefix matched: ids %v, unknown %v", ids, unknown)
	}
}

func TestUnknownNameReturnsClosest(t *testing.T) {
	ids, unknown := resolve(lists, []string{"Rosi SRL", "Gialli Import"})
	if !slices.Equal(ids, []string{"l-gialli"}) {
		t.Fatalf("ids = %v", ids)
	}
	if got := unknown["Rosi SRL"]; !slices.Equal(got, []string{"Rossi SRL", "Rossi Snc"}) {
		t.Fatalf("closest to Rosi SRL = %v", got)
	}
	if _, unknown := resolve(nil, []string{"Rossi"}); unknown["Rossi"] == nil || len(unknown["Rossi"]) != 0 {
		t.Fatalf("an empty list suggests %v, want an empty list", unknown["Rossi"])
	}
}

// A closest name resembles the unknown one: within half the longer name by edit distance, or one
// containing the other. The agent's own probes on the lab VM got the only tag for all three of
// fatture, contratti and a nonsense name (spec §5, amended a third time 2026-10-23).
func TestClosestNamesAreSimilar(t *testing.T) {
	labels := []Label{
		{ID: "l-cliente", Kind: KindTag, Name: "Cliente"},
		{ID: "l-gialli", Kind: KindCounterparty, Name: "Gialli Import"},
		{ID: "l-fattura", Kind: KindType, Name: "Fattura"},
	}
	for name, want := range map[string][]string{
		"fatture":                  {"Fattura"},
		"contratti":                {},
		"__tag_test_inesistente__": {},
		"x":                        {},
		"Gialli":                   {"Gialli Import"},
	} {
		if _, unknown := resolve(labels, []string{name}); !slices.Equal(unknown[name], want) {
			t.Errorf("closest to %q = %v, want %v", name, unknown[name], want)
		}
	}
}

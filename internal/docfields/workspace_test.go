package docfields

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestSavedViewValidation(t *testing.T) {
	base := SavedView{Name: "Invoices", Version: 1, Query: "q=Cedar&search_mode=content&type=invoice&tags_mode=all&inbox=true&limit=25", Display: "cards"}
	for name, change := range map[string]func(*SavedView){
		"empty name": func(v *SavedView) { v.Name = " " },
		"long name":  func(v *SavedView) { v.Name = strings.Repeat("é", 81) },
		"version":    func(v *SavedView) { v.Version = 2 },
		"display":    func(v *SavedView) { v.Display = "hidden" },
		"oversize":   func(v *SavedView) { v.Query = "q=" + strings.Repeat("a", 16384) },
		"identity":   func(v *SavedView) { v.Query = "identity_id=other" },
		"scope":      func(v *SavedView) { v.Query = "scope=all" },
		"unknown":    func(v *SavedView) { v.Query = "surprise=1" },
		"mode":       func(v *SavedView) { v.Query = "search_mode=agent" },
		"boolean":    func(v *SavedView) { v.Query = "inbox=maybe" },
		"sort":       func(v *SavedView) { v.Query = "sort=secret" },
		"date":       func(v *SavedView) { v.Query = "date_from=tomorrow" },
		"custom": func(v *SavedView) {
			v.Query = "custom=" + url.QueryEscape(`[{"field":"type","operator":"eq","value":"x"}]`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := base
			change(&v)
			_, err := New(nil).SaveView(context.Background(), "owner", v)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if err := validateView(&base); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceFilterValidation(t *testing.T) {
	for _, q := range []ListQuery{
		{SearchMode: "other"}, {Custom: []CustomFilter{{Field: "custom:x", Operator: "range", Value: "1"}}},
		{Custom: []CustomFilter{{Field: "custom:", Operator: "eq", Value: "1"}}},
		{Custom: []CustomFilter{{Field: "custom:x", Operator: "unassigned", Value: "1"}}},
	} {
		if _, err := libraryPredicates(q, map[string]any{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", q, err)
		}
	}
}

func TestCustomFilterQueryBoundary(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[] []`, `[{"field":"custom:x","operator":"eq","value":3}]`, `[{"field":"custom:x","operator":"eq","value":"paid","scope":"other"}]`} {
		if _, err := ParseCustomFilters(raw); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", raw, err)
		}
	}
	filters, err := ParseCustomFilters(`[{"field":"custom:x","operator":"eq","value":"paid"},{"field":"custom:y","operator":"unassigned"}]`)
	if err != nil || len(filters) != 2 || filters[0].Value != "paid" || filters[1].Operator != "unassigned" {
		t.Fatalf("filters=%+v %v", filters, err)
	}
}

// A scope is the files and the folders it names, one OR clause beside the other predicates, the
// same clause the search builds; past 200 entries it is refused before any query.
func TestListScopeJoinsKeysAndFolders(t *testing.T) {
	params := map[string]any{}
	predicates, err := libraryPredicates(ListQuery{FileType: "all", SourceKeys: []string{"altro/c.pdf"}, SourcePrefixes: []string{"archivio/"}}, params)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(predicates, "(source_key IN :source_keys OR source_key LIKE :source_prefix_0)") || params["source_prefix_0"] != "archivio/%" {
		t.Fatalf("predicates = %v, params = %v", predicates, params)
	}
	wide := ListQuery{SourcePrefixes: make([]string, 150), SourceKeys: make([]string, 51)}
	if _, err := libraryPredicates(wide, map[string]any{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a scope of 201 entries: %v", err)
	}
}

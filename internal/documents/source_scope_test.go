package documents

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeSourceScopesCanonicalizesDeduplicatesAndSorts(t *testing.T) {
	got, err := NormalizeSourceScopes([]SourceScope{
		{Kind: SourceScopeFolder, Path: `/zeta//2026/`},
		{Kind: SourceScopeFile, Path: `\alpha\report.pdf`},
		{Kind: SourceScopeFile, Path: `/alpha/report.pdf`},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []SourceScope{
		{Kind: SourceScopeFile, Path: "alpha/report.pdf"},
		{Kind: SourceScopeFolder, Path: "zeta/2026"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %#v, want %#v", got, want)
	}
}

func TestNormalizeSourceScopesRejectsInvalidInput(t *testing.T) {
	tests := map[string][]SourceScope{
		"unknown kind": {{Kind: "bucket", Path: "docs"}},
		"blank path":   {{Kind: SourceScopeFile, Path: "  "}},
		"root folder":  {{Kind: SourceScopeFolder, Path: "/"}},
		"traversal":    {{Kind: SourceScopeFolder, Path: "docs/../private"}},
		"dot segment":  {{Kind: SourceScopeFile, Path: "docs/./report.pdf"}},
		"nul":          {{Kind: SourceScopeFile, Path: "docs/\x00report.pdf"}},
		"overlong":     {{Kind: SourceScopeFile, Path: strings.Repeat("x", MaxSourceScopePathRunes+1)}},
	}
	tooMany := make([]SourceScope, MaxSourceScopes+1)
	for i := range tooMany {
		tooMany[i] = SourceScope{Kind: SourceScopeFile, Path: string(rune('a' + i))}
	}
	tests["too many"] = tooMany
	for name, scopes := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeSourceScopes(scopes); err == nil {
				t.Fatalf("accepted %#v", scopes)
			}
		})
	}
}

func TestSourceScopesRoundTripThroughContextWithoutAliasing(t *testing.T) {
	scopes := []SourceScope{{Kind: SourceScopeFolder, Path: "docs"}}
	ctx := WithSourceScopes(t.Context(), scopes)
	scopes[0].Path = "mutated"
	got := SourceScopesFromContext(ctx)
	if !reflect.DeepEqual(got, []SourceScope{{Kind: SourceScopeFolder, Path: "docs"}}) {
		t.Fatalf("context scopes = %#v", got)
	}
	got[0].Path = "also-mutated"
	if again := SourceScopesFromContext(ctx); again[0].Path != "docs" {
		t.Fatalf("context returned aliased data: %#v", again)
	}
}

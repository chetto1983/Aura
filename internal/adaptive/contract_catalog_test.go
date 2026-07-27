package adaptive

import (
	"strings"
	"testing"
)

// contract_catalog.go sat at 40% under the db_integration-only gate, including the
// sensitiveCatalogID guard — the thing standing between a secret-shaped identifier and a
// persisted exposure record. Nothing exercised it.

func TestNewRegisteredCatalogDeliveryMetadataRanksToolsAndSkills(t *testing.T) {
	for _, tt := range []struct {
		name string
		kind ResultKind
	}{
		{name: "tools", kind: ResultTool},
		{name: "skills", kind: ResultSkill},
	} {
		t.Run(tt.name, func(t *testing.T) {
			results, revisions, err := NewRegisteredCatalogDeliveryMetadata(
				tt.kind,
				[]string{"alpha", "beta", "gamma"},
				[]string{"gamma", "alpha"},
				"catalog-v7",
			)
			if err != nil {
				t.Fatalf("NewRegisteredCatalogDeliveryMetadata: %v", err)
			}

			want := []string{"gamma", "alpha"}
			if len(results) != len(want) {
				t.Fatalf("results = %d, want %d", len(results), len(want))
			}
			for i, id := range want {
				if results[i].id != id || results[i].kind != tt.kind {
					t.Errorf("results[%d] = %v/%v, want %v/%v",
						i, results[i].kind, results[i].id, tt.kind, id)
				}
			}
			// The catalog revision is a SCHEMA revision: it pins the ordering contract,
			// not mutable result content.
			if len(revisions.values) != 1 {
				t.Fatalf("revisions = %d, want 1", len(revisions.values))
			}
			if got := revisions.values[RevisionSchema].value; got != "catalog-v7" {
				t.Errorf("schema revision = %q, want %q", got, "catalog-v7")
			}
		})
	}
}

func TestNewRegisteredCatalogDeliveryMetadataRejections(t *testing.T) {
	for _, tt := range []struct {
		name            string
		kind            ResultKind
		registered      []string
		delivered       []string
		catalogRevision string
		wantErr         string
	}{
		{
			name:            "chunks do not belong to a ranked catalog",
			kind:            ResultChunk,
			registered:      []string{"alpha"},
			catalogRevision: "catalog-v7",
			wantErr:         "kind",
		},
		{
			name:            "delivered entry was never a candidate",
			kind:            ResultTool,
			registered:      []string{"alpha"},
			delivered:       []string{"beta"},
			catalogRevision: "catalog-v7",
			wantErr:         "not registered",
		},
		{
			name:            "the same entry is ranked twice",
			kind:            ResultSkill,
			registered:      []string{"alpha"},
			delivered:       []string{"alpha", "alpha"},
			catalogRevision: "catalog-v7",
			wantErr:         "duplicated",
		},
		{
			name:            "candidate ID is not a safe slug",
			kind:            ResultTool,
			registered:      []string{"Alpha"},
			catalogRevision: "catalog-v7",
			wantErr:         "invalid",
		},
		{
			name:            "catalog revision is mutable",
			kind:            ResultTool,
			registered:      []string{"alpha"},
			delivered:       []string{"alpha"},
			catalogRevision: "latest",
			wantErr:         "invalid",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			results, revisions, err := NewRegisteredCatalogDeliveryMetadata(
				tt.kind, tt.registered, tt.delivered, tt.catalogRevision,
			)
			if err == nil {
				t.Fatalf("accepted %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
			if results != nil || len(revisions.values) != 0 {
				t.Errorf("rejection returned usable metadata: %#v / %#v", results, revisions.values)
			}
		})
	}
}

func TestSensitiveCatalogIDBlocksSecretShapedIdentifiers(t *testing.T) {
	// Separator-insensitive by construction: '_' and '.' normalize to '-', so a token
	// cannot slip through by changing its punctuation.
	blocked := []string{
		"api-key",
		"api_key",
		"api.key",
		"API-KEY",
		"private-summary",
		"apikey-store",
		"secret",
		"secrets-manager",
		"password-reset",
		"credential-store",
		"bearer-token",
		"token",
		"content-hash",
		"fetch-tokens",
		// Matching is by PREFIX within a token, so the guard deliberately over-blocks an
		// innocent word rather than risk letting a content identifier through.
		"contented",
	}
	for _, value := range blocked {
		if !sensitiveCatalogID(value) {
			t.Errorf("sensitiveCatalogID(%q) = false, want it blocked", value)
		}
	}

	allowed := []string{
		"web-search",
		"shell-exec",
		"document-search",
		"keystone", // "key" alone is not a blocked prefix
	}
	for _, value := range allowed {
		if sensitiveCatalogID(value) {
			t.Errorf("sensitiveCatalogID(%q) = true, want it allowed", value)
		}
	}
}

func TestValidImmutableRevisionIDAcceptsOnlyPinnedForms(t *testing.T) {
	valid := []string{
		"parser-v2",                // v<digits> segment
		"v13",                      // bare version
		"index/hnsw-v1",            // path separator
		"model@2026",               // trailing numeric build after '@'
		"abcdef012345",             // 12-char lower hex
		"chunker-abcdef0123456789", // hex segment inside a name
		"summary-v1234",            // "summary" alone is not the blocked "private-summary"
	}
	for _, value := range valid {
		if !validImmutableRevisionID(value) {
			t.Errorf("validImmutableRevisionID(%q) = false, want true", value)
		}
	}

	invalid := []string{
		"",             // empty
		"latest",       // mutable tag — the whole point of the check
		"main",         // branch name
		"parser",       // no version segment
		"v",            // 'v' alone is not a version
		"vX",           // not digits
		"abcdefg01234", // 'g' is not lower hex
		"model@",       // '@' with no build
		"model@beta",   // non-numeric build
		"api-key-v2",   // pinned, but secret-shaped
		"@2026",        // '@' at index 0 is not a suffix form
		"ABCDEF012345", // uppercase hex is not accepted
		"parser-V2",    // uppercase version marker
		"token-v1",     // secret-shaped even when pinned
		"contents-v1",  // content prefix
	}
	for _, value := range invalid {
		if validImmutableRevisionID(value) {
			t.Errorf("validImmutableRevisionID(%q) = true, want false", value)
		}
	}
}

func TestASCIIDigitAndLowerHexPredicatesRejectEmpty(t *testing.T) {
	// Both predicates loop before their emptiness check, so an empty string would
	// otherwise fall through as "every character passed".
	if allASCIIDigits("") {
		t.Error("allASCIIDigits(\"\") = true, want false")
	}
	if allLowerHex("") {
		t.Error("allLowerHex(\"\") = true, want false")
	}
	if !allASCIIDigits("0123456789") {
		t.Error("allASCIIDigits(digits) = false, want true")
	}
	if allASCIIDigits("12a") {
		t.Error("allASCIIDigits(\"12a\") = true, want false")
	}
	if !allLowerHex("0123456789abcdef") {
		t.Error("allLowerHex(hex) = false, want true")
	}
	if allLowerHex("0g") {
		t.Error("allLowerHex(\"0g\") = true, want false")
	}
}

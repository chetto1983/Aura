package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/wiki"
)

// TestExtractorPrompt_IncludesAliasInstruction verifies that buildExtractionPrompt
// contains the alias rule paragraph in the rules section.
func TestExtractorPrompt_IncludesAliasInstruction(t *testing.T) {
	req := ExtractionRequest{SourceID: "src_test", Body: "test body"}
	prompt := buildExtractionPrompt(req)
	if !strings.Contains(prompt, "aliases") {
		t.Fatal("prompt missing 'aliases' instruction in rules section")
	}
	if !strings.Contains(prompt, "surface forms") {
		t.Fatal("prompt missing 'surface forms' description")
	}
}

// TestExtractorPrompt_SchemaIncludesAliasesField verifies the output schema example
// shows aliases for both entities and concepts.
func TestExtractorPrompt_SchemaIncludesAliasesField(t *testing.T) {
	req := ExtractionRequest{SourceID: "src_test", Body: "test body"}
	prompt := buildExtractionPrompt(req)
	// Schema example must show aliases in entity and concept sections.
	if !strings.Contains(prompt, `"aliases": ["surface form 1"`) {
		t.Fatalf("prompt schema missing aliases example; got snippet:\n%s",
			prompt[max(0, strings.Index(prompt, "Output schema")):])
	}
}

// TestExtractor_ParsesAliasesField verifies that aliases returned by the LLM
// are correctly parsed into ExtractedEntity.Aliases and ExtractedConcept.Aliases.
func TestExtractor_ParsesAliasesField(t *testing.T) {
	fake := &fakeLLMClient{response: `{
		"entities": [
			{"slug": "siemens", "title": "Siemens", "type": "org", "short_description": "German conglomerate.", "aliases": ["Siemens AG", "SIEMENS"]}
		],
		"concepts": [
			{"slug": "graph-memory", "title": "Graph Memory", "summary": "Wiki as a compounding KG.", "aliases": ["graph memory", "KG approach"]}
		],
		"links": []
	}`}
	ex := NewLLMExtractor(fake, "model")
	delta, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "body"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(delta.Entities[0].Aliases) != 2 {
		t.Fatalf("entity aliases = %v, want [Siemens AG, SIEMENS]", delta.Entities[0].Aliases)
	}
	if delta.Entities[0].Aliases[0] != "Siemens AG" || delta.Entities[0].Aliases[1] != "SIEMENS" {
		t.Fatalf("entity aliases = %v, want [Siemens AG, SIEMENS]", delta.Entities[0].Aliases)
	}
	if len(delta.Concepts[0].Aliases) != 2 {
		t.Fatalf("concept aliases = %v, want 2 entries", delta.Concepts[0].Aliases)
	}
}

// TestExtractor_DropsEmptyAliases verifies that empty or whitespace-only alias
// entries are silently dropped during validation.
func TestExtractor_DropsEmptyAliases(t *testing.T) {
	fake := &fakeLLMClient{response: `{
		"entities": [
			{"slug": "alice", "title": "Alice", "type": "person", "short_description": "lead.", "aliases": ["", "  ", "Alice Wonderland"]}
		],
		"concepts": [],
		"links": []
	}`}
	ex := NewLLMExtractor(fake, "model")
	delta, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "body"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(delta.Entities[0].Aliases) != 1 || delta.Entities[0].Aliases[0] != "Alice Wonderland" {
		t.Fatalf("aliases = %v, want [Alice Wonderland]", delta.Entities[0].Aliases)
	}
}

// TestExtractor_RejectsAliasTooLong verifies that an alias exceeding 200 chars
// is rejected with a validation error.
func TestExtractor_RejectsAliasTooLong(t *testing.T) {
	longAlias := strings.Repeat("x", 201)
	fake := &fakeLLMClient{response: `{
		"entities": [
			{"slug": "alice", "title": "Alice", "type": "person", "short_description": "lead.", "aliases": ["` + longAlias + `"]}
		],
		"concepts": [],
		"links": []
	}`}
	ex := NewLLMExtractor(fake, "model")
	if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "body"}); err == nil {
		t.Fatal("expected validation error for alias > 200 chars")
	}
}

// TestExtractor_Accepts200CharAlias verifies that an alias of exactly 200 chars is accepted.
func TestExtractor_Accepts200CharAlias(t *testing.T) {
	alias200 := strings.Repeat("a", 200)
	fake := &fakeLLMClient{response: `{
		"entities": [
			{"slug": "alice", "title": "Alice", "type": "person", "short_description": "lead.", "aliases": ["` + alias200 + `"]}
		],
		"concepts": [],
		"links": []
	}`}
	ex := NewLLMExtractor(fake, "model")
	delta, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "body"})
	if err != nil {
		t.Fatalf("200-char alias should be accepted: %v", err)
	}
	if len(delta.Entities[0].Aliases) != 1 {
		t.Fatalf("aliases = %v, want 1 entry", delta.Entities[0].Aliases)
	}
}

// TestUpsertEntityPage_WritesAliasesToNewPage is the Siemens probe from the story:
// mock extractor returns 1 entity 'Siemens' with aliases=['Siemens AG', 'SIEMENS']
// → wiki page 'siemens' written with those aliases in frontmatter.
func TestUpsertEntityPage_WritesAliasesToNewPage(t *testing.T) {
	ex := &stubExtractor{
		delta: ExtractionDelta{
			Entities: []ExtractedEntity{
				{
					Slug:             "siemens",
					Title:            "Siemens",
					Type:             "org",
					ShortDescription: "German multinational conglomerate.",
					Aliases:          []string{"Siemens AG", "SIEMENS"},
				},
			},
		},
	}
	env := newTestPipelineWithExtractor(t, ex)
	src := putOCRCompleteAs(t, env.sources, "siemens.pdf", "fake-siemens-bytes", "# Notes\n\nSiemens AG is a large company.")

	if _, err := env.pipeline.Compile(context.Background(), src.ID); err != nil {
		t.Fatalf("Compile: %v", err)
	}

	page, err := env.wiki.ReadPage("siemens")
	if err != nil {
		t.Fatalf("ReadPage(siemens): %v", err)
	}
	t.Logf("siemens page aliases: %v", page.Aliases)
	if len(page.Aliases) != 2 {
		t.Fatalf("aliases = %v, want [Siemens AG, SIEMENS]", page.Aliases)
	}
	if page.Aliases[0] != "Siemens AG" || page.Aliases[1] != "SIEMENS" {
		t.Fatalf("aliases = %v, want [Siemens AG, SIEMENS]", page.Aliases)
	}
}

// TestUpsertEntityPage_MergesAliasesFromSecondSource is the second Siemens probe:
// same entity ingested from a new source with aliases=['Siemens AG', 'Siemens S.p.A.']
// → final aliases set contains all 3 unique forms.
func TestUpsertEntityPage_MergesAliasesFromSecondSource(t *testing.T) {
	ex := &stubExtractor{
		delta: ExtractionDelta{
			Entities: []ExtractedEntity{
				{
					Slug:             "siemens",
					Title:            "Siemens",
					Type:             "org",
					ShortDescription: "First mention.",
					Aliases:          []string{"Siemens AG", "SIEMENS"},
				},
			},
		},
	}
	env := newTestPipelineWithExtractor(t, ex)

	// First ingest.
	src1 := putOCRCompleteAs(t, env.sources, "s1.pdf", "fake-siemens-1", "body 1")
	if _, err := env.pipeline.Compile(context.Background(), src1.ID); err != nil {
		t.Fatalf("Compile src1: %v", err)
	}

	// Second ingest: same entity, overlapping alias + new alias.
	ex.delta.Entities[0].ShortDescription = "Second mention."
	ex.delta.Entities[0].Aliases = []string{"Siemens AG", "Siemens S.p.A."}
	src2 := putOCRCompleteAs(t, env.sources, "s2.pdf", "fake-siemens-2", "body 2")
	if _, err := env.pipeline.Compile(context.Background(), src2.ID); err != nil {
		t.Fatalf("Compile src2: %v", err)
	}

	page, err := env.wiki.ReadPage("siemens")
	if err != nil {
		t.Fatalf("ReadPage(siemens): %v", err)
	}
	t.Logf("siemens page aliases after 2 ingests: %v", page.Aliases)

	// Must contain all 3 unique forms: Siemens AG, SIEMENS, Siemens S.p.A.
	wantAliases := map[string]bool{
		"Siemens AG":      false,
		"SIEMENS":         false,
		"Siemens S.p.A.": false,
	}
	for _, a := range page.Aliases {
		wantAliases[a] = true
	}
	for alias, found := range wantAliases {
		if !found {
			t.Errorf("alias %q missing from page.Aliases = %v", alias, page.Aliases)
		}
	}
	if len(page.Aliases) != 3 {
		t.Fatalf("aliases = %v, want exactly 3 unique entries", page.Aliases)
	}
}

// TestUpsertEntityPage_CaseFoldDedupAliases verifies that aliases differing only
// by case are not duplicated when merging.
func TestUpsertEntityPage_CaseFoldDedupAliases(t *testing.T) {
	ex := &stubExtractor{
		delta: ExtractionDelta{
			Entities: []ExtractedEntity{
				{
					Slug:             "alice",
					Title:            "Alice",
					Type:             "person",
					ShortDescription: "First.",
					Aliases:          []string{"alice wonderland"},
				},
			},
		},
	}
	env := newTestPipelineWithExtractor(t, ex)
	src1 := putOCRCompleteAs(t, env.sources, "a1.pdf", "fake-a1", "body a1")
	if _, err := env.pipeline.Compile(context.Background(), src1.ID); err != nil {
		t.Fatal(err)
	}

	// Second source sends same alias with different casing.
	ex.delta.Entities[0].ShortDescription = "Second."
	ex.delta.Entities[0].Aliases = []string{"Alice Wonderland"} // same as "alice wonderland" folded
	src2 := putOCRCompleteAs(t, env.sources, "a2.pdf", "fake-a2", "body a2")
	if _, err := env.pipeline.Compile(context.Background(), src2.ID); err != nil {
		t.Fatal(err)
	}

	page, err := env.wiki.ReadPage("alice")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("alice aliases: %v", page.Aliases)
	// Should have exactly 1 alias — the first-occurrence casing wins.
	if len(page.Aliases) != 1 {
		t.Fatalf("aliases = %v, want exactly 1 (case-fold dedup)", page.Aliases)
	}
	if page.Aliases[0] != "alice wonderland" {
		t.Fatalf("alias = %q, want 'alice wonderland' (first casing)", page.Aliases[0])
	}
}

// TestBackwardCompat_PageWithNoAliasesRoundtrips verifies that an existing page
// without aliases: field parses to Aliases=nil and roundtrips without corruption.
func TestBackwardCompat_PageWithNoAliasesRoundtrips(t *testing.T) {
	env := newTestPipelineWithExtractor(t, &stubExtractor{delta: ExtractionDelta{}})
	wikiStore := env.wiki

	// Write a page without aliases (simulates pre-GRAPH-01 page).
	page := &wiki.Page{
		Title:         "Legacy Page",
		Body:          "legacy body",
		Category:      "entity",
		Tags:          []string{"entity", "concept"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "ingest_v2",
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}
	if err := wikiStore.WritePage(context.Background(), page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	got, err := wikiStore.ReadPage("legacy-page")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(got.Aliases) != 0 {
		t.Fatalf("aliases = %v, want nil/empty for page written without aliases", got.Aliases)
	}
	if got.Title != "Legacy Page" || got.Body != "legacy body" {
		t.Fatalf("roundtrip corrupted page: title=%q body=%q", got.Title, got.Body)
	}
}

// TestUpsertConceptPage_WritesAliasesToNewPage verifies that concept aliases
// are persisted to the wiki page frontmatter on first write.
func TestUpsertConceptPage_WritesAliasesToNewPage(t *testing.T) {
	ex := &stubExtractor{
		delta: ExtractionDelta{
			Concepts: []ExtractedConcept{
				{
					Slug:    "graph-memory",
					Title:   "Graph Memory",
					Summary: "Wiki as a compounding KG.",
					Aliases: []string{"graph memory", "KG approach"},
				},
			},
		},
	}
	env := newTestPipelineWithExtractor(t, ex)
	src := putOCRCompleteAs(t, env.sources, "km.pdf", "fake-km", "body km")

	if _, err := env.pipeline.Compile(context.Background(), src.ID); err != nil {
		t.Fatalf("Compile: %v", err)
	}

	page, err := env.wiki.ReadPage("graph-memory")
	if err != nil {
		t.Fatalf("ReadPage(graph-memory): %v", err)
	}
	t.Logf("graph-memory aliases: %v", page.Aliases)
	if len(page.Aliases) != 2 {
		t.Fatalf("aliases = %v, want [graph memory, KG approach]", page.Aliases)
	}
}

// TestUpsertConceptPage_MergesAliasesFromSecondSource verifies concept alias
// accumulation across two ingests mirrors the entity behaviour.
func TestUpsertConceptPage_MergesAliasesFromSecondSource(t *testing.T) {
	ex := &stubExtractor{
		delta: ExtractionDelta{
			Concepts: []ExtractedConcept{
				{
					Slug:    "graph-memory",
					Title:   "Graph Memory",
					Summary: "First summary.",
					Aliases: []string{"graph memory"},
				},
			},
		},
	}
	env := newTestPipelineWithExtractor(t, ex)

	src1 := putOCRCompleteAs(t, env.sources, "c1.pdf", "fake-c1", "body c1")
	if _, err := env.pipeline.Compile(context.Background(), src1.ID); err != nil {
		t.Fatal(err)
	}
	ex.delta.Concepts[0].Summary = "Second summary."
	ex.delta.Concepts[0].Aliases = []string{"graph memory", "KG approach"}
	src2 := putOCRCompleteAs(t, env.sources, "c2.pdf", "fake-c2", "body c2")
	if _, err := env.pipeline.Compile(context.Background(), src2.ID); err != nil {
		t.Fatal(err)
	}

	page, err := env.wiki.ReadPage("graph-memory")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("graph-memory aliases: %v", page.Aliases)
	if len(page.Aliases) != 2 {
		t.Fatalf("aliases = %v, want [graph memory, KG approach]", page.Aliases)
	}
}

// TestMergeAliases unit-tests the helper directly.
func TestMergeAliases(t *testing.T) {
	cases := []struct {
		name     string
		existing []string
		incoming []string
		want     []string
	}{
		{
			"new alias added",
			[]string{"Siemens AG"},
			[]string{"SIEMENS"},
			[]string{"Siemens AG", "SIEMENS"},
		},
		{
			"case-fold dedup drops incoming",
			[]string{"Siemens AG"},
			[]string{"siemens ag"},
			[]string{"Siemens AG"},
		},
		{
			"empty incoming no change",
			[]string{"Siemens AG"},
			nil,
			[]string{"Siemens AG"},
		},
		{
			"both empty",
			nil,
			nil,
			nil,
		},
		{
			"empty strings in incoming dropped",
			[]string{"X"},
			[]string{"", "  ", "Y"},
			[]string{"X", "Y"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeAliases(tc.existing, tc.incoming)
			if len(got) != len(tc.want) {
				t.Fatalf("mergeAliases = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}


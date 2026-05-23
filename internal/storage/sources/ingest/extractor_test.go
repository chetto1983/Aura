package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

// fakeLLMClient returns a canned response for every Send call. The
// tests exercise the extractor's prompt building + JSON parsing +
// validation without needing a real LLM, and let us simulate malformed
// or hostile model output deterministically.
type fakeLLMClient struct {
	response string
	err      error
	// captured by Send so tests can assert prompt structure
	lastReq llm.Request
}

func (f *fakeLLMClient) Send(_ context.Context, req llm.Request) (llm.Response, error) {
	f.lastReq = req
	if f.err != nil {
		return llm.Response{}, f.err
	}
	return llm.Response{Content: f.response}, nil
}

// Stream is required by the llm.Client interface but not used by the
// extractor (which only invokes Send). Returning a closed channel is
// the cheapest "unsupported" answer.
func (f *fakeLLMClient) Stream(_ context.Context, _ llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token)
	close(ch)
	return ch, nil
}

func TestLLMExtractor_ParsesValidJSON(t *testing.T) {
	fake := &fakeLLMClient{response: `{
		"entities": [
			{"slug": "alice", "title": "Alice", "type": "person", "short_description": "Project lead."}
		],
		"concepts": [
			{"slug": "graph-memory", "title": "Graph Memory", "summary": "Wiki as a compounding KG.", "key_claims": ["wiki IS the graph"]}
		],
		"links": [
			{"from_slug": "alice", "to_slug": "graph-memory", "type": "mentions"}
		]
	}`}
	ex := NewLLMExtractor(fake, "deepseek-nano")
	delta, err := ex.Extract(context.Background(), ExtractionRequest{
		SourceID: "src_abc",
		Body:     "Alice writes about graph memory.",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(delta.Entities) != 1 || delta.Entities[0].Slug != "alice" {
		t.Fatalf("entities = %+v", delta.Entities)
	}
	if len(delta.Concepts) != 1 || delta.Concepts[0].Slug != "graph-memory" {
		t.Fatalf("concepts = %+v", delta.Concepts)
	}
	if len(delta.Links) != 1 || delta.Links[0].From != "alice" || delta.Links[0].To != "graph-memory" {
		t.Fatalf("links = %+v", delta.Links)
	}
}

func TestLLMExtractor_TolaratesMarkdownFencesAroundJSON(t *testing.T) {
	fake := &fakeLLMClient{response: "Sure! Here is the extraction:\n\n```json\n" + `{
		"entities": [{"slug": "x", "title": "X", "type": "concept", "short_description": "desc"}],
		"concepts": [],
		"links": []
	}` + "\n```\n\nLet me know if you want more."}
	ex := NewLLMExtractor(fake, "model")
	delta, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src_x", Body: "x body"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if delta.TotalItems() != 1 || delta.Entities[0].Slug != "x" {
		t.Fatalf("unexpected delta: %+v", delta)
	}
}

func TestLLMExtractor_RejectsOverLimit(t *testing.T) {
	var entries strings.Builder
	for i := range extractorMaxItems + 1 {
		if i > 0 {
			entries.WriteByte(',')
		}
		entries.WriteString(`{"slug":"e`)
		entries.WriteByte(byte('a' + i))
		entries.WriteString(`","title":"E","type":"concept","short_description":"d"}`)
	}
	fake := &fakeLLMClient{response: `{"entities":[` + entries.String() + `],"concepts":[],"links":[]}`}
	ex := NewLLMExtractor(fake, "model")
	if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"}); err == nil {
		t.Fatal("expected validation error for over-limit delta")
	}
}

// Wave 2.6 bug fix: malformed slugs are no longer rejected outright —
// canonicalizeSlugs derives the canonical slug from the title before
// validation, so the LLM's slug field becomes advisory. The test cases
// below confirm that each malformed slug gets REWRITTEN to the
// canonical Slug(title) instead of erroring the whole delta.
func TestLLMExtractor_CanonicalizesMalformedSlug(t *testing.T) {
	cases := map[string]struct {
		json string
		want string // expected canonical slug after canonicalizeSlugs runs
	}{
		"uppercase":       {`{"slug":"Alice","title":"Alice","type":"person","short_description":"d"}`, "alice"},
		"slug-from-title": {`{"slug":"foo","title":"Foo Bar","type":"person","short_description":"d"}`, "foo-bar"},
		"short-slug":      {`{"slug":"davide","title":"Davide Marchetto","type":"person","short_description":"d"}`, "davide-marchetto"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakeLLMClient{response: `{"entities":[` + tc.json + `],"concepts":[],"links":[]}`}
			ex := NewLLMExtractor(fake, "model")
			delta, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"})
			if err != nil {
				t.Fatalf("canonicalization should accept %s, got error: %v", name, err)
			}
			if delta.Entities[0].Slug != tc.want {
				t.Fatalf("%s canonicalized to %q, want %q", name, delta.Entities[0].Slug, tc.want)
			}
		})
	}
}

// canonicalizeSlugs must remap link references that pointed at the
// LLM's original (pre-canonical) slug to the new canonical slug, or
// every cross-reference in the delta breaks silently.
func TestLLMExtractor_CanonicalizeRemapsLinkReferences(t *testing.T) {
	fake := &fakeLLMClient{response: `{
		"entities":[
			{"slug":"davide","title":"Davide Marchetto","type":"person","short_description":"d"},
			{"slug":"aura","title":"Aura","type":"project","short_description":"d"}
		],
		"concepts":[],
		"links":[{"from_slug":"davide","to_slug":"aura","type":"extends"}]
	}`}
	ex := NewLLMExtractor(fake, "model")
	delta, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if delta.Entities[0].Slug != "davide-marchetto" {
		t.Fatalf("entity slug = %q, want davide-marchetto", delta.Entities[0].Slug)
	}
	if delta.Links[0].From != "davide-marchetto" {
		t.Fatalf("link.From = %q, want davide-marchetto (remap failed)", delta.Links[0].From)
	}
	if delta.Links[0].To != "aura" {
		t.Fatalf("link.To = %q, want aura (already canonical, not remapped)", delta.Links[0].To)
	}
}

func TestLLMExtractor_RejectsUnknownEntityType(t *testing.T) {
	fake := &fakeLLMClient{response: `{"entities":[{"slug":"x","title":"X","type":"alien","short_description":"d"}],"concepts":[],"links":[]}`}
	ex := NewLLMExtractor(fake, "model")
	if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"}); err == nil {
		t.Fatal("expected validation error for unknown type")
	}
}

func TestLLMExtractor_RejectsUnknownLinkType(t *testing.T) {
	fake := &fakeLLMClient{response: `{
		"entities":[{"slug":"a","title":"A","type":"concept","short_description":"d"},
		           {"slug":"b","title":"B","type":"concept","short_description":"d"}],
		"concepts":[],
		"links":[{"from_slug":"a","to_slug":"b","type":"yeets"}]
	}`}
	ex := NewLLMExtractor(fake, "model")
	if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"}); err == nil {
		t.Fatal("expected validation error for unknown link type")
	}
}

func TestLLMExtractor_RejectsLinkToUnknownSlug(t *testing.T) {
	fake := &fakeLLMClient{response: `{
		"entities":[{"slug":"a","title":"A","type":"concept","short_description":"d"}],
		"concepts":[],
		"links":[{"from_slug":"a","to_slug":"ghost","type":"mentions"}]
	}`}
	ex := NewLLMExtractor(fake, "model")
	if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"}); err == nil {
		t.Fatal("expected validation error for unknown to_slug")
	}
}

func TestLLMExtractor_AcceptsLinkToExistingSlug(t *testing.T) {
	fake := &fakeLLMClient{response: `{
		"entities":[{"slug":"a","title":"A","type":"concept","short_description":"d"}],
		"concepts":[],
		"links":[{"from_slug":"a","to_slug":"existing-page","type":"mentions"}]
	}`}
	ex := NewLLMExtractor(fake, "model")
	_, err := ex.Extract(context.Background(), ExtractionRequest{
		SourceID:      "src",
		Body:          "b",
		ExistingSlugs: []string{"existing-page"},
	})
	if err != nil {
		t.Fatalf("Extract should accept link to existing slug: %v", err)
	}
}

func TestLLMExtractor_RejectsSelfLoopLink(t *testing.T) {
	fake := &fakeLLMClient{response: `{
		"entities":[{"slug":"a","title":"A","type":"concept","short_description":"d"}],
		"concepts":[],
		"links":[{"from_slug":"a","to_slug":"a","type":"mentions"}]
	}`}
	ex := NewLLMExtractor(fake, "model")
	if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"}); err == nil {
		t.Fatal("expected validation error for self-loop link")
	}
}

func TestLLMExtractor_RejectsDuplicateSlugAfterCanonicalize(t *testing.T) {
	// Two entities with the SAME title canonicalize to the same slug
	// after canonicalizeSlugs runs. The downstream seenLocal[] check
	// must still reject this — duplicate pages would clobber each other.
	fake := &fakeLLMClient{response: `{
		"entities":[
			{"slug":"a","title":"Same Title","type":"concept","short_description":"d"},
			{"slug":"b","title":"Same Title","type":"concept","short_description":"d"}
		],
		"concepts":[],
		"links":[]
	}`}
	ex := NewLLMExtractor(fake, "model")
	if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"}); err == nil {
		t.Fatal("expected validation error for post-canonicalize duplicate slug")
	}
}

func TestLLMExtractor_RejectsEntityWithoutTitle(t *testing.T) {
	fake := &fakeLLMClient{response: `{
		"entities":[{"slug":"empty","title":"","type":"concept","short_description":"d"}],
		"concepts":[],
		"links":[]
	}`}
	ex := NewLLMExtractor(fake, "model")
	if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"}); err == nil {
		t.Fatal("expected validation error for empty title")
	}
}

func TestLLMExtractor_RejectsEmptyBody(t *testing.T) {
	fake := &fakeLLMClient{response: `{}`}
	ex := NewLLMExtractor(fake, "model")
	if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "   "}); err == nil {
		t.Fatal("expected error for whitespace-only body")
	}
}

func TestLLMExtractor_PromptIncludesExistingSlugs(t *testing.T) {
	fake := &fakeLLMClient{response: `{"entities":[],"concepts":[],"links":[]}`}
	ex := NewLLMExtractor(fake, "deepseek-nano")
	_, err := ex.Extract(context.Background(), ExtractionRequest{
		SourceID:      "src_xyz",
		Body:          "body",
		ExistingSlugs: []string{"agent-md", "soul-md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := fake.lastReq.Messages[0].Content
	if !strings.Contains(prompt, "agent-md") || !strings.Contains(prompt, "soul-md") {
		t.Fatalf("prompt missing existing slugs: %q", prompt)
	}
	if !strings.Contains(prompt, "src_xyz") {
		t.Fatalf("prompt missing source ID")
	}
}

func TestLLMExtractor_RequestIsDeterministicTemperatureZero(t *testing.T) {
	fake := &fakeLLMClient{response: `{"entities":[],"concepts":[],"links":[]}`}
	ex := NewLLMExtractor(fake, "model")
	_, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastReq.Temperature == nil || *fake.lastReq.Temperature != 0.0 {
		t.Fatalf("Temperature should be 0.0 (deterministic), got %v", fake.lastReq.Temperature)
	}
	if fake.lastReq.Model != "model" {
		t.Fatalf("Model = %q, want %q", fake.lastReq.Model, "model")
	}
}

func TestLLMExtractor_SurfacesNoJSONError(t *testing.T) {
	fake := &fakeLLMClient{response: "I cannot help with this request."}
	ex := NewLLMExtractor(fake, "model")
	if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "body"}); err == nil {
		t.Fatal("expected error when LLM returns no JSON envelope")
	}
}

func TestPromptVersion_MatchesWikiRegex(t *testing.T) {
	// The wiki PromptVersion regex (internal/wiki/schema.go:14) requires
	// v{n} or ingest_v{n} etc. The extractor's PromptVersion must match
	// so wiki pages stamped with it pass Validate.
	pv := PromptVersion()
	if !strings.HasPrefix(pv, "ingest_v") {
		t.Fatalf("PromptVersion %q must start with ingest_v", pv)
	}
}

func TestLLMExtractor_AcceptsAllNewLinkTypes(t *testing.T) {
	newTypes := []string{"cites", "applies", "implements", "references", "depends_on", "semantically_similar_to"}
	for _, lt := range newTypes {
		t.Run(lt, func(t *testing.T) {
			fake := &fakeLLMClient{response: `{
				"entities":[
					{"slug":"a","title":"A","type":"concept","short_description":"d"},
					{"slug":"b","title":"B","type":"concept","short_description":"d"}
				],
				"concepts":[],
				"links":[{"from_slug":"a","to_slug":"b","type":"` + lt + `"}]
			}`}
			ex := NewLLMExtractor(fake, "model")
			delta, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"})
			if err != nil {
				t.Fatalf("Extract should accept link type %q: %v", lt, err)
			}
			if delta.Links[0].Type != lt {
				t.Fatalf("link type = %q, want %q", delta.Links[0].Type, lt)
			}
		})
	}
}

func TestBuildExtractionPrompt_ContainsNewLinkTypes(t *testing.T) {
	prompt := buildExtractionPrompt(ExtractionRequest{SourceID: "src", Body: "body"})
	for _, lt := range []string{"cites", "applies", "implements", "references", "depends_on", "semantically_similar_to"} {
		if !strings.Contains(prompt, lt) {
			t.Errorf("prompt missing link type %q", lt)
		}
	}
	// Prompt should contain type guidance examples.
	if !strings.Contains(prompt, "Type guidance:") {
		t.Error("prompt missing Type guidance line")
	}
}

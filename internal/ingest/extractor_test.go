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
	for i := 0; i < extractorMaxItems+1; i++ {
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

func TestLLMExtractor_RejectsBadSlug(t *testing.T) {
	cases := map[string]string{
		"uppercase":       `{"slug":"Alice","title":"A","type":"person","short_description":"d"}`,
		"spaces":          `{"slug":"foo bar","title":"A","type":"person","short_description":"d"}`,
		"double-hyphen":   `{"slug":"foo--bar","title":"A","type":"person","short_description":"d"}`,
		"trailing-hyphen": `{"slug":"foo-","title":"A","type":"person","short_description":"d"}`,
	}
	for name, ent := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakeLLMClient{response: `{"entities":[` + ent + `],"concepts":[],"links":[]}`}
			ex := NewLLMExtractor(fake, "model")
			if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"}); err == nil {
				t.Fatalf("expected validation error for %s slug", name)
			}
		})
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

func TestLLMExtractor_RejectsDuplicateSlug(t *testing.T) {
	fake := &fakeLLMClient{response: `{
		"entities":[
			{"slug":"shared","title":"E","type":"concept","short_description":"d"},
			{"slug":"shared","title":"E2","type":"concept","short_description":"d"}
		],
		"concepts":[],
		"links":[]
	}`}
	ex := NewLLMExtractor(fake, "model")
	if _, err := ex.Extract(context.Background(), ExtractionRequest{SourceID: "src", Body: "b"}); err == nil {
		t.Fatal("expected validation error for duplicate slug")
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

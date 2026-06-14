package onboarding

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// fakeClient streams a fixed JSON body as one Text chunk.
type fakeClient struct{ body string }

func (f fakeClient) Stream(_ context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 1)
	ch <- llm.Chunk{Text: f.body, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func TestLLMAnswerExtractor_Work(t *testing.T) {
	ex := NewLLMAnswerExtractor(fakeClient{body: `{"expertise":["backend"],"stack":["Go","Neo4j"]}`}, "m")
	got, err := ex.Extract(context.Background(), StepWork, "sono un dev backend, uso Go e Neo4j")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Stack) != 2 || got.Stack[0] != "Go" {
		t.Errorf("stack = %v", got.Stack)
	}
	if len(got.Expertise) != 1 || got.Expertise[0] != "backend" {
		t.Errorf("expertise = %v", got.Expertise)
	}
}

func TestLLMAnswerExtractor_BadJSONFallsBack(t *testing.T) {
	ex := NewLLMAnswerExtractor(fakeClient{body: "not json"}, "m")
	got, err := ex.Extract(context.Background(), StepProjects, "lavoro su Aura")
	if err != nil {
		t.Fatalf("fallback should not error: %v", err)
	}
	if len(got.Projects) != 1 || got.Projects[0] != "lavoro su Aura" {
		t.Errorf("fallback projects = %v", got.Projects)
	}
}

func TestExtractSystemPrompt_CorrectsAndFullDefault(t *testing.T) {
	// Every prompt must instruct the model to correct misspellings.
	for _, step := range []Step{StepIdentity, StepWork, StepProjects, StepSocial, StepDraft, Step("unknown")} {
		p := extractSystemPrompt(step)
		if !strings.Contains(p, "Correct obvious misspellings") {
			t.Errorf("step %q prompt missing misspelling correction directive:\n%s", step, p)
		}
	}

	// The default (non-scoped) path at StepDraft must expose all fields so an
	// edit correction can update any fact.
	dflt := extractSystemPrompt(StepDraft)
	for _, field := range []string{"expertise", "stack", "projects", "people"} {
		if !strings.Contains(dflt, field) {
			t.Errorf("default/StepDraft prompt missing field %q:\n%s", field, dflt)
		}
	}

	// Scoped steps must still expose only their own fields.
	work := extractSystemPrompt(StepWork)
	if !strings.Contains(work, "expertise") || !strings.Contains(work, "stack") {
		t.Errorf("StepWork prompt missing scoped fields:\n%s", work)
	}
	if strings.Contains(work, "projects") || strings.Contains(work, "interests") {
		t.Errorf("StepWork prompt leaks non-work fields:\n%s", work)
	}
}

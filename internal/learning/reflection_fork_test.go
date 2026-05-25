package learning_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aura/aura/internal/agent/agentdef"
	"github.com/aura/aura/internal/learning"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/memoryindex"
)

type fakeReflectionLLM struct {
	responses []string
	requests  []llm.Request
	calls     int
}

func (f *fakeReflectionLLM) Send(_ context.Context, req llm.Request) (llm.Response, error) {
	f.calls++
	f.requests = append(f.requests, req)
	if f.calls <= len(f.responses) {
		return llm.Response{Content: f.responses[f.calls-1]}, nil
	}
	return llm.Response{Content: `{}`}, nil
}

func (f *fakeReflectionLLM) Stream(context.Context, llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token)
	close(ch)
	return ch, nil
}

type fakeReflectorRunner struct {
	response string
	calls    int

	archetype string
	prompt    string
	model     string
	chain     []string
}

func (f *fakeReflectorRunner) Run(_ context.Context, archetype, prompt, modelOverride string, parentChain []string) (string, error) {
	f.calls++
	f.archetype = archetype
	f.prompt = prompt
	f.model = modelOverride
	f.chain = append([]string(nil), parentChain...)
	return f.response, nil
}

func TestReflectionHook_HonorsMaxPerSession(t *testing.T) {
	store := openTestMemStore(t)
	llmc := &fakeReflectionLLM{responses: []string{
		`{"observations":["turn 1"],"patterns":[],"user_preferences":[],"user_reflections":[]}`,
		`{"observations":["turn 2"],"patterns":[],"user_preferences":[],"user_reflections":[]}`,
		`{"observations":["turn 3"],"patterns":[],"user_preferences":[],"user_reflections":[]}`,
		`{"observations":["turn 4"],"patterns":[],"user_preferences":[],"user_reflections":[]}`,
		`{"observations":["turn 5"],"patterns":[],"user_preferences":[],"user_reflections":[]}`,
		`{"observations":["turn 6"],"patterns":[],"user_preferences":[],"user_reflections":[]}`,
	}}
	hook := &learning.ReflectionHook{Client: llmc, MaxPerSession: 5, MinTurnTokens: 1}
	turn := reflectionTestTurn()

	for i := 0; i < 6; i++ {
		errs := hook.Apply(context.Background(), turn, store)
		if len(errs) != 0 {
			t.Fatalf("Apply %d errors: %v", i+1, errs)
		}
	}
	if llmc.calls != 5 {
		t.Fatalf("LLM calls = %d, want 5", llmc.calls)
	}
	docs := searchReflectionDocs(t, store, "turn")
	if len(docs) != 5 {
		t.Fatalf("reflection docs = %d, want 5", len(docs))
	}
}

func TestReflectionHook_SkipsTrivialTurns(t *testing.T) {
	store := openTestMemStore(t)
	llmc := &fakeReflectionLLM{responses: []string{`{"observations":["should not run"]}`}}
	hook := &learning.ReflectionHook{Client: llmc, MaxPerSession: 5, MinTurnTokens: 200}

	errs := hook.Apply(context.Background(), learning.ReflectionTurn{
		RunID:       "run-trivial",
		ThreadID:    "thread-trivial",
		UserMessage: "ok",
	}, store)
	if len(errs) != 0 {
		t.Fatalf("Apply errors: %v", errs)
	}
	if llmc.calls != 0 {
		t.Fatalf("LLM calls = %d, want 0", llmc.calls)
	}
}

func TestReflectionHook_PersistsValidJSON(t *testing.T) {
	store := openTestMemStore(t)
	llmc := &fakeReflectionLLM{responses: []string{`{
		"observations":["User asked for atomic commits"],
		"patterns":["Prefers bounded slices"],
		"user_preferences":["Keep plans concrete"],
		"user_reflections":["Working on Aura OH1"]
	}`}}
	hook := &learning.ReflectionHook{Client: llmc, MaxPerSession: 5, MinTurnTokens: 1}

	errs := hook.Apply(context.Background(), reflectionTestTurn(), store)
	if len(errs) != 0 {
		t.Fatalf("Apply errors: %v", errs)
	}
	docs := searchReflectionDocs(t, store, "atomic")
	if len(docs) != 1 {
		t.Fatalf("reflection docs = %d, want 1", len(docs))
	}
	body := docs[0].Body
	for _, want := range []string{"User asked for atomic commits", "Prefers bounded slices", "Keep plans concrete", "Working on Aura OH1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("reflection body = %q, missing %q", body, want)
		}
	}
}

func TestReflectionHook_DirectPromptMatchesReflectorArchetype(t *testing.T) {
	store := openTestMemStore(t)
	llmc := &fakeReflectionLLM{responses: []string{`{}`}}
	hook := &learning.ReflectionHook{Client: llmc, MaxPerSession: 5, MinTurnTokens: 1}

	errs := hook.Apply(context.Background(), reflectionTestTurn(), store)
	if len(errs) != 0 {
		t.Fatalf("Apply errors: %v", errs)
	}
	if llmc.calls != 1 || len(llmc.requests) != 1 {
		t.Fatalf("LLM calls = %d requests = %d, want 1", llmc.calls, len(llmc.requests))
	}
	messages := llmc.requests[0].Messages
	if len(messages) < 1 {
		t.Fatal("LLM request missing system message")
	}
	if want := agentdef.MustBuiltinPrompt(learning.ReflectorArchetype); messages[0].Content != want {
		t.Fatalf("system prompt changed: got %d bytes, want %d", len(messages[0].Content), len(want))
	}
}

func TestReflectionHook_ReflectorArchetypeProducesIdenticalJSON(t *testing.T) {
	response := `{
		"observations":["User asked for atomic commits"],
		"patterns":["Prefers bounded slices"],
		"user_preferences":["Keep plans concrete"],
		"user_reflections":["Working on Aura OH1"]
	}`
	directStore, directErrs := applyReflectionResponse(t, response)
	if len(directErrs) != 0 {
		t.Fatalf("direct Apply errors: %v", directErrs)
	}

	runnerStore := openTestMemStore(t)
	llmc := &fakeReflectionLLM{responses: []string{`{"observations":["should not call"]}`}}
	runner := &fakeReflectorRunner{response: response}
	hook := &learning.ReflectionHook{Client: llmc, DelegateRunner: runner, MaxPerSession: 5, MinTurnTokens: 1}
	errs := hook.Apply(context.Background(), reflectionTestTurn(), runnerStore)
	if len(errs) != 0 {
		t.Fatalf("runner Apply errors: %v", errs)
	}
	if llmc.calls != 0 {
		t.Fatalf("direct LLM calls with delegate runner = %d, want 0", llmc.calls)
	}
	if runner.calls != 1 || runner.archetype != learning.ReflectorArchetype {
		t.Fatalf("runner call = %d archetype = %q, want reflector", runner.calls, runner.archetype)
	}
	if runner.model != "" {
		t.Fatalf("runner model override = %q, want empty", runner.model)
	}
	if want := []string{learning.ReflectorArchetype}; !reflect.DeepEqual(runner.chain, want) {
		t.Fatalf("runner chain = %+v, want %+v", runner.chain, want)
	}
	if !strings.Contains(runner.prompt, "user_message:") || !strings.Contains(runner.prompt, "tool_calls:") {
		t.Fatalf("runner prompt missing turn fields: %q", runner.prompt)
	}

	directDocs := searchReflectionDocs(t, directStore, "atomic")
	runnerDocs := searchReflectionDocs(t, runnerStore, "atomic")
	if len(directDocs) != 1 || len(runnerDocs) != 1 {
		t.Fatalf("docs direct=%d runner=%d, want 1 each", len(directDocs), len(runnerDocs))
	}
	if runnerDocs[0].Body != directDocs[0].Body {
		t.Fatalf("runner body = %q, direct body = %q", runnerDocs[0].Body, directDocs[0].Body)
	}
}

func TestReflectionHook_RejectsMalformedJSON(t *testing.T) {
	store, errs := applyReflectionResponse(t, `not-json`)
	if len(errs) != 1 {
		t.Fatalf("Apply errors = %v, want one parse error", errs)
	}
	if docs := searchReflectionDocs(t, store, "not-json"); len(docs) != 0 {
		t.Fatalf("malformed extraction wrote %d docs", len(docs))
	}
}

func TestReflectionHook_EmptyExtractionAccepted(t *testing.T) {
	store, errs := applyReflectionResponse(t, `{}`)
	if len(errs) != 0 {
		t.Fatalf("Apply errors: %v", errs)
	}
	if docs := searchReflectionDocs(t, store, "reflection"); len(docs) != 0 {
		t.Fatalf("empty extraction wrote %d docs", len(docs))
	}
}

func applyReflectionResponse(t *testing.T, response string) (*memoryindex.Store, []error) {
	t.Helper()
	store := openTestMemStore(t)
	llmc := &fakeReflectionLLM{responses: []string{response}}
	hook := &learning.ReflectionHook{Client: llmc, MaxPerSession: 5, MinTurnTokens: 1}
	return store, hook.Apply(context.Background(), reflectionTestTurn(), store)
}

func reflectionTestTurn() learning.ReflectionTurn {
	return learning.ReflectionTurn{
		RunID:         "run-reflection",
		ThreadID:      "thread-reflection",
		UserMessage:   strings.Repeat("Please keep the implementation slice atomic and verified. ", 8),
		AssistantText: strings.Repeat("I will use focused tests and commit one slice. ", 8),
		ToolCalls: []learning.ReflectionToolCall{{
			Name:          "search",
			Success:       true,
			ResultPreview: "found graph plan references",
		}},
	}
}

func searchReflectionDocs(t *testing.T, store *memoryindex.Store, query string) []memoryindex.Document {
	t.Helper()
	docs, err := store.Search(context.Background(), query, memoryindex.Filter{
		Kinds: []string{learning.KindReflection},
		Limit: 20,
	})
	if err != nil {
		t.Fatalf("Search reflection docs: %v", err)
	}
	return docs
}

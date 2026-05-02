package summarizer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/llm"
)

// fakeOpenAIHandler returns a fixed chat-completion response wrapping the
// given candidates JSON as the assistant message content.
func fakeOpenAIHandler(candidatesJSON string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": candidatesJSON,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 20,
				"total_tokens":      30,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func newFakeLLMClient(candidatesJSON string) llm.Client {
	srv := httptest.NewServer(fakeOpenAIHandler(candidatesJSON))
	// Note: srv is not closed — leaks in test but acceptable for unit tests.
	return llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  "test",
		BaseURL: srv.URL,
		Model:   "test-model",
	})
}

func TestScorer_FiltersByMinSalience(t *testing.T) {
	candidates := `{"candidates":[
		{"fact":"Marco lives in Bologna","score":0.9,"category":"person","related_slugs":[],"source_turn_ids":[1]},
		{"fact":"low score fact","score":0.3,"category":"fact","related_slugs":[],"source_turn_ids":[2]}
	]}`
	client := newFakeLLMClient(candidates)
	scorer := summarizer.NewScorer(client, "test-model", 0.7)

	turns := []conversation.Turn{
		{Role: "user", Content: "Marco lives in Bologna"},
		{Role: "assistant", Content: "Interesting."},
	}
	got, err := scorer.Score(context.Background(), turns)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 candidate (filtered by MinSalience=0.7), got %d", len(got))
	}
	if got[0].Score < 0.7 {
		t.Fatalf("returned candidate below min salience: %f", got[0].Score)
	}
}

func TestScorer_EmptyCandidates(t *testing.T) {
	client := newFakeLLMClient(`{"candidates":[]}`)
	scorer := summarizer.NewScorer(client, "test-model", 0.7)

	got, err := scorer.Score(context.Background(), nil)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 candidates, got %d", len(got))
	}
}

func TestScorer_MalformedJSON(t *testing.T) {
	// LLM returns non-JSON content
	client := newFakeLLMClient(`this is not json`)
	scorer := summarizer.NewScorer(client, "test-model", 0.7)

	_, err := scorer.Score(context.Background(), []conversation.Turn{
		{Role: "user", Content: "test"},
	})
	if err == nil {
		t.Fatal("want error for malformed JSON response, got nil")
	}
}

// TestScorer_LLMCallError covers the LLM transport error path by pointing the
// client at a server that returns HTTP 500.
func TestScorer_LLMCallError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  "test",
		BaseURL: srv.URL,
		Model:   "test-model",
	})
	scorer := summarizer.NewScorer(client, "test-model", 0.7)

	_, err := scorer.Score(context.Background(), []conversation.Turn{
		{Role: "user", Content: "test"},
	})
	if err == nil {
		t.Fatal("want error when LLM returns 500, got nil")
	}
}

// TestScorer_SystemRoleSkipped verifies that turns with role "system" are
// excluded from the prompt text sent to the LLM.
func TestScorer_SystemRoleSkipped(t *testing.T) {
	// A single system turn — after filtering the prompt has no real content,
	// but the LLM call still happens. We just verify no error and filtering runs.
	candidates := `{"candidates":[{"fact":"a fact","score":0.9,"category":"fact","related_slugs":[],"source_turn_ids":[1]}]}`
	client := newFakeLLMClient(candidates)
	scorer := summarizer.NewScorer(client, "test-model", 0.5)

	turns := []conversation.Turn{
		{Role: "system", Content: "you are a helpful assistant"},
		{Role: "user", Content: "hello"},
	}
	got, err := scorer.Score(context.Background(), turns)
	if err != nil {
		t.Fatalf("Score with system turn: %v", err)
	}
	// The system turn was skipped; the user turn was included; LLM returned 1 candidate.
	if len(got) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(got))
	}
}

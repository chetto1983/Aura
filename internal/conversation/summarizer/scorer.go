package summarizer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
)

// Scorer extracts salience-scored candidate facts from a slice of turns.
type Scorer interface {
	Score(ctx context.Context, turns []conversation.Turn) ([]Candidate, error)
}

// LLMScorer is the default Scorer: posts turns to an LLM at temperature=0
// and parses the structured JSON response.
type LLMScorer struct {
	client      llm.Client
	model       string
	minSalience float64
}

// NewScorer returns a Scorer backed by the given LLM client.
// minSalience is the minimum score (0-1) a candidate must have to be returned.
func NewScorer(client llm.Client, model string, minSalience float64) *LLMScorer {
	return &LLMScorer{
		client:      client,
		model:       model,
		minSalience: minSalience,
	}
}

const scorerSystemPrompt = `You are a knowledge extraction assistant. Analyze the conversation turns and identify factual claims worth storing in a personal knowledge base.

Return ONLY valid JSON in this exact schema (no markdown, no prose):
{
  "candidates": [
    {
      "fact": "<concise factual claim>",
      "score": <0.0-1.0 salience score>,
      "category": "<person|project|preference|fact|todo>",
      "related_slugs": ["<existing-wiki-slug>"],
      "source_turn_ids": [<turn_id_integers>]
    }
  ]
}

Score 1.0 = highly specific, durable, personally relevant fact. Score 0.0 = generic or ephemeral.
Extract at most 5 candidates. If nothing noteworthy, return {"candidates":[]}.`

type scorerResponse struct {
	Candidates []Candidate `json:"candidates"`
}

// Score calls the LLM with the turns and returns candidates above MinSalience.
func (s *LLMScorer) Score(ctx context.Context, turns []conversation.Turn) ([]Candidate, error) {
	if len(turns) == 0 {
		return nil, nil
	}

	var sb strings.Builder
	sb.WriteString("Conversation turns:\n\n")
	for _, t := range turns {
		if t.Role == "system" {
			continue
		}
		fmt.Fprintf(&sb, "[%s] %s\n", t.Role, t.Content)
	}

	temp := 0.0
	resp, err := s.client.Send(ctx, llm.Request{
		Model: s.model,
		Messages: []llm.Message{
			{Role: "system", Content: scorerSystemPrompt},
			{Role: "user", Content: sb.String()},
		},
		Temperature: &temp,
	})
	if err != nil {
		return nil, fmt.Errorf("scorer llm call: %w", err)
	}

	var parsed scorerResponse
	if err := json.Unmarshal([]byte(resp.Content), &parsed); err != nil {
		return nil, fmt.Errorf("scorer parse response: %w (content: %q)", err, resp.Content)
	}

	out := make([]Candidate, 0, len(parsed.Candidates))
	for _, c := range parsed.Candidates {
		if c.Score >= s.minSalience {
			out = append(out, c)
		}
	}
	return out, nil
}

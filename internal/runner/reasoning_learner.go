package runner

import (
	"context"
	"strings"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoninglearn"
)

// reasoningOracle labels a turn with a reasoning tier by running the router
// prompt against the configured LLM client — the teacher for the async
// self-improvement learner. It never touches a user turn's hot path.
type reasoningOracle struct {
	client llm.Client
	model  string
}

func (o *reasoningOracle) Label(ctx context.Context, text string) (prompt.ReasoningTier, bool) {
	enabled := false
	req := llm.Request{
		Model:       o.model,
		Messages:    []llm.Message{{Role: llm.RoleSystem, Content: prompt.ReasoningRouterSystemPrompt}, {Role: llm.RoleUser, Content: text}},
		Temperature: 0,
		MaxTokens:   32,
		Reasoning:   llm.ReasoningConfig{Enabled: &enabled},
		ToolChoice:  "none",
	}
	ch, err := o.client.Stream(ctx, req)
	if err != nil {
		return "", false
	}
	var b strings.Builder
	for c := range ch {
		if c.Err != nil {
			return "", false
		}
		b.WriteString(c.Text)
	}
	tier := prompt.ParseReasoningRouterTier(strings.TrimSpace(b.String()))
	return tier, tier.Valid()
}

// buildReasoningLearner wires the async self-improvement worker when learning is
// enabled and a save-capable store + a classifier are present; nil otherwise.
// It attaches the learner to the (shared) classifier so uncertain turns are
// observed.
func buildReasoningLearner(d Deps, classifier *prompt.ReasoningClassifier) *reasoninglearn.Learner {
	if classifier == nil || !d.LLM.ReasoningLearning || d.ReasoningSaver == nil {
		return nil
	}
	l := reasoninglearn.New(reasoninglearn.Config{
		Oracle:  &reasoningOracle{client: d.Client, model: d.LLM.Model},
		Saver:   d.ReasoningSaver,
		Refresh: classifier.Refresh,
	})
	classifier.SetLearner(l)
	return l
}

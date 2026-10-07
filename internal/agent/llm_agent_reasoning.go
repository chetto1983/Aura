package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

// tierClassifier is the seed bank a turn is read against (prompt.ReasoningClassifier).
type tierClassifier interface {
	Classify(ctx context.Context, text string) (prompt.ReasoningVerdict, bool)
}

// resolveClassifier prefers the shared injected classifier (production, anchors built
// once); it falls back to a per-agent one built from Embedder when only that is supplied
// (tests/standalone). nil when neither is wired, as an interface: a nil
// *prompt.ReasoningClassifier stored in one would read as a classifier.
func resolveClassifier(cfg LlmAgentConfig) tierClassifier {
	if cfg.Classifier != nil {
		return cfg.Classifier
	}
	if classifier := prompt.NewReasoningClassifier(cfg.Embedder); classifier != nil {
		return classifier
	}
	return nil
}

// adaptiveReasoningTier classifies the CONVERSATION, so it gates on the generalized
// IsReasoningTarget — the same predicate ApplyAdaptiveEffort and ApplyFixedReasoning
// use. It was OpenRouter-only, upstream of the identical restriction in
// ApplyAdaptiveEffort: on every other backend the tier was never even COMPUTED, so
// lifting the downstream gate alone changed nothing. Measured live on Ollama 0.33.2 with
// gemma4:31b-cloud, 2026-08-31 — with only the downstream gate widened, a turn produced
// no tier decision at all.
func (a *LlmAgent) adaptiveReasoningTier(ctx context.Context) (prompt.ReasoningTier, bool) {
	if !a.cfg.AdaptiveReasoning || !prompt.IsReasoningTarget(a.cfg.Provider, a.cfg.BaseURL) {
		return "", false
	}
	user := prompt.LastGenuineUserContent(a.history)
	if strings.TrimSpace(user) == "" {
		return prompt.ReasoningTierLow, true
	}

	// Fast path: the local embedding classifier (embedding sidecar, ~10ms) replaces
	// the per-turn LLM router round-trip. On any embed failure it returns false;
	// when a classifier is wired, degrade to static low reasoning instead of
	// spending a second network call every turn.
	if a.classifier != nil {
		if prompt.IsTrivialGreeting(user) {
			return prompt.ReasoningTierNone, true
		}
		if verdict, ok := a.classifier.Classify(ctx, user); ok {
			reasoningtrace.Record("adaptive_reasoning_classifier_decision", map[string]any{
				"thread_id": a.sessionID,
				"tier":      verdict.Tier,
				"margin":    verdict.Margin,
				"source":    "embedding",
			})
			return verdict.Tier, true
		}
		reasoningtrace.Record("adaptive_reasoning_classifier_miss", map[string]any{
			"thread_id": a.sessionID,
			"fallback":  "static_low",
		})
		return prompt.ReasoningTierLow, true
	}
	if tier, outcome := a.askTeacher(ctx, user); outcome == teacherSuccess {
		return tier, true
	}
	return prompt.ReasoningTierLow, true
}

// teacherOutcome is how one synchronous teacher attempt ended.
type teacherOutcome string

const (
	teacherSuccess  teacherOutcome = "success"
	teacherTimeout  teacherOutcome = "timeout"
	teacherInvalid  teacherOutcome = "invalid"
	teacherError    teacherOutcome = "error"
	teacherCanceled teacherOutcome = "canceled"
)

// askTeacher asks the router prompt once, synchronously, for the tier of user's request: the
// teacher of the turn-recall spec, on the turn's own client and route, bounded by
// reasoningRouterTimeout. Every attempt is counted with its outcome.
func (a *LlmAgent) askTeacher(ctx context.Context, user string) (tier prompt.ReasoningTier, outcome teacherOutcome) {
	defer func() { recordTeacherAttempt(string(outcome)) }()
	routeCtx, cancel := context.WithTimeout(ctx, a.reasoningRouterTimeout())
	defer cancel()
	routeCtx, llmEnd := llmCallBoundary.Start(routeCtx)
	var boundaryErr error
	defer llmEnd.PanicSafe(&boundaryErr)
	enabled := false
	req := llm.Request{
		Model:       a.cfg.Model,
		Messages:    []llm.Message{{Role: llm.RoleSystem, Content: prompt.ReasoningRouterSystemPrompt}, {Role: llm.RoleUser, Content: user}},
		Temperature: 0,
		MaxTokens:   32,
		Reasoning:   llm.ReasoningConfig{Enabled: &enabled},
		SessionID:   a.sessionID,
		ToolChoice:  "none",
	}
	reasoningtrace.Record("adaptive_reasoning_router_request", map[string]any{
		"thread_id":  a.sessionID,
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"reasoning":  req.Reasoning,
		"user":       user,
	})

	ch, err := a.streamWithOpenRetry(routeCtx, req, "adaptive_reasoning_router")
	if err != nil {
		boundaryErr = err
		recordLLMError(llmErrorKind("reasoning_router_open", err))
		reasoningtrace.Record("adaptive_reasoning_router_error", map[string]any{"error": err.Error()})
		return "", teacherFailure(routeCtx, err)
	}
	var b strings.Builder
	for c := range ch {
		if c.Err != nil {
			boundaryErr = c.Err
			recordLLMError(llmErrorKind("reasoning_router_stream", c.Err))
			reasoningtrace.Record("adaptive_reasoning_router_error", map[string]any{"error": c.Err.Error()})
			return "", teacherFailure(routeCtx, c.Err)
		}
		if c.Usage != nil {
			recordUsage(*c.Usage)
		}
		b.WriteString(c.Text)
	}
	raw := strings.TrimSpace(b.String())
	tier = prompt.ParseReasoningRouterTier(raw)
	if !tier.Valid() {
		reasoningtrace.Record("adaptive_reasoning_router_invalid", map[string]any{"raw": raw})
		return "", teacherInvalid
	}
	reasoningtrace.Record("adaptive_reasoning_router_decision", map[string]any{"raw": raw, "tier": tier})
	return tier, teacherSuccess
}

// teacherFailure names why the teacher gave no answer: its own deadline, the turn's
// cancellation, or anything else.
func teacherFailure(ctx context.Context, err error) teacherOutcome {
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return teacherTimeout
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return teacherCanceled
	}
	return teacherError
}

func (a *LlmAgent) reasoningRouterTimeout() time.Duration {
	const maxReasoningRouterTimeout = 2 * time.Second
	total := time.Duration(a.cfg.TotalTimeoutSec) * time.Second
	if total <= 0 {
		return maxReasoningRouterTimeout
	}
	if total < maxReasoningRouterTimeout {
		return total
	}
	return maxReasoningRouterTimeout
}

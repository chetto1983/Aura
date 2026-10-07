package agent

import (
	"context"
	"errors"
	"strings"

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

// The outcomes AskTeacher returns and counts.
const (
	TeacherSuccess  = "success"
	TeacherTimeout  = "timeout"
	TeacherInvalid  = "invalid"
	TeacherError    = "error"
	TeacherCanceled = "canceled"
)

// AskTeacher asks the router prompt once for the tier of user's request: the teacher of the
// turn-recall spec. The runner asks it in the background, on the turn's own client and
// model, once the turn's decision is written, so its answer labels the turn for later ones
// and never decides this one. ctx carries the bound. Like conversations.GenerateTitle it
// drains the stream. Every attempt is counted with its outcome.
func AskTeacher(ctx context.Context, client llm.Client, model, user string) (tier prompt.ReasoningTier, outcome string) {
	defer func() { recordTeacherAttempt(outcome) }()
	ctx, llmEnd := llmCallBoundary.Start(ctx)
	var boundaryErr error
	defer llmEnd.PanicSafe(&boundaryErr)
	enabled := false
	req := llm.Request{
		Model:       model,
		Messages:    []llm.Message{{Role: llm.RoleSystem, Content: prompt.ReasoningRouterSystemPrompt}, {Role: llm.RoleUser, Content: user}},
		Temperature: 0,
		MaxTokens:   32,
		Reasoning:   llm.ReasoningConfig{Enabled: &enabled},
		ToolChoice:  "none",
	}
	reasoningtrace.Record("adaptive_reasoning_router_request", map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"reasoning":  req.Reasoning,
		"user":       user,
	})

	fail := func(kind string, err error) (prompt.ReasoningTier, string) {
		boundaryErr = err
		recordLLMError(llmErrorKind(kind, err))
		reasoningtrace.Record("adaptive_reasoning_router_error", map[string]any{"error": err.Error()})
		return "", teacherFailure(ctx, err)
	}
	if client == nil {
		return fail("reasoning_router_open", errors.New("nil client"))
	}
	recordLLMStreamOpen()
	ch, err := client.Stream(ctx, req)
	if err != nil {
		return fail("reasoning_router_open", err)
	}
	var b strings.Builder
	for c := range ch {
		if c.Err != nil {
			return fail("reasoning_router_stream", c.Err)
		}
		if c.Usage != nil {
			recordUsage(*c.Usage)
		}
		b.WriteString(c.Text)
	}
	// A provider can close the stream at the deadline with no error and no text (measured
	// on the lab VM, 2026-10-07: attempts ending at 2001 ms read as invalid answers).
	if err := ctx.Err(); err != nil {
		return fail("reasoning_router_stream", err)
	}
	raw := strings.TrimSpace(b.String())
	tier = prompt.ParseReasoningRouterTier(raw)
	if !tier.Valid() {
		reasoningtrace.Record("adaptive_reasoning_router_invalid", map[string]any{"raw": raw})
		return "", TeacherInvalid
	}
	reasoningtrace.Record("adaptive_reasoning_router_decision", map[string]any{"raw": raw, "tier": tier})
	return tier, TeacherSuccess
}

// teacherFailure names why the teacher gave no answer: the caller's deadline, its
// cancellation, or anything else.
func teacherFailure(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return TeacherTimeout
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return TeacherCanceled
	}
	return TeacherError
}

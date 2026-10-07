package runner

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/redact"
)

// TurnDecisionStore persists how a user turn's effort was decided (migration 0137).
// *conversations.Store satisfies it.
type TurnDecisionStore interface {
	RecordTurnDecision(ctx context.Context, conversationID string, seq int, d conversations.TurnDecision) error
}

// TurnRecallStore reads an identity's past turns; the composition root binds the tenant
// memory clients behind it.
type TurnRecallStore interface {
	RecallTurns(ctx context.Context, request arcadedb.TurnRecallRequest) (arcadedb.TurnRecall, error)
}

// turnContext is what the dispatched message's reading needs: the typed text, and from the
// loaded history its context key and whether anything conversational precedes it.
type turnContext struct {
	text       string
	key        string
	standalone bool
}

// readTurnContext keys the dispatched message by what the model reads before it: the
// always-block (governing instructions), every prior message, and the context blocks the
// message was composed with. The per-turn memory block is left out because it is rebuilt
// from memory on every turn, and a leading persisted system turn because the agent sends
// its own. A message with attachments, or whose model text does not end with what was
// typed, gets no key: its input is not versioned, so it is never a reusable label.
func readTurnContext(history []llm.Message, input turnInput, cfg conversations.ContextConfig, attachments []string) turnContext {
	if input.visibleUserMsg == nil {
		return turnContext{}
	}
	visible := *input.visibleUserMsg
	turn := turnContext{text: visible}
	if len(attachments) > 0 {
		return turn
	}
	current := -1
	for index, message := range slices.Backward(history) {
		if message.Role == llm.RoleUser && message.Content == visible {
			current = index
			break
		}
	}
	if current < 0 {
		return turn
	}
	blocks := ""
	if input.modelUserMsg != nil {
		prefix, ok := strings.CutSuffix(*input.modelUserMsg, visible)
		if !ok {
			return turn
		}
		blocks = prefix
	}
	transient := ""
	if cfg.TransientContext != nil {
		transient = cfg.TransientContext.Content
	}
	prior := make([]llm.Message, 0, current)
	standalone := true
	for index, message := range history[:current] {
		switch {
		case index == 0 && message.Role == llm.RoleSystem:
		case message.Role == llm.RoleUser && transient != "" && message.Content == transient:
		case message.Role == llm.RoleUser && cfg.AlwaysBlock != "" && message.Content == cfg.AlwaysBlock:
			prior = append(prior, message)
		default:
			standalone = false
			prior = append(prior, message)
		}
	}
	turn.key, turn.standalone = agent.TurnContextKey(prior, blocks), standalone
	return turn
}

// turnReading binds the dispatched user turn to the identity's memory and to the tracker
// its decision is written from. A run without a dispatched user row (resume, branch re-run)
// gets the zero reading: it reads no memory and writes no decision.
func (r *Runner) turnReading(ctx context.Context, tr *turnTracker, turn turnContext) agent.TurnReading {
	if tr.userTurnSeq <= 0 {
		return agent.TurnReading{}
	}
	reading := agent.TurnReading{
		Text:       turn.text,
		ContextKey: turn.key,
		SourceRef:  reasoningSourceRef(tr.convID, tr.userTurnSeq),
		Standalone: turn.standalone,
		OnDecision: func(d agent.TurnDecision) { tr.decision = &d },
	}
	if identityID := identityctx.IdentityID(ctx); r.turnRecall != nil && identityID != "" {
		reading.Recaller = identityTurnRecaller{store: r.turnRecall, identityID: identityID}
	}
	return reading
}

// identityTurnRecaller is the agent's recall port bound to one identity.
type identityTurnRecaller struct {
	store      TurnRecallStore
	identityID string
}

func (r identityTurnRecaller) RecallTurns(ctx context.Context, request agent.TurnRecallRequest) (agent.TurnRecall, error) {
	recall, err := r.store.RecallTurns(ctx, arcadedb.TurnRecallRequest{
		IdentityID: r.identityID, Text: request.Text, ContextKey: request.ContextKey,
		RouteKey: request.RouteKey, PolicyVersion: request.PolicyVersion, SourceRef: request.SourceRef,
		DeferredTools: request.DeferredTools, IncludeLabels: request.IncludeLabels,
	})
	if err != nil {
		return agent.TurnRecall{}, err
	}
	return agent.TurnRecall{
		UserLabels:    agentRecalledTurns(recall.UserLabels),
		TeacherLabels: agentRecalledTurns(recall.TeacherLabels),
		ToolTurns:     agentRecalledTurns(recall.ToolTurns),
	}, nil
}

func agentRecalledTurns(turns []arcadedb.RecalledTurn) []agent.RecalledTurn {
	out := make([]agent.RecalledTurn, len(turns))
	for index, turn := range turns {
		out[index] = agent.RecalledTurn(turn)
	}
	return out
}

// recordTurnDecision writes the turn's decision onto its user row once, when the round
// reaches a durable stop (its answer or its pause). The answer is already committed and the
// decision is provenance, so a failed write is a warning, never a failed turn.
func (r *Runner) recordTurnDecision(ctx context.Context, tr *turnTracker) {
	if r.turnDecisions == nil || tr.userTurnSeq <= 0 || tr.decisionRecorded {
		return
	}
	tr.decisionRecorded = true
	decision := conversations.TurnDecision{ContextKey: tr.contextKey}
	if d := tr.decision; d != nil {
		decision.Effort, decision.EffortRequested = string(d.Effort), string(d.EffortRequested)
		decision.EffortSource, decision.RouteKey = d.EffortSource, d.RouteKey
		decision.PolicyVersion, decision.OriginRef = d.PolicyVersion, d.OriginRef
	}
	if decision == (conversations.TurnDecision{}) {
		return
	}
	if err := r.turnDecisions.RecordTurnDecision(ctx, tr.convID, tr.userTurnSeq, decision); err != nil {
		slog.Warn("turn decision write failed; the turn is unaffected",
			"conv", redact.Line(tr.convID), "seq", tr.userTurnSeq, "err", redact.Line(err.Error()))
	}
}

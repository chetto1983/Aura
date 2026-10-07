package runner

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/redact"
)

// TurnDecisionStore persists how a user turn's effort was decided (migration 0137), and the
// background teacher's label that may later replace a guess. *conversations.Store satisfies it.
type TurnDecisionStore interface {
	RecordTurnDecision(ctx context.Context, conversationID string, seq int, d conversations.TurnDecision) error
	RecordTeacherLabel(ctx context.Context, conversationID string, seq int, requested string) error
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
// decision is provenance, so a failed write is a warning, never a failed turn. A written
// decision that asks the teacher then starts its worker.
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
		return
	}
	if tr.decision != nil && tr.decision.AskTeacher {
		r.maybeTeachTurn(ctx, tr)
	}
}

// maybeTeachTurn asks the teacher to label a turn whose decision asked for it, without the
// turn waiting (spec 2026-10-06, "Amendment 2026-10-07"). It follows maybeAutoTitle: the
// turn's own client and route, a bounded context the finished turn cannot cancel, and the
// WaitGroup Stop joins. It starts only after the decision is written, so the label always
// lands after it.
func (r *Runner) maybeTeachTurn(turnCtx context.Context, tr *turnTracker) {
	if (r.breaker != nil && r.breaker.Allow() != nil) || tr.userText == "" {
		return
	}
	runtime := r.trackerLLMSnapshot(tr)
	convID, seq, text := tr.convID, tr.userTurnSeq, tr.userText
	r.wg.Go(func() {
		ctx := context.WithoutCancel(turnCtx) // load-bearing: turnCtx cancels on Turn return
		ctx, cancel := context.WithTimeout(ctx, r.teacherTimeout)
		defer cancel()
		started := time.Now()
		tier, outcome := agent.AskTeacher(ctx, runtime.Client, runtime.Config.Model, text)
		elapsed := time.Since(started)
		// The label stores the tier's effort before the clamp, as migration 0137 defines
		// reasoning_effort_requested; a reuse clamps it again. effort is what that reuse
		// would send on this route, logged only.
		var requested, effort llm.ReasoningEffort
		if outcome == agent.TeacherSuccess {
			requested = tier.Effort()
			effort = runtime.Config.ClampReasoningEffort(requested)
			r.persistTeacherLabel(turnCtx, convID, seq, requested)
		}
		// The ref is logged without its scheme, as on the turn-read line: the production
		// handler blanks every postgres:// string as a DSN.
		slog.Info("adaptive reasoning: teacher label",
			"thread_id", redact.Line(convID), "source_ref", agent.LoggableSourceRef(reasoningSourceRef(convID, seq)),
			"outcome", outcome, "tier", string(tier), "requested", string(requested),
			"effort", string(effort), "teacher_ms", elapsed.Milliseconds())
	})
}

func (r *Runner) persistTeacherLabel(turnCtx context.Context, convID string, seq int, requested llm.ReasoningEffort) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(turnCtx), backgroundWriteTimeout)
	defer cancel()
	if err := r.turnDecisions.RecordTeacherLabel(ctx, convID, seq, string(requested)); err != nil {
		slog.Warn("adaptive reasoning: teacher label write failed",
			"conv", redact.Line(convID), "seq", seq, "err", redact.Line(err.Error()))
	}
}

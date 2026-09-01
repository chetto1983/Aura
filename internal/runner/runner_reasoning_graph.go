package runner

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/redact"
	"github.com/google/uuid"
)

const (
	reasoningGraphSummaryRunes = 4096
	reasoningGraphWriteTimeout = 5 * time.Second
)

// ReasoningGraphSink is the narrow identity-scoped graph persistence boundary.
type ReasoningGraphSink interface {
	UpsertReasoningTrace(context.Context, arcadedb.ReasoningTrace) error
}

// ReasoningTraceBuilder accumulates one authorized provider-visible attempt.
type ReasoningTraceBuilder struct {
	runID     uuid.UUID
	summary   strings.Builder
	runes     int
	createdAt time.Time
}

// Reset discards every event from a repudiated provider attempt.
func (b *ReasoningTraceBuilder) Reset() {
	b.runID = uuid.Nil
	b.summary.Reset()
	b.runes = 0
	b.createdAt = time.Time{}
}

// ObserveReasoning accepts only the provider-visible reasoning event shape that
// the runner's authorization gate has already approved.
func (b *ReasoningTraceBuilder) ObserveReasoning(ev *agent.Event) {
	if ev == nil || ev.RequestID == uuid.Nil || ev.LLMResponse == nil || ev.LLMResponse.Reasoning == "" {
		return
	}
	if b.runID != uuid.Nil && b.runID != ev.RequestID {
		return
	}
	if b.runID == uuid.Nil {
		b.runID = ev.RequestID
		b.createdAt = ev.Timestamp.UTC()
		if b.createdAt.IsZero() {
			b.createdAt = time.Now().UTC()
		}
	}
	b.appendReasoning(ev.LLMResponse.Reasoning)
}

// ObserveToolInvocation joins one structured runtime tool event to the active trace.
// The RED stub deliberately records nothing until the metadata policy lands in GREEN.
func (*ReasoningTraceBuilder) ObserveToolInvocation(*agent.Event) {}

func (b *ReasoningTraceBuilder) appendReasoning(delta string) {
	remaining := reasoningGraphSummaryRunes - b.runes
	if remaining <= 0 {
		return
	}
	count := utf8.RuneCountInString(delta)
	if count <= remaining {
		b.summary.WriteString(delta)
		b.runes += count
		return
	}
	b.summary.WriteString(headRunes(delta, remaining))
	b.runes += remaining
}

// CommitSourceTurn finalizes one successful trace against an already-committed
// authoritative assistant turn.
func (b *ReasoningTraceBuilder) CommitSourceTurn(
	identityID, conversationID string,
	turnSeq int,
	terminalAt time.Time,
) (arcadedb.ReasoningTrace, bool) {
	summary := strings.TrimSpace(b.summary.String())
	if b.runID == uuid.Nil || summary == "" || strings.TrimSpace(identityID) == "" ||
		strings.TrimSpace(conversationID) == "" || turnSeq <= 0 {
		return arcadedb.ReasoningTrace{}, false
	}
	if terminalAt.IsZero() {
		terminalAt = time.Now().UTC()
	}
	trace := arcadedb.ReasoningTrace{
		IdentityID: identityID, TraceID: b.runID.String(),
		SourceRef:      reasoningSourceRef(conversationID, turnSeq),
		ConversationID: conversationID, TurnSeq: turnSeq,
		ProviderSummary: summary, Status: arcadedb.ReasoningStatusSucceeded,
		CreatedAt: b.createdAt, TerminalAt: terminalAt.UTC(),
		Steps: []arcadedb.ReasoningStep{{
			Index: 1, ProviderSummary: summary, CreatedAt: b.createdAt,
		}},
	}
	b.Reset()
	return trace, true
}

func reasoningSourceRef(conversationID string, turnSeq int) string {
	return "postgres://aura/conversations/" + conversationID + "/turns/" + strconv.Itoa(turnSeq)
}

func (r *Runner) observeReasoningGraph(tr *turnTracker, ev *agent.Event) {
	if r == nil || tr == nil || r.reasoningGraphSink == nil {
		return
	}
	tr.reasoningGraph.ObserveReasoning(ev)
}

func (r *Runner) observeReasoningTool(tr *turnTracker, ev *agent.Event) {
	if r == nil || tr == nil || r.reasoningGraphSink == nil {
		return
	}
	tr.reasoningGraph.ObserveToolInvocation(ev)
}

func (r *Runner) commitSourceTurn(ctx context.Context, tr *turnTracker, ev *agent.Event) {
	if r == nil || tr == nil || ev == nil || r.reasoningGraphSink == nil {
		return
	}
	identityID := identityctx.IdentityID(ctx)
	if identityID == "" {
		return
	}
	turnSeq, err := r.Conv.CountTurns(ctx, tr.convID)
	if err != nil {
		slog.Warn("reasoning graph source lookup failed after answer commit", "err", redact.Line(err.Error()))
		return
	}
	trace, ok := tr.reasoningGraph.CommitSourceTurn(identityID, tr.convID, turnSeq, ev.Timestamp)
	if !ok {
		return
	}
	graphCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reasoningGraphWriteTimeout)
	defer cancel()
	if err := r.reasoningGraphSink.UpsertReasoningTrace(graphCtx, trace); err != nil {
		slog.Warn("reasoning graph delivery failed after answer commit", "err", redact.Line(err.Error()))
	}
}

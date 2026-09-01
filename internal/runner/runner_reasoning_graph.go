package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/redact"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
)

const (
	reasoningGraphSummaryRunes     = 4096
	reasoningGraphObservationRunes = 1024
	reasoningGraphReferenceRunes   = 256
	reasoningGraphMaxSteps         = 64
	reasoningGraphMaxToolsPerStep  = 32
	reasoningGraphMaxReferences    = 32
	reasoningGraphWriteTimeout     = 5 * time.Second
)

// ReasoningGraphSink is the narrow identity-scoped graph persistence boundary.
type ReasoningGraphSink interface {
	UpsertReasoningTrace(context.Context, arcadedb.ReasoningTrace) error
}

// ReasoningDeletionStore removes derived traces before their authoritative source disappears.
type ReasoningDeletionStore interface {
	DeleteReasoningBySource(context.Context, arcadedb.ReasoningDeleteSelector) (int, error)
}

// ReasoningTraceBuilder accumulates one authorized provider-visible attempt.
type ReasoningTraceBuilder struct {
	runID     uuid.UUID
	summary   strings.Builder
	runes     int
	createdAt time.Time
	steps     []reasoningStepBuilder
	afterTool bool
	seenCalls map[string]struct{}
}

type reasoningStepBuilder struct {
	summary   strings.Builder
	createdAt time.Time
	tools     []arcadedb.ReasoningToolCall
}

type reasoningToolPolicy struct {
	observation     bool
	artifact        bool
	entityArgFields []string
}

var reasoningToolPolicies = map[string]reasoningToolPolicy{
	"memory_batch":       {},
	"memory_forget":      {entityArgFields: []string{"entity"}},
	"memory_recall":      {},
	"memory_upsert_fact": {entityArgFields: []string{"subject", "object"}},
	"send_file":          {observation: true, artifact: true},
	"task":               {},
}

// Reset discards every event from a repudiated provider attempt.
func (b *ReasoningTraceBuilder) Reset() {
	b.runID = uuid.Nil
	b.summary.Reset()
	b.runes = 0
	b.createdAt = time.Time{}
	b.steps = nil
	b.afterTool = false
	b.seenCalls = nil
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
	kept := b.appendReasoning(ev.LLMResponse.Reasoning)
	if kept == "" {
		return
	}
	if len(b.steps) == 0 || b.afterTool {
		if len(b.steps) == reasoningGraphMaxSteps {
			return
		}
		createdAt := ev.Timestamp.UTC()
		if createdAt.IsZero() {
			createdAt = b.createdAt
		}
		b.steps = append(b.steps, reasoningStepBuilder{createdAt: createdAt})
		b.afterTool = false
	}
	step := &b.steps[len(b.steps)-1]
	step.summary.WriteString(kept)
}

// ObserveToolInvocation joins one structured runtime tool event to the active trace.
func (b *ReasoningTraceBuilder) ObserveToolInvocation(ev *agent.Event) {
	if ev == nil || ev.RequestID == uuid.Nil || b.runID == uuid.Nil || ev.RequestID != b.runID ||
		ev.Actions.ToolInvocation == nil || ev.Actions.ToolInvocation.Event != agent.ToolInvocationEnd ||
		len(b.steps) == 0 {
		return
	}
	ti := ev.Actions.ToolInvocation
	policy, allowed := reasoningToolPolicies[ti.ToolName]
	status, ok := reasoningToolStatus(ti.Status)
	if !allowed || !ok || strings.TrimSpace(ti.ToolCallID) == "" {
		return
	}
	if b.seenCalls == nil {
		b.seenCalls = make(map[string]struct{})
	}
	if _, duplicate := b.seenCalls[ti.ToolCallID]; duplicate {
		return
	}
	step := &b.steps[len(b.steps)-1]
	if len(step.tools) == reasoningGraphMaxToolsPerStep {
		return
	}
	tool := arcadedb.ReasoningToolCall{
		CallID: strings.TrimSpace(ti.ToolCallID), ToolName: ti.ToolName,
		Status: status, DurationMillis: max(ti.DurationMS, 0),
		ArgumentDigest: reasoningArgumentDigest(ti.Arguments),
	}
	if policy.observation {
		tool.Observation = reasoningObservation(ti.ResultPreview)
	}
	if policy.artifact && status == "succeeded" {
		tool.ArtifactRefs = reasoningArtifactRefs(ti.Meta)
	}
	if status == "succeeded" {
		tool.EntityRefs = reasoningEntityRefs(ti.Arguments, policy.entityArgFields)
	}
	step.tools = append(step.tools, tool)
	b.seenCalls[ti.ToolCallID] = struct{}{}
	b.afterTool = true
}

func (b *ReasoningTraceBuilder) appendReasoning(delta string) string {
	remaining := reasoningGraphSummaryRunes - b.runes
	if remaining <= 0 {
		return ""
	}
	count := utf8.RuneCountInString(delta)
	if count <= remaining {
		b.summary.WriteString(delta)
		b.runes += count
		return delta
	}
	kept := headRunes(delta, remaining)
	b.summary.WriteString(kept)
	b.runes += remaining
	return kept
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
	steps := make([]arcadedb.ReasoningStep, 0, len(b.steps))
	sourceRef := reasoningSourceRef(conversationID, turnSeq)
	for _, pending := range b.steps {
		stepSummary := strings.TrimSpace(pending.summary.String())
		if stepSummary == "" {
			continue
		}
		tools := append([]arcadedb.ReasoningToolCall(nil), pending.tools...)
		for index := range tools {
			tools[index].SourceRef = sourceRef
		}
		steps = append(steps, arcadedb.ReasoningStep{
			Index: len(steps) + 1, ProviderSummary: stepSummary,
			CreatedAt: pending.createdAt, ToolCalls: tools,
		})
	}
	if len(steps) == 0 {
		return arcadedb.ReasoningTrace{}, false
	}
	trace := arcadedb.ReasoningTrace{
		IdentityID: identityID, TraceID: b.runID.String(),
		SourceRef:      sourceRef,
		ConversationID: conversationID, TurnSeq: turnSeq,
		ProviderSummary: summary, Status: arcadedb.ReasoningStatusSucceeded,
		CreatedAt: b.createdAt, TerminalAt: terminalAt.UTC(),
		Steps: steps,
	}
	b.Reset()
	return trace, true
}

func reasoningToolStatus(status string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "success", "succeeded":
		return "succeeded", true
	case "error", "failed":
		return "failed", true
	case "canceled", "cancelled":
		return "cancelled", true
	default:
		return "", false
	}
}

func reasoningArgumentDigest(arguments string) string {
	if strings.TrimSpace(arguments) == "" {
		return ""
	}
	safe := toolinvocations.RedactForLedger(arguments, toolinvocations.ArgsRawCapBytes)
	digest := sha256.Sum256([]byte(safe))
	return hex.EncodeToString(digest[:])
}

func reasoningObservation(observation string) string {
	trimmed := strings.TrimSpace(observation)
	lowered := strings.ToLower(trimmed)
	if trimmed == "" || strings.HasPrefix(lowered, "data:") || strings.Contains(lowered, ";base64,") {
		return ""
	}
	safe := strings.TrimSpace(toolinvocations.RedactForLedger(
		trimmed, toolinvocations.ResultPreviewCapBytes))
	return headRunes(safe, reasoningGraphObservationRunes)
}

func reasoningArtifactRefs(meta map[string]any) []string {
	descriptor, ok := meta["artifact"].(map[string]any)
	if !ok {
		return nil
	}
	assetID, _ := descriptor["asset_id"].(string)
	assetID = strings.TrimSpace(assetID)
	if !reasoningReferenceSegment(assetID) {
		return nil
	}
	return []string{"artifact://assets/" + assetID}
}

func reasoningEntityRefs(arguments string, fields []string) []string {
	if len(fields) == 0 || strings.TrimSpace(arguments) == "" {
		return nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		value, _ := args[field].(string)
		value = strings.TrimSpace(value)
		if value == "" || utf8.RuneCountInString(value) > reasoningGraphReferenceRunes ||
			strings.ContainsAny(value, "\r\n\x00") {
			continue
		}
		seen[value] = struct{}{}
	}
	refs := make([]string, 0, min(len(seen), reasoningGraphMaxReferences))
	for ref := range seen {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs[:min(len(refs), reasoningGraphMaxReferences)]
}

func reasoningReferenceSegment(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > reasoningGraphReferenceRunes {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
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

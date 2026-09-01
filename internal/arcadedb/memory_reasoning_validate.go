package arcadedb

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/chetto1983/aura/internal/redact"
)

func normalizeReasoningTrace(trace ReasoningTrace) (ReasoningTrace, error) {
	if err := validateReasoningIdentity(trace); err != nil {
		return ReasoningTrace{}, err
	}
	summary, err := normalizeReasoningEvidence("provider_summary", trace.ProviderSummary, reasoningSummaryRunes)
	if err != nil {
		return ReasoningTrace{}, err
	}
	trace.ProviderSummary = summary
	if len(trace.Steps) > reasoningMaxSteps {
		return ReasoningTrace{}, fmt.Errorf("arcadedb: reasoning steps exceeds %d items", reasoningMaxSteps)
	}
	steps := make([]ReasoningStep, len(trace.Steps))
	callIDs := make(map[string]struct{})
	for stepOffset, step := range trace.Steps {
		if step.Index != stepOffset+1 {
			return ReasoningTrace{}, fmt.Errorf("arcadedb: reasoning step index %d is not contiguous", step.Index)
		}
		if step.CreatedAt.IsZero() {
			return ReasoningTrace{}, fmt.Errorf("arcadedb: reasoning step %d created_at must be set", step.Index)
		}
		step.ProviderSummary, err = normalizeReasoningEvidence(
			"step provider_summary", step.ProviderSummary, reasoningSummaryRunes)
		if err != nil {
			return ReasoningTrace{}, err
		}
		if len(step.ToolCalls) > reasoningMaxToolsPerStep {
			return ReasoningTrace{}, fmt.Errorf("arcadedb: reasoning step %d tools exceeds %d items",
				step.Index, reasoningMaxToolsPerStep)
		}
		tools := make([]ReasoningToolCall, len(step.ToolCalls))
		for toolIndex, tool := range step.ToolCalls {
			if err := validateReasoningTool(trace.SourceRef, tool); err != nil {
				return ReasoningTrace{}, err
			}
			if _, duplicate := callIDs[tool.CallID]; duplicate {
				return ReasoningTrace{}, fmt.Errorf("arcadedb: duplicate reasoning call_id %q", tool.CallID)
			}
			callIDs[tool.CallID] = struct{}{}
			tool.Observation = redact.String(strings.TrimSpace(tool.Observation))
			tool.ArtifactRefs = append([]string(nil), tool.ArtifactRefs...)
			tool.EntityRefs = append([]string(nil), tool.EntityRefs...)
			tools[toolIndex] = tool
		}
		step.ToolCalls = tools
		steps[stepOffset] = step
	}
	trace.Steps = steps
	return trace, nil
}

func validateReasoningIdentity(trace ReasoningTrace) error {
	for name, value := range map[string]string{
		"identity_id": trace.IdentityID, "trace_id": trace.TraceID,
		"conversation_id": trace.ConversationID,
	} {
		if err := validateReasoningText(name, value, reasoningReferenceRunes); err != nil {
			return err
		}
	}
	if err := validateReasoningSourceRef(trace.SourceRef); err != nil {
		return err
	}
	if trace.TurnSeq <= 0 {
		return fmt.Errorf("arcadedb: reasoning turn_seq must be positive")
	}
	if trace.CreatedAt.IsZero() {
		return fmt.Errorf("arcadedb: reasoning created_at must be set")
	}
	switch trace.Status {
	case ReasoningStatusSucceeded, ReasoningStatusFailed, ReasoningStatusCancelled:
	default:
		return fmt.Errorf("arcadedb: unsupported reasoning status %q", trace.Status)
	}
	return nil
}

func validateReasoningTool(traceSource string, tool ReasoningToolCall) error {
	for name, value := range map[string]string{
		"call_id": tool.CallID, "tool_name": tool.ToolName, "tool status": tool.Status,
	} {
		if err := validateReasoningText(name, value, reasoningReferenceRunes); err != nil {
			return err
		}
	}
	if tool.Status != "succeeded" && tool.Status != "failed" && tool.Status != "cancelled" {
		return fmt.Errorf("arcadedb: unsupported reasoning tool status %q", tool.Status)
	}
	if tool.DurationMillis < 0 {
		return fmt.Errorf("arcadedb: reasoning tool duration_ms must be non-negative")
	}
	if tool.ArgumentDigest != "" {
		if len(tool.ArgumentDigest) != reasoningDigestRunes {
			return fmt.Errorf("arcadedb: reasoning argument_digest must be a SHA-256 hex digest")
		}
		decoded, err := hex.DecodeString(tool.ArgumentDigest)
		if err != nil || len(decoded) != reasoningDigestRunes/2 || tool.ArgumentDigest != strings.ToLower(tool.ArgumentDigest) {
			return fmt.Errorf("arcadedb: reasoning argument_digest must be lowercase SHA-256 hex")
		}
	}
	if err := validateReasoningEvidence("observation", tool.Observation, reasoningObservationRunes); err != nil {
		return err
	}
	if tool.SourceRef != traceSource {
		return fmt.Errorf("arcadedb: reasoning tool source_ref must match the trace source")
	}
	if len(tool.ArtifactRefs) > reasoningMaxReferences || len(tool.EntityRefs) > reasoningMaxReferences {
		return fmt.Errorf("arcadedb: reasoning tool references exceeds %d items", reasoningMaxReferences)
	}
	for _, ref := range tool.ArtifactRefs {
		if !strings.HasPrefix(ref, "artifact://") || strings.Contains(ref, "..") {
			return fmt.Errorf("arcadedb: reasoning artifact_ref is not an allowlisted artifact URI")
		}
		if err := validateReasoningText("artifact_ref", ref, reasoningReferenceRunes); err != nil {
			return err
		}
	}
	for _, ref := range tool.EntityRefs {
		if err := validateReasoningText("entity_ref", ref, defaultMemoryLimits.EntityRunes); err != nil {
			return err
		}
	}
	return nil
}

func normalizeReasoningEvidence(name, value string, limit int) (string, error) {
	if err := validateReasoningEvidence(name, value, limit); err != nil {
		return "", err
	}
	return redact.String(strings.TrimSpace(value)), nil
}

func validateReasoningEvidence(name, value string, limit int) error {
	if err := validateReasoningText(name, value, limit); err != nil {
		return err
	}
	lowered := strings.ToLower(value)
	if strings.Contains(lowered, ";base64,") || strings.HasPrefix(strings.TrimSpace(lowered), "data:") {
		return fmt.Errorf("arcadedb: reasoning %s contains an embedded blob", name)
	}
	return nil
}

func validateReasoningSourceRef(ref string) error {
	if !strings.HasPrefix(ref, "postgres://aura/conversations/") || strings.Contains(ref, "..") {
		return fmt.Errorf("arcadedb: reasoning source_ref must name an authoritative conversation turn")
	}
	return validateReasoningText("source_ref", ref, reasoningReferenceRunes)
}

func validateReasoningText(name, value string, limit int) error {
	if value == "" || value != strings.TrimSpace(value) {
		return fmt.Errorf("arcadedb: reasoning %s must be non-empty and canonical", name)
	}
	if err := validateRuneLimit("reasoning "+name, value, limit); err != nil {
		return err
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("arcadedb: reasoning %s contains a control character", name)
		}
	}
	return nil
}

func reasoningTraceParams(trace ReasoningTrace) map[string]any {
	return map[string]any{
		"identity_id": trace.IdentityID, "trace_id": trace.TraceID, "source_ref": trace.SourceRef,
		"conversation_id": trace.ConversationID, "turn_seq": trace.TurnSeq,
		"provider_summary": trace.ProviderSummary, "status": string(trace.Status),
		"created_at":  trace.CreatedAt.UTC().Format(time.RFC3339Nano),
		"terminal_at": nullableTime(trace.TerminalAt), "expires_at": nullableTime(trace.ExpiresAt),
	}
}

func reasoningStepParams(trace ReasoningTrace, step ReasoningStep) map[string]any {
	return map[string]any{
		"identity_id": trace.IdentityID, "trace_id": trace.TraceID, "step_index": step.Index,
		"provider_summary": step.ProviderSummary, "created_at": step.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func reasoningToolParams(trace ReasoningTrace, step ReasoningStep, tool ReasoningToolCall) map[string]any {
	return map[string]any{
		"identity_id": trace.IdentityID, "trace_id": trace.TraceID, "step_index": step.Index,
		"call_id": tool.CallID, "tool_name": tool.ToolName, "status": tool.Status,
		"duration_ms": tool.DurationMillis, "argument_digest": tool.ArgumentDigest,
		"observation": tool.Observation, "artifact_refs": tool.ArtifactRefs,
		"entity_refs": tool.EntityRefs, "source_ref": tool.SourceRef,
	}
}

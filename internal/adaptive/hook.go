package adaptive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	agenttools "github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

var decisionHookNamespace = uuid.MustParse("8e908827-c641-4af7-980f-119033f7f145")

// EventWriter appends an adaptive observation to the authoritative ledger.
type EventWriter interface {
	Record(context.Context, Event) (int64, error)
}

// DecisionHook captures the real pre-execution control points used by LlmAgent.
// It stores only selected actions and hashes; prompt/tool arguments never enter
// the adaptive ledger.
type DecisionHook struct {
	writer EventWriter
	gate   interface {
		Gate(context.Context, uuid.UUID) (PolicyGate, error)
	}
}

// NewDecisionHook creates an ungated observation hook for tests and offline tools.
func NewDecisionHook(writer EventWriter) *DecisionHook {
	return &DecisionHook{writer: writer}
}

// NewGatedDecisionHook creates an observation hook controlled by the distributed policy.
func NewGatedDecisionHook(writer EventWriter, gate *PolicyController) *DecisionHook {
	return &DecisionHook{writer: writer, gate: gate}
}

// OnTurnStart implements agent.Hook.
func (h *DecisionHook) OnTurnStart(context.Context, agent.HookTurn) error { return nil }

// BeforeModel records the served reasoning configuration without prompt content.
func (h *DecisionHook) BeforeModel(ctx context.Context, req *llm.Request) (*agent.ModelHookResult, error) {
	if req == nil {
		return nil, errors.New("adaptive decision hook received nil model request")
	}
	catalogNames := make([]string, 0, len(req.Tools))
	for _, definition := range req.Tools {
		catalogNames = append(catalogNames, definition.Function.Name)
	}
	catalogHash := sha256.Sum256([]byte(strings.Join(catalogNames, "\x00")))
	catalogHashHex := hex.EncodeToString(catalogHash[:])
	fingerprint := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\x00%s\x00%d\x00%s\x00%d\x00%d\x00%s",
		req.Model, req.Reasoning.Effort, req.Reasoning.MaxTokens,
		req.ToolChoice, len(req.Messages), len(req.Tools), catalogHashHex,
	)))
	err := h.recordDecision(ctx, "reasoning", "reasoning", "model:"+hex.EncodeToString(fingerprint[:]), map[string]any{
		"domain":              "reasoning",
		"action":              req.Reasoning.Effort,
		"max_reasoning":       req.Reasoning.MaxTokens,
		"tool_choice":         req.ToolChoice,
		"model":               req.Model,
		"message_count":       len(req.Messages),
		"tool_catalog_size":   len(req.Tools),
		"tool_catalog_sha256": catalogHashHex,
	})
	return nil, err
}

// BeforeTool records the invoked top-level tool without argument content.
func (h *DecisionHook) BeforeTool(ctx context.Context, call llm.ToolCall) (*agent.ToolHookResult, error) {
	argsHash := sha256.Sum256([]byte(call.Function.Arguments))
	domain := adaptiveToolDomain(call.Function.Name)
	err := h.recordDecision(ctx, domain, "tool:"+call.ID, "tool:"+call.ID+":"+call.Function.Name, map[string]any{
		"domain":           domain,
		"action":           call.Function.Name,
		"tool_call_id":     call.ID,
		"arguments_bytes":  len(call.Function.Arguments),
		"arguments_sha256": hex.EncodeToString(argsHash[:]),
	})
	return nil, err
}

// AfterTool implements agent.Hook.
func (h *DecisionHook) AfterTool(context.Context, llm.ToolCall, agenttools.ToolResult) (*agent.ToolResultHookResult, error) {
	return nil, nil
}

// OnTurnEnd implements agent.Hook.
func (h *DecisionHook) OnTurnEnd(context.Context, agent.HookTurn) error { return nil }

func (h *DecisionHook) recordDecision(ctx context.Context, domain, decisionPoint, eventPoint string, fields map[string]any) error {
	if h == nil || h.writer == nil {
		return errors.New("adaptive decision hook requires an event writer")
	}
	ownerID, err := uuid.Parse(identityctx.IdentityID(ctx))
	if err != nil {
		return fmt.Errorf("adaptive decision owner: %w", err)
	}
	requestID, err := uuid.Parse(agenttools.RequestIDFromContext(ctx))
	if err != nil {
		return fmt.Errorf("adaptive decision request: %w", err)
	}
	decisionID := DecisionIDForPoint(requestID, decisionPoint)
	if h.gate != nil {
		gate, err := h.gate.Gate(ctx, decisionID)
		if err != nil {
			return fmt.Errorf("read adaptive decision gate: %w", err)
		}
		if !gate.Observe {
			return nil
		}
	}
	payload := make(map[string]any, len(fields)+2)
	payload["schema_version"] = "1.0"
	payload["evidence_type"] = "decision"
	for key, value := range fields {
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal adaptive %s decision: %w", domain, err)
	}
	event, err := NewEvent(EventParams{
		ID:          uuid.NewSHA1(decisionHookNamespace, []byte(requestID.String()+":"+eventPoint)),
		OwnerID:     ownerID,
		AggregateID: requestID.String(),
		DecisionID:  decisionID,
		Kind:        EventDecision,
		Payload:     raw,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	if _, err := h.writer.Record(ctx, event); err != nil {
		return fmt.Errorf("record adaptive %s decision: %w", domain, err)
	}
	return nil
}

func adaptiveToolDomain(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "document_search", "knowledge_search", "memory_search", "memory_get", "graph_search":
		return "knowledge"
	case "skill", "skill_search", "skill_install", "skill_run":
		return "skill"
	default:
		return "tool"
	}
}

var _ agent.Hook = (*DecisionHook)(nil)

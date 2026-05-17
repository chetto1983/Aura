package chat

// hub_lifecycle.go holds the persistence + event-translation helpers for the
// Hub. These functions translate between the Hub's in-memory Run/OutboundEvent
// shape and the runstore.* schema, drive the durable AppendEvent / question
// flows, and assemble the lifecycle payloads consumed by dashboards and
// replay. Pure helpers (no Hub-state mutation) live here too.
//
// Split from hub.go to keep that file under the 600-LOC god-class threshold.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	runstore "github.com/aura/aura/internal/storage/runs"
)

func (h *Hub) persistLifecycleEvent(ctx context.Context, run *Run, ev OutboundEvent) error {
	persistCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	event, err := h.lifecycle.AppendEvent(persistCtx, lifecycleEventParams(run, ev))
	if err != nil {
		return fmt.Errorf("chat: append lifecycle event: %w", err)
	}
	if ev.Type == EventQuestionRequested {
		if err := h.recordQuestionRequested(persistCtx, run, ev, event); err != nil {
			return err
		}
	}
	return nil
}

func isDurableRunEvent(eventType EventType) bool {
	switch eventType {
	case EventRunStarted, EventToolStart, EventToolEnd, EventQuestionRequested, EventQuestionAnswered, EventMessageDone, EventUsage, EventDone, EventError, EventCancelled:
		return true
	default:
		return false
	}
}

func lifecycleRunMetadata(msg InboundMessage) map[string]any {
	metadata := map[string]any{
		"delivery_mode": string(msg.Mode),
	}
	if msg.ID != "" {
		metadata["inbound_message_id"] = msg.ID
	}
	if msg.ParentRunID != "" {
		metadata["parent_run_id"] = msg.ParentRunID
	}
	if msg.Locale != "" {
		metadata["locale"] = msg.Locale
	}
	if msg.TimeZone != "" {
		metadata["time_zone"] = msg.TimeZone
	}
	return metadata
}

func lifecycleEventParams(run *Run, ev OutboundEvent) runstore.AppendEventParams {
	params := runstore.AppendEventParams{
		ID:             ev.ID,
		RunID:          run.ID,
		Type:           string(ev.Type),
		ActorID:        run.ActorID,
		IdempotencyKey: ev.ID,
		Payload:        lifecyclePayload(run, ev),
		RedactionLevel: runstore.RedactionMetadata,
		CreatedAt:      ev.CreatedAt,
	}
	switch ev.Type {
	case EventRunStarted:
		params.RunStatus = string(RunStatusRunning)
	case EventCancelled:
		params.RunStatus = string(RunStatusCancelled)
		params.CancelledAt = &ev.CreatedAt
		params.CompletedAt = firstTime(run.CompletedAt, &ev.CreatedAt)
		params.LastError = "context_canceled"
	case EventError:
		params.RunStatus = string(RunStatusFailed)
		params.CompletedAt = firstTime(run.CompletedAt, &ev.CreatedAt)
		params.LastError = "agent_loop_error"
	case EventToolStart, EventToolEnd:
	case EventQuestionRequested:
		params.RunStatus = string(RunStatusWaitingForUser)
	case EventQuestionAnswered:
	case EventUsage:
		params.Stats = durableStats(ev.Payload)
	case EventMessageDone:
		params.FinalTextPreview = textPreview(ev.Content)
	case EventDone:
		params.RunStatus = string(run.Status)
		if run.Status != RunStatusWaitingForUser {
			params.CompletedAt = firstTime(run.CompletedAt, &ev.CreatedAt)
		}
		if run.Status == RunStatusCancelled {
			params.CancelledAt = firstTime(run.CompletedAt, &ev.CreatedAt)
			params.LastError = "context_canceled"
		}
		if run.Status == RunStatusFailed {
			params.LastError = "agent_loop_error"
		}
		params.FinalTextPreview = finalTextPreview(run)
	}
	return params
}

func lifecyclePayload(run *Run, ev OutboundEvent) map[string]any {
	payload := map[string]any{
		"event_type": string(ev.Type),
		"status":     string(run.Status),
		"channel":    string(run.Channel),
	}
	if run.ThreadID != "" {
		payload["thread_id"] = run.ThreadID
	}
	if run.PrincipalID != "" {
		payload["principal_id"] = run.PrincipalID
	}
	if run.ActorID != "" {
		payload["actor_id"] = run.ActorID
	}
	if parentRunID, _ := run.Metadata["parent_run_id"].(string); parentRunID != "" {
		payload["parent_run_id"] = parentRunID
	}
	switch ev.Type {
	case EventToolStart:
		copyPayloadKeys(payload, ev.Payload, "tool", "tool_call_id", "arg_keys")
	case EventToolEnd:
		copyPayloadKeys(payload, ev.Payload, "tool", "tool_call_id", "success", "elapsed_ms")
		if success, _ := ev.Payload["success"].(bool); !success {
			payload["error_class"] = "tool_failed"
		}
		if ev.Payload["preview"] != nil {
			payload["result_redaction"] = "preview_omitted"
		}
	case EventQuestionRequested:
		payload["question_id"] = ev.ID
		copyPayloadKeys(payload, ev.Payload, "kind", "tool", "tool_call_id", "why_blocking", "blocking_scope")
		if question, _ := ev.Payload["question"].(string); question != "" {
			payload["question_preview"] = textPreview(question)
		}
		if options := payloadStringSlice(ev.Payload["options"]); len(options) > 0 {
			payload["options_count"] = len(options)
		}
	case EventQuestionAnswered:
		copyPayloadKeys(payload, ev.Payload, "question_id", "answered_message_id", "selected_option_count", "has_free_text", "redaction_level")
	case EventUsage:
		for k, v := range durableStats(ev.Payload) {
			payload[k] = v
		}
	case EventMessageDone:
		copyPayloadKeys(payload, ev.Payload, "delivered")
		if preview := textPreview(ev.Content); preview != "" {
			payload["final_text_preview"] = preview
		}
	case EventCancelled:
		payload["reason_class"] = "context_canceled"
	case EventError:
		payload["error_class"] = "agent_loop_error"
	}
	return payload
}

func (h *Hub) recordQuestionRequested(ctx context.Context, run *Run, ev OutboundEvent, stored runstore.Event) error {
	store, ok := h.lifecycle.(questionLifecycleStore)
	if !ok {
		return nil
	}
	question, _ := ev.Payload["question"].(string)
	if strings.TrimSpace(question) == "" {
		return nil
	}
	kind, _ := ev.Payload["kind"].(string)
	tool, _ := ev.Payload["tool"].(string)
	toolCallID, _ := ev.Payload["tool_call_id"].(string)
	_, err := store.RecordQuestionRequested(ctx, runstore.RecordQuestionRequestedParams{
		ID:           stored.ID,
		RunID:        run.ID,
		EventID:      stored.ID,
		ThreadID:     run.ThreadID,
		ActorID:      stored.ActorID,
		Channel:      string(run.Channel),
		Kind:         kind,
		QuestionText: question,
		Options:      payloadStringSlice(ev.Payload["options"]),
		RequestedAt:  stored.CreatedAt,
		Producer: map[string]any{
			"tool":         tool,
			"tool_call_id": toolCallID,
		},
		Metadata: map[string]any{"redaction_level": runstore.RedactionMetadata},
	})
	if err != nil {
		return fmt.Errorf("chat: record question requested: %w", err)
	}
	return nil
}

func copyPayloadKeys(dst, src map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}

func payloadStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return append([]string(nil), s...)
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

func decodeQuestionOptions(raw string) []string {
	var options []string
	_ = json.Unmarshal([]byte(raw), &options)
	return options
}

func questionAnswerIdempotencyKey(questionID, messageID string) string {
	if messageID == "" {
		return "question_answered:" + questionID
	}
	return "question_answered:" + questionID + ":" + messageID
}

func durableStats(payload map[string]any) map[string]any {
	stats := map[string]any{}
	copyPayloadKeys(
		stats,
		payload,
		"llm_calls",
		"tool_calls",
		"loop_steps",
		"tokens_prompt",
		"tokens_completion",
		"tokens_total",
		"cost_usd",
		"terminal_tool",
	)
	return stats
}

func chatRunFromStored(stored runstore.Run) *Run {
	metadata := map[string]any{}
	if stored.MetadataJSON != "" {
		_ = json.Unmarshal([]byte(stored.MetadataJSON), &metadata)
	}
	if stored.ParentRunID != "" {
		metadata["parent_run_id"] = stored.ParentRunID
	}
	return &Run{
		ID:          stored.ID,
		ThreadID:    stored.ThreadID,
		PrincipalID: stored.PrincipalID,
		ActorID:     stored.ActorID,
		Channel:     Channel(stored.Channel),
		Status:      RunStatus(stored.Status),
		Model:       stored.Model,
		StartedAt:   stored.StartedAt,
		CompletedAt: stored.CompletedAt,
		LastError:   stored.LastError,
		Metadata:    metadata,
	}
}

func firstTime(candidates ...*time.Time) *time.Time {
	for _, candidate := range candidates {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}

func finalTextPreview(run *Run) string {
	text, _ := run.Metadata["final_text"].(string)
	return textPreview(text)
}

func textPreview(text string) string {
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > 280 {
		runes = runes[:280]
	}
	return string(runes)
}

// newRunID + newEventID generate short hex correlators (8 bytes = 16 hex
// chars). Match the agent runtime run_id format so logs across the two layers
// correlate naturally.
func newRunID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(buf[:])
}

func newEventID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "0000"
	}
	return hex.EncodeToString(buf[:])
}

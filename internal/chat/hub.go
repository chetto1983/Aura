package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aura/aura/internal/identity"
	runstore "github.com/aura/aura/internal/storage/runs"
)

// AgentLoop is the single channel-neutral entry point into the agent
// runtime. Production wiring binds this to a small adapter over
// internal/agentruntime.Run (which already speaks the OnEvent callback
// pattern). Tests inject a fake.
//
// The contract: Run reads the InboundMessage, executes one agent turn
// (LLM calls + tool dispatch), emits OutboundEvents through the emit
// callback as work progresses, and returns when the run reaches a
// terminal state (Done / Error / Cancelled). The Run record passed in is
// mutated in place — adapters see the final Status / CompletedAt /
// LastError after Run returns.
type AgentLoop interface {
	Run(ctx context.Context, run *Run, msg InboundMessage, emit EmitFn) error
}

// EmitFn is the per-event publish callback the Agent Loop uses to push
// OutboundEvents toward the outbound adapter selected for this run's
// channel. Errors from emit are logged but do not abort the run — a
// downed outbound (e.g. Telegram API momentarily unreachable) shouldn't
// kill the agent mid-turn; the adapter is responsible for retry.
type EmitFn func(event OutboundEvent) error

// InboundAdapter translates a channel-specific raw payload (tele.Update,
// HTTP request, scheduler tick) into an InboundMessage. One adapter per
// channel; registered with the Hub at boot.
type InboundAdapter interface {
	Channel() Channel
	Normalize(ctx context.Context, raw any) (InboundMessage, error)
}

// OutboundAdapter consumes OutboundEvents for a given channel. Multiple
// outbound adapters can exist per channel (e.g. Telegram has the
// progressive-edit adapter for streaming and a deferred adapter for
// silent-mode heartbeat results) — the Hub picks based on
// InboundMessage.Mode.
type OutboundAdapter interface {
	Channel() Channel
	Mode() DeliveryMode
	Deliver(ctx context.Context, event OutboundEvent) error
}

// LifecycleStore is the minimal durable run/event store the Hub needs.
// It records metadata-level lifecycle state only; full prompts, tool
// arguments, and adapter payloads stay out of the default trace plane.
type LifecycleStore interface {
	CreateOrGetRun(ctx context.Context, params runstore.CreateRunParams) (runstore.Run, bool, error)
	AppendEvent(ctx context.Context, params runstore.AppendEventParams) (runstore.Event, error)
}

// Hub is the central dispatcher. Boot wires every adapter once; Receive
// is called by inbound entry points (Telegram update handler, /api/chat
// handler, scheduler tick) with the channel-specific raw payload.
type Hub struct {
	loop         AgentLoop
	lifecycle    LifecycleStore
	inbound      map[Channel]InboundAdapter
	outbound     map[outboundKey][]OutboundAdapter
	logger       *slog.Logger
	cancels      sync.Map // RunID → context.CancelFunc; used by /stop
	seqCounter   atomic.Int64
	threadStatus sync.Map // ThreadID → RunStatus; updated after each dispatch
}

// ThreadRunStatus returns the most recent run status for the given thread.
// Returns ("", false) when no run has been dispatched for this thread yet.
func (h *Hub) ThreadRunStatus(threadID string) (RunStatus, bool) {
	if v, ok := h.threadStatus.Load(threadID); ok {
		if s, ok := v.(RunStatus); ok {
			return s, true
		}
	}
	return "", false
}

type outboundKey struct {
	Channel Channel
	Mode    DeliveryMode
}

// Config bundles the dependencies for New.
type Config struct {
	Loop           AgentLoop
	LifecycleStore LifecycleStore
	Logger         *slog.Logger
}

// New constructs an empty Hub. Adapters are registered via
// RegisterInbound / RegisterOutbound before the first Receive call.
func New(cfg Config) (*Hub, error) {
	if cfg.Loop == nil {
		return nil, errors.New("chathub: AgentLoop is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		loop:      cfg.Loop,
		lifecycle: cfg.LifecycleStore,
		inbound:   make(map[Channel]InboundAdapter),
		outbound:  make(map[outboundKey][]OutboundAdapter),
		logger:    logger,
	}, nil
}

// RegisterInbound binds an InboundAdapter to its declared channel. A
// second register for the same channel replaces the first (with a warn
// log) so tests can swap fakes without restart.
func (h *Hub) RegisterInbound(adapter InboundAdapter) {
	if adapter == nil {
		return
	}
	ch := adapter.Channel()
	if _, exists := h.inbound[ch]; exists {
		h.logger.Warn("chathub: replacing inbound adapter", "channel", ch)
	}
	h.inbound[ch] = adapter
}

// RegisterOutbound binds an OutboundAdapter. Multiple adapters per
// (channel, mode) pair are allowed — they're called in registration order.
// Use-case: a telegram channel can have both the live-edit adapter and a
// trace-archive adapter both watching the same event stream.
func (h *Hub) RegisterOutbound(adapter OutboundAdapter) {
	if adapter == nil {
		return
	}
	key := outboundKey{Channel: adapter.Channel(), Mode: adapter.Mode()}
	h.outbound[key] = append(h.outbound[key], adapter)
}

// Receive is the inbound entry point. The raw payload is normalized via
// the registered adapter for inboundChannel, then handed to the AgentLoop
// alongside an emit closure that fans out to every outbound adapter
// matching (channel, msg.Mode). Receive blocks until the run reaches a
// terminal state OR ctx is cancelled.
func (h *Hub) Receive(ctx context.Context, inboundChannel Channel, raw any) (*Run, error) {
	adapter, ok := h.inbound[inboundChannel]
	if !ok {
		return nil, fmt.Errorf("chathub: no inbound adapter for channel %q", inboundChannel)
	}
	msg, err := adapter.Normalize(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("chathub: normalize %s: %w", inboundChannel, err)
	}
	return h.dispatch(ctx, msg)
}

// ReceiveMessage skips the inbound-adapter step for callers that already
// hold a normalized InboundMessage (test fixtures, replay tools, the
// scheduled-task path that produces synthetic messages directly).
func (h *Hub) ReceiveMessage(ctx context.Context, msg InboundMessage) (*Run, error) {
	return h.dispatch(ctx, msg)
}

func (h *Hub) dispatch(ctx context.Context, msg InboundMessage) (*Run, error) {
	runID := newRunID()
	actorID := identity.ActorIDFromContext(ctx)
	meta := map[string]any{}
	if msg.ParentRunID != "" {
		meta["parent_run_id"] = msg.ParentRunID
	}
	startedAt := time.Now().UTC()
	run := &Run{
		ID:          runID,
		ThreadID:    msg.ThreadID,
		PrincipalID: msg.PrincipalID,
		ActorID:     actorID,
		Channel:     msg.Channel,
		Status:      RunStatusRunning,
		StartedAt:   startedAt,
		Metadata:    meta,
	}
	if h.lifecycle != nil {
		stored, created, err := h.lifecycle.CreateOrGetRun(ctx, runstore.CreateRunParams{
			ID:             run.ID,
			ParentRunID:    msg.ParentRunID,
			ThreadID:       msg.ThreadID,
			PrincipalID:    msg.PrincipalID,
			ActorID:        actorID,
			Channel:        string(msg.Channel),
			Status:         string(RunStatusRunning),
			IdempotencyKey: msg.ID,
			StartedAt:      startedAt,
			Metadata:       lifecycleRunMetadata(msg),
		})
		if err != nil {
			return nil, fmt.Errorf("chathub: persist run: %w", err)
		}
		run = chatRunFromStored(stored)
		if !created {
			return run, nil
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	runCtx = identity.WithRunID(runCtx, run.ID)
	if recorder, ok := h.lifecycle.(identity.AuthorizationDenialRecorder); ok {
		runCtx = identity.WithAuthorizationDenialRecorder(runCtx, recorder)
	}
	h.cancels.Store(runID, cancel)
	defer func() {
		h.cancels.Delete(runID)
		cancel()
	}()

	emit := h.makeEmit(runCtx, context.WithoutCancel(ctx), run, msg.Mode)

	if err := emit(OutboundEvent{
		Type:    EventRunStarted,
		Payload: map[string]any{"principal_id": msg.PrincipalID, "thread_id": msg.ThreadID, "channel": string(msg.Channel)},
	}); err != nil {
		h.logger.Warn("chathub: emit run_started failed", "run_id", runID, "err", err)
	}

	err := h.loop.Run(runCtx, run, msg, emit)
	now := time.Now().UTC()
	run.CompletedAt = &now
	if err != nil {
		if errors.Is(err, context.Canceled) {
			run.Status = RunStatusCancelled
			_ = emit(OutboundEvent{Type: EventCancelled, Payload: map[string]any{"reason": "context canceled"}})
		} else if run.Status == RunStatusWaitingForUser {
			// AgentLoop set WaitingForUser before returning error (e.g., ask_user
			// out-of-range reply — the question is still pending, not a real failure).
			// Suppress EventError; the caller already sent a reject message.
		} else {
			run.Status = RunStatusFailed
			run.LastError = err.Error()
			_ = emit(OutboundEvent{Type: EventError, Payload: map[string]any{"error": err.Error()}})
		}
		_ = emit(OutboundEvent{Type: EventDone, Payload: map[string]any{"status": string(run.Status)}})
		h.threadStatus.Store(run.ThreadID, run.Status)
		if run.Status == RunStatusWaitingForUser {
			return run, nil // not a real error; question still pending
		}
		return run, err
	}
	if run.Status == RunStatusRunning {
		run.Status = RunStatusCompleted
	}
	_ = emit(OutboundEvent{Type: EventDone, Payload: map[string]any{"status": string(run.Status)}})
	h.threadStatus.Store(run.ThreadID, run.Status)
	return run, nil
}

// Stop best-effort cancels an in-flight run by ID. Idempotent — calling
// twice with the same ID is safe; calling with an unknown ID is a no-op.
// PRD §11.5: "/stop usa una registry centrale run_id → cancel".
func (h *Hub) Stop(runID string) bool {
	v, ok := h.cancels.Load(runID)
	if !ok {
		return false
	}
	if cancel, ok := v.(context.CancelFunc); ok {
		cancel()
		return true
	}
	return false
}

// makeEmit returns a closure that fans out an OutboundEvent to every
// adapter registered for (msg.Channel, msg.Mode). Adds the run-level
// fields (RunID, ThreadID, Seq, CreatedAt) so the AgentLoop callback
// only fills the event-specific bits.
func (h *Hub) makeEmit(ctx, persistCtx context.Context, run *Run, mode DeliveryMode) EmitFn {
	key := outboundKey{Channel: run.Channel, Mode: mode}
	adapters := h.outbound[key]
	if len(adapters) == 0 {
		// Fallback: try the deferred adapter set for this channel. Many
		// channels register only one outbound regardless of mode — pick it
		// up so silent-mode heartbeat tests don't crash on missing wiring.
		for k, v := range h.outbound {
			if k.Channel == run.Channel {
				adapters = v
				break
			}
		}
	}

	return func(ev OutboundEvent) error {
		if ev.ID == "" {
			ev.ID = newEventID()
		}
		ev.RunID = run.ID
		ev.ThreadID = run.ThreadID
		ev.Channel = run.Channel
		if ev.Seq == 0 {
			ev.Seq = h.seqCounter.Add(1)
		}
		if ev.CreatedAt.IsZero() {
			ev.CreatedAt = time.Now().UTC()
		}
		var firstErr error
		if h.lifecycle != nil && isDurableRunEvent(ev.Type) {
			if err := h.persistLifecycleEvent(persistCtx, run, ev); err != nil {
				firstErr = err
				h.logger.Error("chathub: persist lifecycle event failed",
					"run_id", run.ID, "channel", run.Channel,
					"event", ev.Type, "err", err)
			}
		}
		for _, a := range adapters {
			if err := a.Deliver(ctx, ev); err != nil && firstErr == nil {
				firstErr = err
				h.logger.Warn("chathub: outbound deliver failed",
					"run_id", run.ID, "channel", run.Channel, "mode", mode,
					"event", ev.Type, "err", err)
			}
		}
		return firstErr
	}
}

func (h *Hub) persistLifecycleEvent(ctx context.Context, run *Run, ev OutboundEvent) error {
	persistCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := h.lifecycle.AppendEvent(persistCtx, lifecycleEventParams(run, ev))
	if err != nil {
		return fmt.Errorf("chathub: append lifecycle event: %w", err)
	}
	return nil
}

func isDurableRunEvent(eventType EventType) bool {
	switch eventType {
	case EventRunStarted, EventToolStart, EventToolEnd, EventMessageDone, EventUsage, EventDone, EventError, EventCancelled:
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
	case EventUsage:
		params.Stats = durableStats(ev.Payload)
	case EventMessageDone:
		params.FinalTextPreview = textPreview(ev.Content)
	case EventDone:
		params.RunStatus = string(run.Status)
		params.CompletedAt = firstTime(run.CompletedAt, &ev.CreatedAt)
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

func copyPayloadKeys(dst, src map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
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
// chars). Match the format already used by agentruntime.newRunID so logs
// across the two layers correlate naturally.
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

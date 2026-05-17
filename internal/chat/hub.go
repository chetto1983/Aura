package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aura/aura/internal/identity"
	runstore "github.com/aura/aura/internal/storage/runs"
)

// AgentLoop is the single channel-neutral entry point into the agent runtime.
// Production wiring binds this to a small adapter over agent.Run, which already
// speaks the OnEvent callback pattern. Tests inject a fake.
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

// EmitFn is the per-event publish callback the agent runtime uses to push
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

type questionLifecycleStore interface {
	GetQuestion(ctx context.Context, id string) (runstore.Question, error)
	RecordQuestionRequested(ctx context.Context, params runstore.RecordQuestionRequestedParams) (runstore.Question, error)
	RecordQuestionAnswered(ctx context.Context, params runstore.RecordQuestionAnsweredParams) (runstore.Question, error)
	LatestPendingQuestion(ctx context.Context, threadID, channel string) (runstore.Question, bool, error)
}

type PendingQuestion struct {
	ID      string
	RunID   string
	Kind    string
	Options []string
}

// Hub is the central dispatcher. Boot wires every adapter once; Receive
// is called by inbound entry points (Telegram update handler, /api/chat
// handler, scheduler tick) with the channel-specific raw payload.
type Hub struct {
	loop          AgentLoop
	lifecycle     LifecycleStore
	inbound       map[Channel]InboundAdapter
	outbound      map[outboundKey][]OutboundAdapter
	logger        *slog.Logger
	cancels       sync.Map // RunID → context.CancelFunc; used by /stop
	seqCounter    atomic.Int64
	threadStatus  sync.Map // ThreadID → RunStatus; updated after each dispatch
	completedRuns sync.Map // RunID → completedRunEntry; consumed by WaitForRun
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
		return nil, errors.New("chat: AgentLoop is required")
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
		h.logger.Warn("chat: replacing inbound adapter", "channel", ch)
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
		return nil, fmt.Errorf("chat: no inbound adapter for channel %q", inboundChannel)
	}
	msg, err := adapter.Normalize(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("chat: normalize %s: %w", inboundChannel, err)
	}
	return h.dispatch(ctx, msg)
}

// ReceiveMessage skips the inbound-adapter step for callers that already
// hold a normalized InboundMessage (test fixtures, replay tools, the
// scheduled-task path that produces synthetic messages directly).
func (h *Hub) ReceiveMessage(ctx context.Context, msg InboundMessage) (*Run, error) {
	return h.dispatch(ctx, msg)
}

func (h *Hub) PendingQuestion(ctx context.Context, threadID string, channel Channel) (PendingQuestion, bool, error) {
	store, ok := h.lifecycle.(questionLifecycleStore)
	if !ok {
		return PendingQuestion{}, false, nil
	}
	question, ok, err := store.LatestPendingQuestion(ctx, threadID, string(channel))
	if err != nil || !ok {
		return PendingQuestion{}, ok, err
	}
	return PendingQuestion{
		ID:      question.ID,
		RunID:   question.RunID,
		Kind:    question.Kind,
		Options: decodeQuestionOptions(question.OptionsJSON),
	}, true, nil
}

func (h *Hub) RecordQuestionAnswer(ctx context.Context, run *Run, msg InboundMessage, answer QuestionAnswer) error {
	store, ok := h.lifecycle.(questionLifecycleStore)
	if !ok {
		return nil
	}
	if run == nil || run.ID == "" {
		return errors.New("chat: question answer run is required")
	}
	questionID := answer.QuestionID
	var pending runstore.Question
	if questionID == "" {
		found, ok, err := store.LatestPendingQuestion(ctx, msg.ThreadID, string(msg.Channel))
		if err != nil {
			return err
		}
		if !ok {
			return runstore.ErrQuestionNotFound
		}
		pending = found
		questionID = pending.ID
	} else {
		found, err := store.GetQuestion(ctx, questionID)
		if err != nil {
			return err
		}
		pending = found
		if pending.Channel != string(msg.Channel) {
			return runstore.ErrQuestionChannelMismatch
		}
		if pending.Status != runstore.QuestionStatusWaiting {
			return runstore.ErrQuestionNotWaiting
		}
	}
	event, err := h.lifecycle.AppendEvent(ctx, runstore.AppendEventParams{
		RunID:          run.ID,
		Type:           string(EventQuestionAnswered),
		ActorID:        run.ActorID,
		CausationID:    questionID,
		IdempotencyKey: questionAnswerIdempotencyKey(questionID, msg.ID),
		Payload: map[string]any{
			"question_id":           questionID,
			"answered_message_id":   answer.AnsweredMessageID,
			"selected_option_count": len(answer.SelectedOptionIDs),
			"has_free_text":         answer.FreeText != "",
			"redaction_level":       runstore.RedactionMetadata,
		},
		RedactionLevel: runstore.RedactionMetadata,
		CreatedAt:      time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("chat: append question answer event: %w", err)
	}
	_, err = store.RecordQuestionAnswered(ctx, runstore.RecordQuestionAnsweredParams{
		ID:                questionID,
		AnswerRunID:       run.ID,
		AnswerEventID:     event.ID,
		ThreadID:          msg.ThreadID,
		Channel:           string(msg.Channel),
		ActorID:           run.ActorID,
		SelectedOptionIDs: answer.SelectedOptionIDs,
		FreeText:          answer.FreeText,
		AnsweredMessageID: answer.AnsweredMessageID,
		AnsweredAt:        event.CreatedAt,
		Metadata:          map[string]any{"answer_channel": string(msg.Channel)},
	})
	if err != nil {
		return fmt.Errorf("chat: record question answer: %w", err)
	}
	return nil
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
			return nil, fmt.Errorf("chat: persist run: %w", err)
		}
		run = chatRunFromStored(stored)
		if !created {
			return run, nil
		}
	}

	// Store swarm run result for WaitForRun when dispatch returns (any path).
	defer func() {
		if msg.Channel == ChannelSwarm && run != nil {
			h.storeCompletedRun(run)
		}
	}()

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
		h.logger.Warn("chat: emit run_started failed", "run_id", runID, "err", err)
	}

	if msg.Channel == ChannelSwarm {
		if authErr := h.authorizeSwarmDispatch(ctx, msg, actorID); authErr != nil {
			now := time.Now().UTC()
			run.CompletedAt = &now
			run.Status = RunStatusFailed
			run.LastError = "swarm_dispatch_denied"
			_ = emit(OutboundEvent{Type: EventError, Payload: map[string]any{"error": "swarm_dispatch_denied"}})
			_ = emit(OutboundEvent{Type: EventDone, Payload: map[string]any{"status": string(run.Status)}})
			h.threadStatus.Store(run.ThreadID, run.Status)
			return run, authErr
		}
	}

	err := h.loop.Run(runCtx, run, msg, emit)
	now := time.Now().UTC()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			run.CompletedAt = &now
			run.Status = RunStatusCancelled
			_ = emit(OutboundEvent{Type: EventCancelled, Payload: map[string]any{"reason": "context canceled"}})
		} else if run.Status == RunStatusWaitingForUser {
			// AgentLoop set WaitingForUser before returning error (e.g., ask_user
			// out-of-range reply — the question is still pending, not a real failure).
			// Suppress EventError; the caller already sent a reject message.
		} else {
			run.CompletedAt = &now
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
		run.CompletedAt = &now
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
				h.logger.Error("chat: persist lifecycle event failed",
					"run_id", run.ID, "channel", run.Channel,
					"event", ev.Type, "err", err)
			}
		}
		for _, a := range adapters {
			if err := a.Deliver(ctx, ev); err != nil && firstErr == nil {
				firstErr = err
				h.logger.Warn("chat: outbound deliver failed",
					"run_id", run.ID, "channel", run.Channel, "mode", mode,
					"event", ev.Type, "err", err)
			}
		}
		return firstErr
	}
}

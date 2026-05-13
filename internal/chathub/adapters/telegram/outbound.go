package telegramadapter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/chathub"
	tele "gopkg.in/telebot.v4"
)

// Defaults mirror internal/telegram/streaming.go so chathub-routed Telegram
// turns feel byte-identical to the legacy direct path. Telegram rate-limits
// editMessage to ~1/s per chat; 600ms is the tuned ceiling that ships today.
const (
	defaultStreamingEditThrottle    = 600 * time.Millisecond
	defaultStreamingMinThreshold    = 30
	defaultPlaceholderText          = "⏳"
	defaultErrorReplyText           = "Sorry, I couldn't process your message. Please try again."
)

// Outbound implements chathub.OutboundAdapter for Telegram streaming
// progressive edits. One Outbound instance is shared across all runs — per-
// run state (current message handle, last edit timestamp, accumulated
// content) is keyed by RunID inside a sync.Map so concurrent conversations
// do not bleed into one another.
//
// The adapter is intentionally minimal compared to the legacy
// internal/telegram/streaming.go consumeStream: chathub already aggregates
// reasoning / content at the agentruntime layer (EventMessageDelta carries
// the assistant text only). When Slice 4 ships SSE streaming the same
// adapter can pass through reasoning by listening to a future
// EventReasoningDelta — for now Telegram users see what they saw before.
type Outbound struct {
	logger   *slog.Logger
	throttle time.Duration
	minBytes int

	runs sync.Map // RunID -> *runState
}

// Config tunes the Outbound. Zero values fall back to package defaults.
type Config struct {
	Logger       *slog.Logger
	EditThrottle time.Duration
	MinThreshold int
}

// NewOutbound constructs an Outbound adapter. Wire one and call
// hub.RegisterOutbound(o) at boot.
func NewOutbound(cfg Config) *Outbound {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	throttle := cfg.EditThrottle
	if throttle <= 0 {
		throttle = defaultStreamingEditThrottle
	}
	minBytes := cfg.MinThreshold
	if minBytes <= 0 {
		minBytes = defaultStreamingMinThreshold
	}
	return &Outbound{logger: logger, throttle: throttle, minBytes: minBytes}
}

// Channel + Mode satisfy chathub.OutboundAdapter.
func (*Outbound) Channel() chathub.Channel    { return chathub.ChannelTelegram }
func (*Outbound) Mode() chathub.DeliveryMode  { return chathub.DeliveryModeStreaming }

// runState is the per-RunID progressive-edit state. We rely on RunID being
// monotonic per Hub instance + unique per run; the Hub mints a fresh ID
// before the first emit so the first event a run sees lazily creates this
// record.
type runState struct {
	mu          sync.Mutex
	bot         tele.API
	recipient   tele.Recipient
	message     *tele.Message // the placeholder or live progressive edit
	buffer      strings.Builder
	lastEditAt  time.Time
	finalized   bool
}

// Deliver consumes a single OutboundEvent. The adapter dispatches on Type
// and updates the per-run state under the run lock.
func (o *Outbound) Deliver(_ context.Context, event chathub.OutboundEvent) error {
	switch event.Type {
	case chathub.EventRunStarted:
		return o.handleStart(event)
	case chathub.EventMessageDelta:
		return o.handleDelta(event)
	case chathub.EventMessageDone:
		return o.handleDone(event)
	case chathub.EventError:
		return o.handleError(event)
	case chathub.EventDone, chathub.EventCancelled:
		return o.handleTerminal(event)
	default:
		// ToolStart / ToolEnd / Usage / etc. are not surfaced to the
		// Telegram user today — they're for SSE / dashboards. Silent
		// no-op keeps the adapter forward-compatible.
		return nil
	}
}

// handleStart records the run-specific tele.API + recipient handles. We do
// NOT yet send the placeholder; the legacy bot already sends one before
// dispatching the agent loop, and we want to mirror that timing so users
// don't see two Telegram messages.
func (o *Outbound) handleStart(event chathub.OutboundEvent) error {
	state, _ := o.runs.LoadOrStore(event.RunID, &runState{})
	rs, ok := state.(*runState)
	if !ok {
		return fmt.Errorf("telegramadapter: invalid runState for run %s", event.RunID)
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	// The Hub injects channel-specific handles via Run.Metadata before
	// it emits run_started. We re-read on every entry so a Hub restart
	// or a fresh runState gets the latest handles.
	if rs.bot == nil {
		bot, _ := event.Payload["tele_bot"].(tele.API)
		rs.bot = bot
	}
	if rs.recipient == nil {
		rec, _ := event.Payload["tele_recipient"].(tele.Recipient)
		rs.recipient = rec
	}
	if rs.message == nil {
		msg, _ := event.Payload["tele_placeholder"].(*tele.Message)
		rs.message = msg
	}
	return nil
}

// handleDelta accumulates the delta and triggers a rate-limited edit. The
// throttle mirrors the legacy 600ms ceiling so a 1 token/ms producer
// settles into ~1 edit/600ms — comfortably inside Telegram's API budget.
func (o *Outbound) handleDelta(event chathub.OutboundEvent) error {
	rs := o.getRun(event.RunID)
	if rs == nil {
		return nil
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.finalized {
		return nil
	}
	rs.buffer.WriteString(event.Content)
	if rs.buffer.Len() < o.minBytes {
		return nil
	}
	if !rs.lastEditAt.IsZero() && time.Since(rs.lastEditAt) < o.throttle {
		return nil
	}
	return o.flushLocked(rs, false)
}

// handleDone is the terminal "the LLM produced a complete answer" event.
// We append any tail content the throttle held back and emit a final edit
// with the full message. After this no further deltas are processed.
func (o *Outbound) handleDone(event chathub.OutboundEvent) error {
	rs := o.getRun(event.RunID)
	if rs == nil {
		return nil
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	// If the agentruntime emitted a non-empty Content on EventMessageDone,
	// prefer it over the locally-accumulated buffer (the legacy path used
	// Result.Text as the source of truth, not the streamed concatenation).
	if event.Content != "" {
		rs.buffer.Reset()
		rs.buffer.WriteString(event.Content)
	}
	rs.finalized = true
	return o.flushLocked(rs, true)
}

// handleError emits a user-facing error message and finalizes the run.
// Mirrors the legacy "Sorry, I couldn't process your message" reply.
func (o *Outbound) handleError(event chathub.OutboundEvent) error {
	rs := o.getRun(event.RunID)
	if rs == nil {
		return nil
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.finalized {
		return nil
	}
	msg := defaultErrorReplyText
	if errText, _ := event.Payload["error"].(string); errText != "" {
		// Keep the user-facing copy generic — the error string can leak
		// internal detail. Operator logs already captured it via Hub.
		o.logger.Warn("telegramadapter: agent error", "run_id", event.RunID, "err", errText)
	}
	rs.buffer.Reset()
	rs.buffer.WriteString(msg)
	rs.finalized = true
	return o.flushLocked(rs, true)
}

// handleTerminal releases the per-run state after Done / Cancelled. Always
// the last event for a run — nothing more arrives after.
func (o *Outbound) handleTerminal(event chathub.OutboundEvent) error {
	o.runs.Delete(event.RunID)
	return nil
}

// flushLocked sends or edits the Telegram message with the current
// buffered content. Caller MUST hold rs.mu. When final is true and the
// buffer is empty we skip — Telegram refuses empty edits and the legacy
// path silently dropped them too.
func (o *Outbound) flushLocked(rs *runState, final bool) error {
	text := strings.TrimSpace(rs.buffer.String())
	if text == "" {
		return nil
	}
	if rs.bot == nil || rs.recipient == nil {
		// Without a bot+recipient we can't deliver. Mirrors the bot's
		// "no recipient set, drop" behavior; the legacy path would
		// panic on a nil recipient — we degrade to a logged no-op.
		o.logger.Warn("telegramadapter: missing bot/recipient on flush; dropping", "final", final)
		return nil
	}
	defer func() {
		rs.lastEditAt = time.Now()
	}()
	if rs.message != nil {
		edited, err := rs.bot.Edit(rs.message, text)
		if err != nil {
			o.logger.Warn("telegramadapter: edit failed", "err", err, "final", final)
			return err
		}
		rs.message = edited
		return nil
	}
	sent, err := rs.bot.Send(rs.recipient, text)
	if err != nil {
		o.logger.Warn("telegramadapter: send failed", "err", err, "final", final)
		return err
	}
	rs.message = sent
	return nil
}

func (o *Outbound) getRun(runID string) *runState {
	v, ok := o.runs.Load(runID)
	if !ok {
		return nil
	}
	rs, _ := v.(*runState)
	return rs
}

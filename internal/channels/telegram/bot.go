// Package telegram is the Telegram channel (Phase 13 / Slice 9b, UX-02). This
// file is the telebot.v4 wrapper implementing the channels.Channel lifecycle:
// Start constructs the bot, registers the text handler, and launches the polling
// goroutine; Stop drains it goleak-clean. The per-turn AG-UI fanout wiring lives
// in agui_subscriber.go; the two render consumers live in status_pane.go and
// renderer.go.
package telegram

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"sync"
	"time"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/channels"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/llm"
	profileflow "github.com/chetto1983/aura/internal/onboarding"
	"github.com/chetto1983/aura/internal/profile"
)

// channelName keys AURA_CHANNEL_TELEGRAM_ENABLED (the registry enable gate).
const channelName = "telegram"

// compile-time proof that *Telegram satisfies the narrow channels.Channel
// lifecycle contract (Name/Start/Stop/IsHealthy) — the registry mounts it as one.
var _ channels.Channel = (*Telegram)(nil)

// turnDriver is the per-turn loop seam: it yields the agent's *agent.Event stream
// for one round over a conversation. *runner.Runner.Turn satisfies it. Declaring
// it consumer-side (not importing *runner.Runner directly) keeps the telegram
// package free of a runner import in the hot path AND lets unit tests inject a
// synthetic event stream through the real Translate→Fanout path without a live
// Runner/DB (the bot is exercised Offline).
type turnDriver func(ctx context.Context, convID string, userMsg *string) iter.Seq2[*agent.Event, error]

type documentIngestor interface {
	IngestPath(ctx context.Context, req documents.IngestRequest, path string) (*documents.Job, error)
}

// botSender is the narrow telebot surface the render consumers (status_pane.go /
// renderer.go) call. *tele.Bot satisfies it implicitly; tests inject a recording
// double so a Send/Edit is asserted on the RESPONSE payload (the spike-017/019
// ground truth: bot-sent messages never appear in getUpdates). Declared here so
// both consumers share one seam and the production bot is the only real impl.
type botSender interface {
	Send(to tele.Recipient, what any, opts ...any) (*tele.Message, error)
	Edit(msg tele.Editable, what any, opts ...any) (*tele.Message, error)
}

// Deps are the Telegram channel's constructor inputs. Runner drives the loop;
// Store persists onboarding/accounts; the throttles come from the channel Config.
// Token is the Bot API credential (empty → the registry enable gate keeps the
// channel from starting). IDGen is injectable so tests pin deterministic AG-UI
// ids; nil → the production uuid generator.
type Deps struct {
	// Turn is the per-turn loop driver (runner.Runner.Turn). Required for live
	// turns; a nil Turn means the channel can start (poll) but a message handler
	// would have nothing to drive — wired by the composition root.
	Turn turnDriver

	// Token is TELEGRAM_BOT_TOKEN (upstream naming).
	Token string

	// Store is the onboarding/account DB seam (plan 13-01) and the live inbound
	// auth resolver. If Store (or the test-only profileAccounts override) is nil,
	// inbound Telegram handlers fail closed except for /start <token> activation.
	Store *Store

	// Profile is the per-identity Agent.md store. Nil means profile onboarding
	// degrades with a user-facing setup message instead of panicking.
	Profile *profile.Store

	// AnswerExtractor parses free-text onboarding answers (Identity/Work/Projects/
	// Social) into structured fields. Nil → those steps fall back to keyword parsing.
	AnswerExtractor profileflow.AnswerExtractor

	// Multimodal carries the 9c sidecar wiring (STT/TTS/vision/documents) the media
	// handlers (OnVoice/OnPhoto/OnDocument) + the TTS-out path read. A zero value
	// means a modality is unconfigured — the handler degrades, never panics. Built
	// by the composition root from config.Config (serve_channels.go).
	Multimodal MultimodalConfig

	// DocumentIngest optionally routes document uploads into Aura's native
	// document ingestion pipeline. Nil preserves the legacy convert-to-markdown
	// path used by existing deployments and tests.
	DocumentIngest documentIngestor

	// Command backends drive the bot-intercept dispatch (commands.go). Search ==
	// conversations.SearchConversationTurns (CLI parity); Cost == the cachemetrics
	// daily aggregation; Clear == conversations.Store.Delete (the /clear hard-delete);
	// Prices/Model render the /cost USD via llm.CostUSD. A nil backend degrades its
	// command to an "unavailable" reply (never a panic).
	Search searchBackend
	Cost   costBackend
	Clear  clearBackend
	Prices map[string]llm.Price
	Model  string

	// Resume is the HITL seam (hitl.go): the Runner's pause surface
	// (PendingFor/SubmitAnswer — *runner.Runner satisfies it). Nil → HITL is inert
	// (no button render, no resume) but the channel still serves plain turns. The
	// continuation turn after a pause resolves is built locally in hitlFor.
	Resume resumeRunner

	// StatusThrottle / ContentThrottle bound the two render consumers; ChatRate
	// bounds the per-chat send queue. Zero → the package defaults (see config).
	StatusThrottleMS  int
	ContentThrottleMS int
	ChatRateLimitMS   int

	// ShowReasoning surfaces the live chain-of-thought in the status pane (default
	// false → redacted lifecycle). It is the master AURA_SHOW_REASONING switch from
	// llm.Config, propagated here by the composition root so the agent's exclude flag,
	// the AG-UI translator's redaction, and this pane all honor one setting.
	// ReasoningFIFORunes caps that rolling window (zero → the package default) and
	// comes from telegram.LoadConfig.
	ShowReasoning      bool
	ReasoningFIFORunes int

	// Offline forces tele.Settings.Offline (unit tests: no getMe, no network).
	Offline bool

	// consumerFactory overrides the per-turn consumer construction. nil (the
	// common path) builds the real status_pane/renderer. Tests inject recorders to
	// prove the fanout distribution in isolation. Unexported: not a public knob.
	consumerFactory consumerFactory

	// profileAccounts overrides the Telegram account resolver for profile
	// onboarding tests. Nil uses Store, matching production.
	profileAccounts profileAccountResolver
}

// Telegram is the Telegram channel: a telebot wrapper implementing
// channels.Channel. It builds a fresh AG-UI Fanout PER TURN (never one at channel
// start — research §1 / channel.go contract). The polling goroutine is tracked by
// a WaitGroup so Stop joins it (goleak-clean — the package goleak TestMain catches
// a leaked poller).
type Telegram struct {
	deps Deps

	mu      sync.Mutex
	bot     *tele.Bot
	wg      sync.WaitGroup
	started bool

	// Per-channel dispatch instances, built ONCE at Start from Deps (never per
	// message): cmds owns the /command intercept + the per-chat /cancel registry;
	// the media clients hold their HTTP clients; docs owns the async-convert wg that
	// Stop drains (goleak-clean). Guarded by mu; read under the handler goroutines.
	cmds    *commands
	onboard *onboarding
	profile *profileOnboarding
	voice   *voiceClient
	photo   *photoClient
	docs    *documentsClient
	tts     *ttsClient

	hitlRepliesHandled map[hitlReplyKey]struct{}

	// pausePrompts holds the last-rendered ask_user pause prompt message per chat so
	// a /cancel text command can clear its inline keyboard (M-e) — the "Annulla"
	// button path disarms via the callback's message, but a /cancel arrives as a
	// fresh message with no handle on the prompt. Set when a prompt is rendered,
	// consumed (and cleared) when the pause resolves. Guarded by mu.
	pausePrompts map[int64]*tele.Message

	// deliverBot / deliverResolver are unexported test seams for Deliver (Phase 20
	// R3): a recording botSender + a stubbed identity→account resolver so the deliver
	// branches are unit-tested with no live API/DB. nil (the production path) means
	// Deliver uses the live bot (read under mu) and t.deps.Store. Not public knobs.
	deliverBot      botSender
	deliverResolver accountResolver
}

type hitlReplyKey struct {
	chatID int64
	msgID  int
}

// NewChannel builds an unstarted Telegram channel over the supplied deps. (Named
// NewChannel, not New, because Store.New already owns the package's New for the DB
// seam — the channel is the higher-level type that holds a *Store.)
func NewChannel(d Deps) *Telegram {
	return &Telegram{deps: d}
}

// Name returns the channel name keying AURA_CHANNEL_TELEGRAM_ENABLED.
func (t *Telegram) Name() string { return channelName }

// statusThrottle / contentThrottle / chatRateLimit resolve the render throttles
// from the Deps, falling back to the PRD defaults when a caller left them zero
// (1500ms status pane / 500ms content / 1000ms per-chat queue).
func (t *Telegram) statusThrottle() time.Duration {
	return msOrDefault(t.deps.StatusThrottleMS, defaultStatusThrottleMS)
}

func (t *Telegram) contentThrottle() time.Duration {
	return msOrDefault(t.deps.ContentThrottleMS, defaultContentThrottleMS)
}

func (t *Telegram) chatRateLimit() time.Duration {
	return msOrDefault(t.deps.ChatRateLimitMS, defaultChatRateLimitMS)
}

// reasoningFIFORunes resolves the live-CoT window cap, applying the package default
// when the caller left it zero (mirrors the throttle accessors).
func (t *Telegram) reasoningFIFORunes() int {
	if t.deps.ReasoningFIFORunes <= 0 {
		return defaultReasoningFIFORunes
	}
	return t.deps.ReasoningFIFORunes
}

// msOrDefault converts a millisecond count to a Duration, applying fallback for a
// non-positive value (zero/negative → the PRD default).
func msOrDefault(ms, fallback int) time.Duration {
	if ms <= 0 {
		ms = fallback
	}
	return time.Duration(ms) * time.Millisecond
}

// Render throttle defaults (PRD §Slice 9 status-pane-B): mirror the config.go
// LoadConfig fallbacks so a channel built without an explicit throttle still
// coalesces correctly.
const (
	defaultStatusThrottleMS  = 1500
	defaultContentThrottleMS = 500
	defaultChatRateLimitMS   = 1000

	// defaultReasoningFIFORunes is the live-CoT window cap (spike: 4096-rune rolling
	// tail) applied when ReasoningFIFORunes is left zero.
	defaultReasoningFIFORunes = 4096
)

// Start constructs the telebot bot (a live getMe unless Offline), registers the
// text-message handler, and launches the polling goroutine. It returns once
// started (NOT per turn). A construction failure (bad token / getMe) returns an
// error the Registry fail-softs. Calling Start twice is a no-op after the first.
func (t *Telegram) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return nil
	}

	settings := tele.Settings{
		Token:   t.deps.Token,
		Offline: t.deps.Offline,
		// DisableKeepAlives on the poller's HTTP client so a getUpdates connection
		// does not linger in the idle pool past Stop (the openai_compat goleak
		// discipline — otherwise a kept-alive http2 conn outlives the poller).
		Client: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}},
	}
	if t.deps.Offline {
		// Offline (unit tests): Settings.Offline only skips the construction-time
		// getMe — Bot.Start still drives the default LongPoller against the live
		// API. Inject a no-op poller that blocks on stop and returns cleanly, so
		// Start/Stop is exercised end-to-end with zero network and goleak-clean.
		settings.Poller = stopWaitPoller{}
	}
	bot, err := tele.NewBot(settings)
	if err != nil {
		return fmt.Errorf("telegram start: construct bot: %w", err)
	}

	// Build the dispatch instances ONCE (not per message) so the documents async wg
	// is a single instance Stop drains, and the /cancel registry is shared across a
	// chat's turns.
	t.buildDispatch()
	t.registerHandlers(ctx, bot)
	t.registerBotCommands(bot)

	t.bot = bot
	t.started = true
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		// Start blocks consuming updates until Stop gracefully shuts the poller
		// down (telebot Bot.Stop closes the poller's stop channel).
		bot.Start()
	}()
	return nil
}

func (t *Telegram) registerBotCommands(bot *tele.Bot) {
	if t.deps.Offline {
		return
	}
	if err := bot.SetCommands(botMenuCommands()); err != nil {
		slog.Warn("telegram: set bot commands failed", "err", err)
	}
}

func botMenuCommands() []tele.Command {
	return []tele.Command{
		{Text: "start", Description: "Saluta o completa un link di setup"},
		{Text: "help", Description: "Mostra la lista dei comandi"},
		{Text: "cancel", Description: "Annulla il turno in corso"},
		{Text: "cost", Description: "Mostra la spesa cumulativa di oggi"},
		{Text: "search", Description: "Cerca nei turni salvati"},
		{Text: "onboard", Description: "Avvia o riavvia il profilo Agent.md"},
		{Text: "new", Description: "Spiega il thread continuo Telegram"},
		{Text: "list", Description: "Indica dove sfogliare le conversazioni"},
		{Text: "reset", Description: "Annulla il turno senza cancellare lo storico"},
		{Text: "clear", Description: "Cancella la conversazione corrente e ricomincia"},
		{Text: "whoami", Description: "Mostra l'identita collegata"},
		{Text: "stop", Description: "Annulla il turno; il bot resta attivo"},
	}
}

// Stop gracefully shuts the poller down and joins the polling goroutine. It is
// goleak-clean (Bot.Stop unblocks Bot.Start, the goroutine returns, the WaitGroup
// drains). Idempotent: a Stop on a never-started channel is a clean no-op.
func (t *Telegram) Stop(ctx context.Context) error {
	t.mu.Lock()
	bot := t.bot
	started := t.started
	t.mu.Unlock()

	if !started || bot == nil {
		return nil
	}
	bot.Stop()

	// Drain any in-flight async document-convert goroutines (goleak-clean — the
	// package TestMain catches a leaked convert goroutine). The poller is already
	// stopped, so no new conversion is accepted past this point.
	t.mu.Lock()
	docs := t.docs
	t.mu.Unlock()
	if docs != nil {
		docs.Stop(ctx)
	}
	t.wg.Wait()

	t.mu.Lock()
	t.started = false
	t.bot = nil
	t.mu.Unlock()
	return nil
}

// IsHealthy reports whether the bot is constructed and polling.
func (t *Telegram) IsHealthy() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.started && t.bot != nil
}

// sender returns the botSender the render consumers use. The live bot satisfies
// botSender; c.Bot() returns the tele.API the handler was invoked with (the same
// *tele.Bot), so the render path sends/edits through the real client.
func (t *Telegram) sender(c tele.Context) botSender {
	if b, ok := c.Bot().(botSender); ok {
		return b
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.bot
}

// stopWaitPoller is a Poller that performs zero network I/O: it blocks until the
// bot signals stop, then returns. Used in Offline (unit-test) mode so Bot.Start /
// Bot.Stop exercise the full start/join lifecycle without hitting the live API
// (the default LongPoller would otherwise issue getUpdates HTTP calls).
type stopWaitPoller struct{}

// Poll blocks on stop and returns when it is closed (the telebot Poller contract:
// listen for stop and exit promptly).
func (stopWaitPoller) Poll(_ *tele.Bot, _ chan tele.Update, stop chan struct{}) {
	<-stop
}

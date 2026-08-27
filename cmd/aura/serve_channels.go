// serve_channels.go is the channels-Registry + setup-server wiring for the
// `aura serve` daemon (Phase 13 / Slice 9, UX-02/03). It is split out of serve.go
// (refactor-on-touch, CLAUDE.md ≤600 LOC) and owns three things:
//
//   - bootChannelsAndSetup: builds the telegram channel (over the shared
//     composition root's Runner + pool), registers it in a channels.Registry, and
//     builds the loopback setup-wizard HTTP server (:9081) with a telebot getMe
//     BotProbe closure;
//   - startChannelSubsystems / stopChannelSubsystems: the fail-soft daemon
//     lifecycle, mirroring serve.go's AG-UI mount — StartAll the registry + run
//     the setup server in a log-but-never-exit goroutine; StopAll + Shutdown on
//     teardown, both BEFORE env.close();
//   - serveTelegramOverride: the --no-telegram / --only=cli flag parsing that
//     overrides the AURA_CHANNEL_TELEGRAM_ENABLED env gate (PRD Punto 1).
//
// Every subsystem is fail-soft: a failed channel Start or a taken setup port is
// logged and aggregated, never aborts the daemon (T-13-09-DaemonAbort).
package main

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"math"
	"net/http"
	"time"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/channels"
	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/onboarding"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/setup"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/jackc/pgx/v5/pgxpool"
)

// localIdentityName is the pre-Authula seeded identity. It remains only as a
// fallback for legacy databases that have no user identity yet.
const localIdentityName = "local"

// setupShutdownTimeout bounds the graceful drain of the setup server on daemon
// shutdown (mirrors aguiShutdownTimeout).
const setupShutdownTimeout = 10 * time.Second

// telegramGetMeTimeout bounds the provisioning-critical getMe probe used to discover the
// bot username. Telebot's default client timeout is longer than daemon boot should wait.
const telegramGetMeTimeout = 5 * time.Second

// bootChannelsAndSetup builds the channels Registry (with the Telegram channel
// registered) + the setup-wizard HTTP server over the shared composition root. It
// reads the Telegram channel config (TELEGRAM_BOT_TOKEN + the AURA_TELEGRAM_*
// throttles) from the environment, resolves the local identity for the setup
// onboarding FK, and applies the --no-telegram/--only=cli override. A nil/empty
// token leaves the channel registered but the registry enable gate keeps it from
// starting unless configured — the daemon still boots (fail-soft).
func bootChannelsAndSetup(ctx context.Context, chat *chatEnv, override func(name string) (enabled, ok bool)) (*channels.Registry, *http.Server) {
	tgCfg := telegram.LoadConfig()

	tg := telegram.NewChannel(buildTelegramDeps(chat, tgCfg))

	reg := channels.NewRegistry()
	reg.Register(tg)
	if override != nil {
		reg.SetEnabledOverride(override)
	}

	setupSrv := buildSetupServer(ctx, chat)
	return reg, setupSrv
}

func buildTelegramDeps(chat *chatEnv, tgCfg telegram.Config) telegram.Deps {
	return telegram.Deps{
		Turn:               ensuringTurn(chat.run),
		Token:              tgCfg.BotToken,
		Store:              telegram.New(chat.pool),
		Profile:            onboarding.NewProfileStore(chat.pool),
		Multimodal:         multimodalConfig(chat.cfg),
		Assets:             chat.assets,
		Search:             chat.conv,
		Cost:               newTodayCost(chat.pool),
		Spend:              &chat.cfg.LLM,
		Clear:              telegramClearAdapter{run: chat.run},
		Prices:             chat.cfg.LLM.Prices,
		Model:              chat.cfg.LLM.Model,
		Resume:             chat.run,
		Steer:              telegramSteerOrNil(chat.steer),
		StatusThrottleMS:   tgCfg.StatusThrottleMS,
		ContentThrottleMS:  tgCfg.ContentThrottleMS,
		ChatRateLimitMS:    tgCfg.ChatRateLimitMS,
		ShowReasoning:      chat.cfg.LLM.ShowReasoning,
		ReasoningFIFORunes: tgCfg.ReasoningFIFORunes,
	}
}

// telegramSteerOrNil converts a concrete *steer.PostgresStore into the narrower
// telegram.SteerPusher interface Deps.Steer needs, without the classic Go
// nil-interface trap: assigning a nil *steer.PostgresStore directly into an
// interface-typed field produces a NON-nil interface (type=*steer.PostgresStore,
// value=nil), so bot_dispatch_turn.go's `if t.deps.Steer == nil` guard would
// never fire on a disabled (AURA_AGUI_RUN_STEER=false) deployment — mirrors
// runner.SteerInboxOrNil (Phase 51 plan 02, D-06).
func telegramSteerOrNil(inbox *steer.PostgresStore) telegram.SteerPusher {
	if inbox == nil {
		return nil
	}
	return inbox
}

// multimodalConfig projects the central config.Config TTS knobs onto the
// telegram-package MultimodalConfig the outbound voice-note path reads. The cloud
// leg reuses the LLM client's OpenRouter base + key (the same credential the agent
// loop uses); the local leg reads the upstream-named sidecar URL. This is the
// serve-side mapper — it lives here (cmd/aura imports both packages) so the telegram
// package stays free of an internal/config import (the sidecar.go contract).
//
// The vision/STT/document knobs are deliberately absent: inbound Telegram media is
// ingested as assets, and internal/assets is wired with its own vision/STT config by
// buildAssetService. Projecting them here too would recreate the second pipeline
// this channel just stopped being.
func multimodalConfig(cfg *config.Config) telegram.MultimodalConfig {
	return telegram.MultimodalConfig{
		OpenRouterBaseURL: cfg.LLM.BaseURL,
		OpenRouterAPIKey:  cfg.LLM.APIKey,
		TTSBaseURL:        cfg.TTSBaseURL,
		TTSVoice:          cfg.TTSVoice,
		TTSFormat:         cfg.TTSFormat,
		TTSModel:          cfg.TTSModel,
		TimeoutSec:        cfg.MultimodalTimeoutSec,
	}
}

// todayCost satisfies telegram's costBackend over the cachemetrics daily
// aggregation. It sums the cache_metrics rows since local midnight and reports the
// provider-reported total cost as the authoritative figure (the OpenRouter
// usage.cost the runner already persisted), so /cost renders the SAME USD the CLI
// footer shows for the same window. completionTokens is not tracked separately in
// cache_metrics; the provider cost (not a token-rate recompute) is the source of
// truth, so it is returned non-nil and llm.CostUSD uses the provider-first path.
type todayCost struct {
	metrics *cachemetrics.Store
}

func newTodayCost(pool *pgxpool.Pool) *todayCost {
	return &todayCost{metrics: cachemetrics.New(pool)}
}

func (c *todayCost) TodayUsage(ctx context.Context) (promptTokens, completionTokens int, providerCost *float64, err error) {
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	agg, err := c.metrics.AggregateSince(ctx, midnight)
	if err != nil {
		return 0, 0, nil, err
	}
	cost := agg.TotalCostUSD
	return clampInt64ToInt(agg.TotalPromptTokens), 0, &cost, nil
}

// clampInt64ToInt narrows an int64 token count to int, saturating into the int
// range rather than truncating/sign-flipping when int is 32-bit (the aggregate's
// source decodes via strconv.ParseInt, so the value is attacker-influenced in
// principle even though real token sums are tiny). Negative sums clamp to 0.
func clampInt64ToInt(v int64) int {
	switch {
	case v < 0:
		return 0
	case v > math.MaxInt:
		return math.MaxInt
	default:
		return int(v)
	}
}

// ensuringTurn wraps Runner.Turn so the first inbound message for a chat lazily
// creates its conversation row before the loop appends to it. Telegram keys a
// stable conversation id off the chat id (a deterministic UUIDv5) and has no
// explicit "new conversation" step like the CLI REPL, so without this the first
// AppendTurn FK-fails. EnsureConversation is idempotent, so every later turn pays
// only a cheap existence Get. A create failure is yielded on the turn's error
// channel, which the translator emits as a RUN_ERROR; the Telegram renderer now
// surfaces that sanitized reason to the user (renderer.go RunErrorEvent case),
// exactly as any other Turn error.
// telegramClearAdapter routes the Telegram /clear hard-delete through the runner's single
// conversation-delete lifecycle (MUSR-05 / D-22) instead of a raw store delete, so /clear
// tears down the same live session state (pending pauses, session tools, background jobs) the
// AG-UI and CLI deletes do — no second deletion path bypasses the lifecycle. The owner
// identity is read from ctx (identityctx); the Telegram per-identity routing not yet threading
// a principal (deferred to the phase-12 cutover) resolves to `local`, matching the current
// chat ownership, and upgrades to the linked identity automatically once ctx carries it — no
// rework here. A not-owned/absent id (rows-affected==0) is a benign no-op: the /clear reply is
// idempotent and the next inbound message lazily re-creates the conversation.
type telegramClearAdapter struct{ run *runner.Runner }

func (a telegramClearAdapter) Delete(ctx context.Context, convID string) error {
	_, err := a.run.DeleteConversationLifecycle(ctx, identityctx.IdentityID(ctx), convID)
	return err
}

func ensuringTurn(run *runner.Runner) func(context.Context, string, *string) iter.Seq2[*agent.Event, error] {
	return func(ctx context.Context, convID string, userMsg *string) iter.Seq2[*agent.Event, error] {
		return func(yield func(*agent.Event, error) bool) {
			if err := run.EnsureConversation(ctx, convID); err != nil {
				yield(nil, err)
				return
			}
			run.Turn(ctx, convID, userMsg)(yield)
		}
	}
}

// buildSetupServer constructs the loopback setup-wizard HTTP server (:9081). The
// BotProbe is a telebot getMe closure (tele.NewBot does a live getMe; the
// username is bot.Me.Username) — it MUST NOT log the token. The IdentityID is the
// resolved `local` identity; if it cannot be resolved (a fresh DB before the seed
// is rare here) the setup server still builds with an empty IdentityID and the
// onboarding mint will surface the FK error to the operator, never crash the boot.
func buildSetupServer(ctx context.Context, chat *chatEnv) *http.Server {
	srv := setup.NewServer(setup.Deps{
		Store:      setupStoreAdapter{inner: telegram.New(chat.pool)},
		Probe:      telegramGetMeProbe,
		Bind:       chat.cfg.SetupBind,
		Token:      chat.cfg.SetupToken,
		IdentityID: resolveSetupIdentityID(ctx, chat.identity),
	})
	return srv.HTTPServer(chat.cfg.SetupBind)
}

// setupStoreAdapter bridges the *telegram.Store onto the setup.Store seam. The two
// interfaces are structurally identical but live in different packages (setup has
// no telegram import); this adapter does the trivial InsertPendingParams field
// copy at the composition root that imports both (the 13-09 adaptation the patterns
// map called out). The other two methods (PendingConsumed/CountAccounts) match the
// telegram.Store signatures verbatim, so they promote through the embedded value.
type setupStoreAdapter struct {
	inner *telegram.Store
}

var _ setup.Store = setupStoreAdapter{}

// InsertPending projects setup.InsertPendingParams onto telegram.InsertPendingParams
// (same fields) and delegates to the real Store.
func (a setupStoreAdapter) InsertPending(ctx context.Context, p setup.InsertPendingParams) error {
	return a.inner.InsertPending(ctx, telegram.InsertPendingParams{
		OnboardingToken: p.OnboardingToken,
		IdentityID:      p.IdentityID,
		GeneratedBy:     p.GeneratedBy,
		ExpiresAt:       p.ExpiresAt,
	})
}

// PendingConsumed delegates to the real Store (identical signature).
func (a setupStoreAdapter) PendingConsumed(ctx context.Context, onboardingToken string) (bool, error) {
	return a.inner.PendingConsumed(ctx, onboardingToken)
}

// CountAccounts delegates to the real Store (identical signature).
func (a setupStoreAdapter) CountAccounts(ctx context.Context) (int64, error) {
	return a.inner.CountAccounts(ctx)
}

// setupIdentityResolver is the identity-store surface the setup server needs to
// choose a real user owner before falling back to legacy `local`.
// rather than aborting the daemon — fail-soft).
type setupIdentityResolver interface {
	ListIdentities(ctx context.Context) ([]identity.Identity, error)
	GetIdentityByName(ctx context.Context, name string) (identity.Identity, error)
}

func resolveSetupIdentityID(ctx context.Context, idStore setupIdentityResolver) string {
	if idStore == nil {
		return ""
	}
	ids, err := idStore.ListIdentities(ctx)
	if err != nil {
		slog.Warn("aura serve: list identities for setup onboarding", "err", err)
	} else {
		for _, id := range ids {
			if id.Kind == "user" && id.ID != "" {
				return id.ID
			}
		}
	}
	id, err := idStore.GetIdentityByName(ctx, localIdentityName)
	if err != nil {
		if !errors.Is(err, identity.ErrIdentityNotFound) {
			slog.Warn("aura serve: resolve legacy local identity for setup onboarding", "err", err)
		}
		return ""
	}
	return id.ID
}

// telegramGetMeProbe validates a bot token via a live telebot getMe and returns
// the bot username. tele.NewBot performs the getMe at construction; on a bad
// token it returns an error. The token is a secret — it is never logged here
// (T-13-07-BotTokenLeak).
func telegramGetMeProbe(ctx context.Context, token string) (string, error) {
	client, cancel := telegramGetMeHTTPClient(ctx, nil)
	defer cancel()
	bot, err := tele.NewBot(tele.Settings{Token: token, Client: client})
	if err != nil {
		return "", err
	}
	return bot.Me.Username, nil
}

func telegramGetMeHTTPClient(ctx context.Context, base http.RoundTripper) (*http.Client, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, telegramGetMeTimeout)
	return &http.Client{
		Timeout:   telegramGetMeTimeout,
		Transport: contextRoundTripper{ctx: probeCtx, base: base},
	}, cancel
}

type contextRoundTripper struct {
	ctx  context.Context
	base http.RoundTripper
}

func (rt contextRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req.WithContext(rt.ctx))
}

// startChannelSubsystems starts the channels Registry + the setup HTTP server as
// fail-soft daemon siblings of the AG-UI gateway. StartAll's per-channel failures
// are aggregated + logged (never fatal); the setup server runs in a log-but-never-
// exit goroutine (mirrors serve.go's AG-UI ListenAndServe). One failed subsystem
// never aborts the daemon (T-13-09-DaemonAbort).
func startChannelSubsystems(ctx context.Context, reg *channels.Registry, setupSrv *http.Server) {
	if err := reg.StartAll(ctx); err != nil {
		// Already logged per-channel inside StartAll; this is the aggregate WARN so a
		// fully-failed registry is visible without aborting the daemon.
		slog.Warn("aura serve: one or more channels failed to start", "err", err)
	}
	if setupSrv != nil {
		slog.Info("aura serve: setup wizard http server listening", "addr", setupSrv.Addr)
		go func() {
			if err := setupSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("aura serve: setup http server stopped", "err", err)
			}
		}()
	}
}

// stopChannelSubsystems drains the channels Registry + the setup HTTP server on
// shutdown. StopAll joins every started channel (goleak-clean); the setup server
// gets a bounded graceful Shutdown. Both run BEFORE env.close() so the pool is
// still open while the channels drain. Idempotent: safe with nothing started.
func stopChannelSubsystems(ctx context.Context, reg *channels.Registry, setupSrv *http.Server) {
	if reg != nil {
		if err := reg.StopAll(ctx); err != nil {
			slog.Warn("aura serve: channel shutdown", "err", err)
		}
	}
	if setupSrv != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), setupShutdownTimeout)
		defer cancel()
		if err := setupSrv.Shutdown(shutCtx); err != nil {
			slog.Warn("aura serve: setup http server shutdown", "err", err)
		}
	}
}

// serveTelegramOverride parses the serve flags into a registry enable override.
// --no-telegram and --only=cli both disable the telegram channel (PRD Punto 1: a
// CLI-only daemon). A bare serve installs NO override (ok=false), leaving the
// AURA_CHANNEL_TELEGRAM_ENABLED env gate authoritative. The returned predicate
// matches the channels.Registry SetEnabledOverride contract: (enabled, ok) where
// ok=false defers to the env gate.
func serveTelegramOverride(args []string) (override func(name string) (enabled, ok bool), installed bool) {
	disableTelegram := false
	for _, a := range args {
		if a == "--no-telegram" || a == "--only=cli" {
			disableTelegram = true
		}
	}
	if !disableTelegram {
		return nil, false
	}
	return func(name string) (bool, bool) {
		if name == "telegram" {
			return false, true // explicitly disabled by the flag
		}
		return false, false // any other channel: defer to its env gate
	}, true
}

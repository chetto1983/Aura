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
	"log/slog"
	"net/http"
	"time"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/channels"
	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/setup"
)

// localIdentityName is the seeded `local` identity (migration 0004) the
// onboarding tokens + telegram accounts FK to. The setup server needs its UUID so
// a minted pending token references a real identity row.
const localIdentityName = "local"

// setupShutdownTimeout bounds the graceful drain of the setup server on daemon
// shutdown (mirrors aguiShutdownTimeout).
const setupShutdownTimeout = 10 * time.Second

// bootChannelsAndSetup builds the channels Registry (with the Telegram channel
// registered) + the setup-wizard HTTP server over the shared composition root. It
// reads the Telegram channel config (TELEGRAM_BOT_TOKEN + the AURA_TELEGRAM_*
// throttles) from the environment, resolves the local identity for the setup
// onboarding FK, and applies the --no-telegram/--only=cli override. A nil/empty
// token leaves the channel registered but the registry enable gate keeps it from
// starting unless configured — the daemon still boots (fail-soft).
func bootChannelsAndSetup(ctx context.Context, chat *chatEnv, override func(name string) (enabled, ok bool)) (*channels.Registry, *http.Server) {
	tgCfg := telegram.LoadConfig()

	tg := telegram.NewChannel(telegram.Deps{
		Turn:              chat.run.Turn,
		Token:             tgCfg.BotToken,
		Store:             telegram.New(chat.pool),
		StatusThrottleMS:  tgCfg.StatusThrottleMS,
		ContentThrottleMS: tgCfg.ContentThrottleMS,
		ChatRateLimitMS:   tgCfg.ChatRateLimitMS,
	})

	reg := channels.NewRegistry()
	reg.Register(tg)
	if override != nil {
		reg.SetEnabledOverride(override)
	}

	setupSrv := buildSetupServer(ctx, chat)
	return reg, setupSrv
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
		IdentityID: resolveLocalIdentityID(ctx, chat.identity),
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

// resolveLocalIdentityID looks up the seeded `local` identity UUID for the setup
// onboarding FK. A lookup failure is logged and returns "" (the setup server
// still boots; an onboarding mint then surfaces the FK error to the operator
// rather than aborting the daemon — fail-soft).
func resolveLocalIdentityID(ctx context.Context, idStore *identity.Store) string {
	id, err := idStore.GetIdentityByName(ctx, localIdentityName)
	if err != nil {
		slog.Warn("aura serve: resolve local identity for setup onboarding", "err", err)
		return ""
	}
	return id.ID
}

// telegramGetMeProbe validates a bot token via a live telebot getMe and returns
// the bot username. tele.NewBot performs the getMe at construction; on a bad
// token it returns an error. The token is a secret — it is never logged here
// (T-13-07-BotTokenLeak).
func telegramGetMeProbe(_ context.Context, token string) (string, error) {
	bot, err := tele.NewBot(tele.Settings{Token: token})
	if err != nil {
		return "", err
	}
	return bot.Me.Username, nil
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

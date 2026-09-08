// serve_env.go holds the booted `aura serve` daemon type split out of serve.go
// (refactor-on-touch, CLAUDE.md ≤600 LOC NO GOD CLASS): serveEnv's field set plus its
// close()/closeAuthulaProviders teardown pair. serve.go keeps runServe/bootServe — the
// boot/lifecycle logic that CONSTRUCTS a serveEnv; this file keeps the type itself and
// how it is torn down.
package main

import (
	"log/slog"
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/channels"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/readiness"
	"github.com/chetto1983/aura/internal/webauth"
)

// serveEnv is the booted daemon: the shared chat composition root plus the cron
// Store + Scheduler the tick loop runs. close() reverse-releases everything the boot
// acquired (MCP closers + pool) via the embedded chatEnv.
type serveEnv struct {
	*chatEnv
	store     *cron.Store
	scheduler *cron.Scheduler
	httpSrv   *http.Server // the AG-UI gateway (Slice 8b), mounted alongside the tick loop
	readiness *readiness.Snapshot
	// shellCompletions owns autonomous background-shell wake goroutines. It is
	// installed only when the shared steer rail is live and stops before shell
	// shutdown, so daemon termination never manufactures a fresh agent turn.
	shellCompletions *shellCompletionDispatcher

	// channels is the Phase-13 channels Registry (Telegram). It mounts as a
	// fail-soft daemon sibling of the AG-UI gateway; runServe StartAll/StopAll it.
	channels *channels.Registry
	// setupSrv is the loopback setup-wizard HTTP server (:9081, Slice 9a/UX-03), a
	// third http.Server sibling to httpSrv. runServe runs + Shutdowns it fail-soft.
	setupSrv *http.Server

	// sweeper is the periodic sidecar-sweep worker (audit M-06 part 2): in a long-
	// running daemon sidecars accumulate between reboots, so it re-runs the boot
	// ScanOrphans on AURA_RUN_DIR_SWEEP_INTERVAL_SEC. runServe Start/Stops it.
	sweeper *conversations.Sweeper
	// approvalExpirySweeper closes unanswered approval pauses through Runner's atomic
	// resume committer. It is owner-scoped and disabled when the configured TTL is <=0.
	approvalExpirySweeper *conversations.Sweeper
	// steerQueueSweeper expires due aura.steer_queue rows (D-07/D-08), writing a
	// readable conversation trace per expired row. Unscoped across identities (the
	// table carries no RLS, migration 0103); disabled when both configured TTLs are
	// <=0. runServe SweepNow/Starts it; drainShutdown Stops it.
	steerQueueSweeper *conversations.Sweeper

	// assetProcessingWorker claims durable asset_process ingestion jobs and runs the
	// shared asset processor pipeline. runServe Start/Stops it with the daemon.
	assetProcessingWorker *runtimeProcessingWorkers

	// delegationWorker claims durable swarm_delegation ingestion jobs (Phase 51,
	// SWARM-03/09) and runs each through the swarm engine's own runChild, pushing
	// the consolidated report via steer.SourceWorker. runServe Start/Stops it with
	// the daemon, same lifecycle as assetProcessingWorker.
	delegationWorker *runtimeProcessingWorkers

	// reconciler is the crash-orphan reconciler (D-01d / GATE-03 durability + GATE-04
	// recovery): it closes a start∧¬end reservation left by a crash by appending a
	// terminal indeterminate `end` fact, never re-invoking a mutating orphan. Its
	// lifecycle mirrors the sweeper's (Start at boot, Stop in drainShutdown, goleak-clean).
	reconciler *gateway.Reconciler

	// authulaProvider is the active embedded Authula web-auth framework.
	// onboardingAuthulaProvider is kept as a distinct slot so cleanup stays correct if a
	// future setup-only composition creates a separate provisioning provider.
	authulaProvider           *webauth.Provider
	onboardingAuthulaProvider *webauth.Provider

	// firstPartyGrants keeps a live OAuth grant for the sidecars Aura ships (calendar,
	// memory, whatsapp) so they mount without a human at a consent screen. Nil on a
	// deployment with no Authula authorization server. runServe EnsureNow/Starts it
	// before the deferred OAuth mounts reconnect; drainShutdown Stops it.
	firstPartyGrants *firstPartyGrantKeeper

	// runRegistry is the detached-run session registry (fix-plan 1.3 Tier B); nil
	// unless AURA_AGUI_RUN_DETACH=true. drainShutdown Closes it (cancel-walk every
	// detached run + reaper join) BEFORE the HTTP drain so cancelled producers flush
	// their terminal frames and attached SSE viewers close promptly.
	runRegistry *agui.RunRegistry
}

// close reverse-releases the daemon-owned resources the embedded chatEnv does not:
// the Authula provider's cleanup workers (when wired), THEN the shared chatEnv close
// (pool + MCP closers). It is the single teardown the deferred env.close() in runServe
// drives.
func (e *serveEnv) close() {
	closeAuthulaProviders(e.authulaProvider, e.onboardingAuthulaProvider)
	e.chatEnv.close()
}

func closeAuthulaProviders(active, onboarding *webauth.Provider) {
	if onboarding != nil && onboarding != active {
		if err := onboarding.Close(); err != nil {
			slog.Warn("aura serve: onboarding authula provider close", "err", err)
		}
	}
	if active != nil {
		if err := active.Close(); err != nil {
			slog.Warn("aura serve: authula provider close", "err", err)
		}
	}
}

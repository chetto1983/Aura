package main

import (
	"context"
	"log/slog"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// serve_bootstrap_resources.go carries the first operator's eager resource legs, split out
// of serve_bootstrap.go on touch and shaped after its sibling in the provisioning saga
// (internal/agui/onboarding_provision_resources.go) — the SAME ports, wired from the SAME
// adapters at the composition root, so the two paths cannot drift into two answers about
// what an identity owns.
//
// Why the first operator needed its own copy at all: the saga provisions ADDITIONAL
// identities and refuses to run before one exists (it re-checks identity.create on the
// creator, and MUSR isolation gates it), so the bootstrap flow has always been separate —
// an advisory-locked transaction that runs when Authula has no users at all. What it was
// missing is everything past memory.
//
// Measured on this deployment 2026-08-31, on the identity the setup wizard had created:
//
//	aura.provisioning_saga      -> 0 rows
//	aura.identity_object_store  -> 0 rows
//	aura-ingest: WARN identity has no usable Garage binding … no rows in result set
//
// With no bucket the ingest sidecar has nothing to reconcile, so services/ingest's
// ensure_schema never runs, IndexedDocument is never created, and the document library
// cannot work on a fresh install — not because anything failed, but because nobody ever
// asked for the bucket.
//
// Every leg here is FAIL-SOFT and LOUD. The account is real and usable without any of
// them, and rolling a first operator back because Garage hiccuped would be the worse
// answer — the same call provisionFirstPartyGrants already makes. But each one is the
// difference between a working capability and one that silently never appears, so each
// failure names the capability the operator has just lost.

// sandboxStarter starts one identity's box. It is a func seam rather than an interface
// because the composition root is the only caller and the only implementation is a closure
// over the live SandboxRouter.
type sandboxStarter func(ctx context.Context, identityID string) error

// mcpReconnect re-runs the deferred-OAuth mount pass.
type mcpReconnect func(ctx context.Context)

// newSandboxStarter adapts the router's own get-or-create seam. Route is idempotent and
// already does everything a box needs — resolve-or-create, resume a suspended box,
// materialize the resolver's sources, attach the egress sidecar — so
// starting a box eagerly is the SAME call every tool call makes, with the identity bound
// on the context rather than read off a request. A nil router yields no leg.
func newSandboxStarter(router *usersandbox.SandboxRouter) sandboxStarter {
	if router == nil {
		return nil
	}
	return func(ctx context.Context, identityID string) error {
		_, err := router.Route(identityctx.WithIdentityID(ctx, identityID))
		return err
	}
}

// bootstrapResources is the first operator's leg set, nil-safe field by field: a
// deployment with no Garage admin, no live MCP mount or no sandbox router simply runs the
// legs it has.
type bootstrapResources struct {
	objectStore agui.ObjectStoreProvisioner
	filesystem  agui.FilesystemProvisioner
	sandbox     sandboxStarter
	remountMCP  mcpReconnect
}

// provision runs the legs in dependency order and never fails the caller. The box leg comes
// after the filesystem one because a box materializes host dirs at create; today's resolver
// reads the deployment-global skills export dir rather than the per-identity roots, so that
// order is defensive rather than load-bearing for it (amendment #206). The MCP remount is
// last for a reason the code below states, and that one IS load-bearing.
func (r bootstrapResources) provision(ctx context.Context, identityID string) {
	if r.objectStore != nil {
		if err := r.objectStore.ProvisionObjectStore(ctx, identityID); err != nil {
			slog.Error("aura serve: bootstrap object store failed — this operator has NO bucket, so nothing can be ingested and the document library will stay empty",
				"err", err)
		}
	}
	if r.filesystem != nil {
		if err := r.filesystem.ProvisionIdentityDirs(ctx, identityID); err != nil {
			slog.Error("aura serve: bootstrap filesystem roots failed — this operator has no skills, mcp or pyscripts directory",
				"err", err)
		}
	}
	if r.sandbox != nil {
		if err := r.sandbox(ctx, identityID); err != nil {
			// Not fatal: Route is the same call every tool makes, so the next one retries.
			// Eager start is here to surface the failure NOW, in the operator's own
			// account creation, instead of inside a tool result the model has to explain.
			slog.Error("aura serve: bootstrap sandbox box did not start — every tool call will be denied until it does",
				"err", err)
		}
	}
	// LAST, and only after the grants minted earlier in provisionTenantMemory exist.
	// StartReconnect resolves each deferred OAuth server's owner from the grant store, and
	// at boot there was no human identity to own one — this identity is the first. Without
	// this call the pass stays the one-shot it makes at startup and the sidecars Aura
	// ships stay unmounted until someone restarts the daemon.
	//
	// Measured 2026-08-31: the daemon booted at 16:18:22, the wizard created this identity
	// at 16:18:35.69, its grants landed at 16:18:35.97 — and calendar, memory and whatsapp
	// stayed out of the agent registry for the next 39 minutes, through a live turn in
	// which the agent asked tool_search for memory_recall three times and was told each
	// time that it is not a registered tool.
	//
	// It gets the request's context with cancellation DETACHED, the same idiom the memory
	// compensation above uses and for the same reason: StartReconnect spawns a goroutine
	// and returns, so this leg is still shaking hands with three sidecars when the wizard's
	// response is written and ctx is cancelled under it. Measured 2026-09-02 on a fresh
	// install: grants at 08:25:26, all three mounts dead at 08:25:27 on "context canceled"
	// with the 40-attempt retry never running, and the daemon stuck on
	// {"ready":false,"reasons":["memory_unavailable"]} until it was restarted by hand.
	// The VALUES have to survive -- Mount reads the identity and the OAuth handle off this
	// context -- so it is WithoutCancel, not a fresh Background.
	if r.remountMCP != nil {
		r.remountMCP(context.WithoutCancel(ctx))
	}
}

package agui

import (
	"context"
	"log/slog"
)

// onboarding_provision_resources.go carries the eager per-identity resource legs the
// Phase-36 provisioning saga fans out to (MUSR-06 / D-08 / D-20/D-21), split out of
// onboarding_provision.go on touch (RESEARCH Pitfall 7, LOC ceiling). They are narrow
// consumer-side ports so the agui package stays free of the objectstore/garageadmin +
// mcp/skills concretes (the composition root wires the adapters; unit tests inject fakes
// or nil). The legs are idempotent so a journaled re-run after a crash converges, and
// each exposes symmetric compensation.

// MemoryProvisioner eagerly creates and erases one identity's ArcadeDB database,
// server credential, and memory schema. It embeds the de-provisioning port so tenant
// creation compensation and owner erasure use the same boundary.
type MemoryProvisioner interface {
	MemoryPurger
	ProvisionMemory(ctx context.Context, identityID string) error
}

// ObjectStoreProvisioner provisions and de-provisions a per-identity Garage bucket +
// scoped key (D-08). ProvisionObjectStore does CreateBucket + CreateKey + AllowBucketKey
// and persists the encrypted secret (plan-06 identity_store); DeprovisionObjectStore is
// the symmetric DeleteBucket + DeleteKey + drop-row. Both are idempotent so the saga (and
// its de-provision mirror) can re-run.
type ObjectStoreProvisioner interface {
	ProvisionObjectStore(ctx context.Context, identityID string) error
	DeprovisionObjectStore(ctx context.Context, identityID string) error
}

// FilesystemProvisioner provisions and de-provisions the per-identity filesystem roots
// (D-20/D-21): ~/.aura/mcp/{id}, $AURA_SKILLS_DIR/{id}, ~/.aura/pyscripts/{id}.
// ProvisionIdentityDirs is idempotent (MkdirAll); DeprovisionIdentityDirs is RemoveAll
// (idempotent). The adapter roots every path through the traversal guard so a crafted
// identity cannot escape its provisioning dir.
type FilesystemProvisioner interface {
	ProvisionIdentityDirs(ctx context.Context, identityID string) error
	DeprovisionIdentityDirs(ctx context.Context, identityID string) error
}

// SandboxProvisioner eagerly creates one identity's per-identity sandbox box (D-09): the
// container, its egress sidecar, and the workspace volume. It embeds the de-provisioning
// port (SandboxPurger, deprovision.go) so the provisioning leg and the deprovision saga's
// teardown leg share one destroy contract — a box EnsureBox created is torn down by the
// same DestroySandbox the deprovision saga already calls. ProvisionSandbox is idempotent
// (SandboxRouter.EnsureBox is a get-or-create seam), so a journaled re-run after a crash
// converges instead of double-creating.
type SandboxProvisioner interface {
	SandboxPurger
	ProvisionSandbox(ctx context.Context, identityID string) error
}

// provisionResourceLegs runs the ArcadeDB + Garage + filesystem + sandbox legs for the
// freshly-created identity, journaled and idempotent. It returns a compensation that
// reverses all legs (idempotent) for the caller to invoke if a LATER leg (Telegram /
// audit) fails. On its OWN failure it compensates the partial work it did (so the caller
// only compensates the earlier legs) and returns the error. Nil optional ports skip their
// leg; Provision's preflight rejects a nil memory port before any cross-store write.
func (s *onboardingService) provisionResourceLegs(ctx context.Context, run *sagaRun, identityID string) (compResources func(), err error) {
	// Compensation reverses whatever this call provisioned, in REVERSE order, best-effort
	// on a cancel-immune context (the request ctx may already be cancelled on failure).
	// Sandbox is FIRST here because it is provisioned LAST below (D-09): the box is the
	// only LIVE COMPUTE this identity owns, and a container that can still write into the
	// very filesystem roots the next block removes is how an orphan gets made.
	compResources = func() {
		cctx := context.WithoutCancel(ctx)
		if s.sandbox != nil {
			if derr := s.sandbox.DestroySandbox(cctx, identityID); derr != nil {
				slog.Error("onboarding: COMP sandbox (destroy box) failed", "step", "compensate")
			}
		}
		if s.filesystem != nil {
			if derr := s.filesystem.DeprovisionIdentityDirs(cctx, identityID); derr != nil {
				slog.Error("onboarding: COMP filesystem (remove identity dirs) failed", "step", "compensate")
			}
		}
		if s.objectStore != nil {
			if derr := s.objectStore.DeprovisionObjectStore(cctx, identityID); derr != nil {
				slog.Error("onboarding: COMP object-store (delete bucket+key) failed", "step", "compensate")
			}
		}
		if s.memory != nil {
			if derr := s.memory.PurgeMemory(cctx, identityID); derr != nil {
				slog.Error("onboarding: COMP memory (drop database+credential) failed", "step", "compensate")
			}
		}
	}

	if s.memory != nil {
		if err := run.step(ctx, sagaStepMemory, func(ctx context.Context) error {
			return s.memory.ProvisionMemory(ctx, identityID)
		}); err != nil {
			if derr := s.memory.PurgeMemory(context.WithoutCancel(ctx), identityID); derr != nil {
				slog.Error("onboarding: COMP memory after provision failure failed", "step", "compensate")
			}
			return compResources, provisionFail("memory provision", err)
		}
	}

	if s.objectStore != nil {
		if err := run.step(ctx, sagaStepGarage, func(ctx context.Context) error {
			return s.objectStore.ProvisionObjectStore(ctx, identityID)
		}); err != nil {
			// Undo the bucket/key this call may have partially created.
			if derr := s.objectStore.DeprovisionObjectStore(context.WithoutCancel(ctx), identityID); derr != nil {
				slog.Error("onboarding: COMP object-store after provision failure failed", "step", "compensate")
			}
			return compResources, provisionFail("object store provision", err)
		}
	}

	if s.filesystem != nil {
		if err := run.step(ctx, sagaStepFilesystem, func(ctx context.Context) error {
			return s.filesystem.ProvisionIdentityDirs(ctx, identityID)
		}); err != nil {
			// Undo the filesystem roots + the object store already provisioned in this call.
			compResources()
			return compResources, provisionFail("filesystem provision", err)
		}
	}

	// Sandbox is LAST (D-09): the box is eager, idempotent (SandboxRouter.EnsureBox is a
	// get-or-create seam) and compensated symmetrically with SandboxPurger.DestroySandbox,
	// which the deprovision saga already calls (deprovision.go). On its OWN failure it
	// self-destroys only — mirroring the memory/objectStore legs above, not filesystem's
	// full compResources() — and returns compResources so the CALLER compensates the
	// earlier legs.
	if s.sandbox != nil {
		if err := run.step(ctx, sagaStepSandbox, func(ctx context.Context) error {
			return s.sandbox.ProvisionSandbox(ctx, identityID)
		}); err != nil {
			if derr := s.sandbox.DestroySandbox(context.WithoutCancel(ctx), identityID); derr != nil {
				slog.Error("onboarding: COMP sandbox after provision failure failed", "step", "compensate")
			}
			return compResources, provisionFail("sandbox provision", err)
		}
	}

	return compResources, nil
}

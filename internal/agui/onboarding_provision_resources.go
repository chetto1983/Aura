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

// provisionResourceLegs runs the ArcadeDB + Garage + filesystem legs for the freshly-created
// identity, journaled and idempotent. It returns a compensation that reverses all legs
// (idempotent) for the caller to invoke if a LATER leg (Telegram / audit) fails. On its
// OWN failure it compensates the partial work it did (so the caller only compensates the
// earlier legs) and returns the error. Nil optional ports skip their leg; Provision's
// preflight rejects a nil memory port before any cross-store write.
func (s *onboardingService) provisionResourceLegs(ctx context.Context, run *sagaRun, identityID string) (compResources func(), err error) {
	// Compensation reverses whatever this call provisioned, in reverse order, best-effort
	// on a cancel-immune context (the request ctx may already be cancelled on failure).
	compResources = func() {
		cctx := context.WithoutCancel(ctx)
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

	return compResources, nil
}

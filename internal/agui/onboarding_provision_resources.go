package agui

import (
	"context"
	"log/slog"
)

// onboarding_provision_resources.go carries the two eager per-identity resource legs the
// Phase-36 provisioning saga fans out to (MUSR-06 / D-08 / D-20/D-21), split out of
// onboarding_provision.go on touch (RESEARCH Pitfall 7, LOC ceiling). Both are narrow
// consumer-side ports so the agui package stays free of the objectstore/garageadmin +
// mcp/skills concretes (the composition root wires the adapters; unit tests inject fakes
// or nil). Both legs are idempotent (Garage create-by-alias, INSERT ON CONFLICT, MkdirAll,
// write-if-absent) so a journaled re-run after a crash converges, and both expose a
// symmetric compensation (DeleteBucket+DeleteKey / RemoveAll).

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
// (D-20/D-21): ~/.aura/mcp/{id}, $AURA_SKILLS_DIR/{id}, ~/.aura/pyscripts/{id}, and an
// empty Agent.md. ProvisionIdentityDirs is idempotent (MkdirAll + write-if-absent);
// DeprovisionIdentityDirs is RemoveAll (idempotent). The adapter roots every path through
// the profile traversal guard so a crafted identity cannot escape its provisioning dir.
type FilesystemProvisioner interface {
	ProvisionIdentityDirs(ctx context.Context, identityID string) error
	DeprovisionIdentityDirs(ctx context.Context, identityID string) error
}

// provisionResourceLegs runs the Garage + filesystem legs for the freshly-created
// identity, journaled and idempotent. It returns a compensation that reverses BOTH legs
// (idempotent) for the caller to invoke if a LATER leg (Telegram / audit) fails. On its
// OWN failure it compensates the partial work it did (so the caller only compensates the
// earlier legs) and returns the error. Unwired ports (nil) skip that leg — a seed-form-
// only or not-yet-cutover deployment provisions no resources (backward compatible).
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

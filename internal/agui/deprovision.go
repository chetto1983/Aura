package agui

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// deprovision.go is the symmetric de-provisioning saga (Phase 36 D-27), the reverse of the
// provisioning saga in onboarding_provision.go. Soft-delete is two-phase: Deactivate is
// IMMEDIATE (block login by killing the Authula sessions, terminate the identity's
// background jobs, stamp deactivated_at + a grace-window purge_after) and retains the data
// for a grace window; Purge runs AFTER the grace window and tears down every plane the
// provisioning saga built, in reverse order. Every step is journaled on
// aura.provisioning_saga with kind='deprovision' (migration 0028) and is idempotent
// (Delete/Deny by-id 404=success, RemoveAll, FK-cascade delete), so an interrupted purge
// re-runs and converges to a fully-removed identity with NO orphaned Authula user, Garage
// bucket, :User graph edge, or per-identity directory (T-36-08-I).
//
// Like the provisioning saga, every teardown is a narrow consumer-side port so the agui
// package stays free of the concrete stores; the composition root wires the adapters and
// unit tests inject fakes. A nil port skips its plane (an unwired plane is a no-op) — used
// by the pre-cutover path and the unit tests.

// defaultDeprovisionGrace is the soft-delete retention window (D-27): after Deactivate the
// identity's data is retained this long before Purge may hard-delete it. Overridable via
// DeprovisionDeps.GraceWindow.
const defaultDeprovisionGrace = 7 * 24 * time.Hour

// DeprovisionTarget identifies one identity to tear down. IdentityID keys every per-identity
// plane; IdentityName is what the aura identity delete cascades on; AuthulaUserID is the
// Authula user to remove + whose sessions to kill. The grace-window scan (ListPurgeable)
// returns fully-populated targets.
type DeprovisionTarget struct {
	IdentityID    string
	IdentityName  string
	AuthulaUserID string
}

// IdentityDeactivator owns the aura.identities soft-delete columns (0029): MarkDeactivated
// stamps deactivated_at=now + purge_after=graceEnd (idempotent); ListPurgeable returns the
// targets whose purge_after has elapsed (deactivated AND past grace); ResolveTarget hydrates
// one target (name + authula user) for an identity id.
type IdentityDeactivator interface {
	MarkDeactivated(ctx context.Context, identityID string, purgeAfter time.Time) error
	ListPurgeable(ctx context.Context, now time.Time) ([]DeprovisionTarget, error)
	ResolveTarget(ctx context.Context, identityID string) (DeprovisionTarget, error)
}

// SessionTerminator blocks login immediately by deleting every Authula session for the
// user (Authula SessionService.DeleteAllByUserID). Idempotent.
type SessionTerminator interface {
	KillSessions(ctx context.Context, authulaUserID string) error
}

// JobTerminator terminates the identity's in-flight background jobs (the plan-03
// owner-scoped kill). Idempotent — terminating an already-stopped job is a no-op.
type JobTerminator interface {
	TerminateJobs(ctx context.Context, identityID string) error
}

// ConversationPurger deletes the identity's conversations + turns (owner-scoped). Idempotent.
type ConversationPurger interface {
	PurgeConversations(ctx context.Context, identityID string) error
}

// MemoryPurger erases the identity's long-term memory, which is one ArcadeDB database and
// one server user (internal/arcadedb/tenant.go). Idempotent: it asserts the postcondition
// (the database is gone, the credential is refused) rather than the drop's exit code, so a
// re-run over an already-purged identity converges instead of failing forever.
type MemoryPurger interface {
	PurgeMemory(ctx context.Context, identityID string) error
}

// SandboxPurger destroys the identity's per-identity box: the container, its egress
// sidecar, and the workspace volume. Idempotent -- destroying an absent box converges,
// because the backend does not raise removing something that is already gone.
//
// It is the box's ONLY teardown. Nothing else sweeps one: the idle reaper suspends and
// retains by design, and the container name is derived from the identity id, so a box
// outliving its identity is an orphan no later run can attribute to anyone.
type SandboxPurger interface {
	DestroySandbox(ctx context.Context, identityID string) error
}

// IdentityDeleter hard-deletes the aura identity by name, cascading capability_grants, the
// identity_auth_link, and the identity_object_store row via FK ON DELETE CASCADE. Idempotent
// (deleting an absent identity affects zero rows). It is the AuraLegWriter.DeleteIdentity
// leg reused as the reverse of Leg A.
type IdentityDeleter interface {
	DeleteIdentity(ctx context.Context, identityName string) error
}

// AuthulaUserDeleter removes the Authula user (the account cascades) — the reverse of Leg B.
// Idempotent.
type AuthulaUserDeleter interface {
	DeleteUser(ctx context.Context, authulaUserID string) error
}

// DeprovisionDeps bundles the reverse-leg ports + the journal + the grace window. Every
// port is optional (nil skips that plane) so the composition root can wire the planes it
// has and unit tests inject a subset.
type DeprovisionDeps struct {
	Journal        SagaJournal
	Deactivator    IdentityDeactivator
	Sessions       SessionTerminator
	Jobs           JobTerminator
	Conversations  ConversationPurger
	Memory         MemoryPurger
	ObjectStore    ObjectStoreProvisioner
	Filesystem     FilesystemProvisioner
	Sandbox        SandboxPurger
	IdentityDelete IdentityDeleter
	AuthulaDelete  AuthulaUserDeleter
	GraceWindow    time.Duration
}

// Deprovisioner runs the two-phase soft-delete + purge saga. It is safe for concurrent use
// across identities (each call is keyed on its own identity id / saga id).
type Deprovisioner struct {
	deps DeprovisionDeps
}

// NewDeprovisioner assembles the saga over the supplied reverse-leg ports.
func NewDeprovisioner(d DeprovisionDeps) *Deprovisioner {
	return &Deprovisioner{deps: d}
}

func (d *Deprovisioner) graceWindow() time.Duration {
	if d.deps.GraceWindow > 0 {
		return d.deps.GraceWindow
	}
	return defaultDeprovisionGrace
}

// Deactivate is the IMMEDIATE soft-delete step (D-27): it stamps deactivated_at + a
// grace-window purge_after, kills the Authula sessions (blocking login), and terminates the
// identity's background jobs. Journaled (kind=deprovision, step=deactivate) and idempotent.
// It resolves the target (for the Authula user id) from the identity id.
func (d *Deprovisioner) Deactivate(ctx context.Context, identityID string) error {
	target, err := d.resolve(ctx, identityID)
	if err != nil {
		return err
	}
	run := newSagaRun(ctx, d.deps.Journal, sagaKindDeprovision, identityID)
	purgeAfter := time.Now().UTC().Add(d.graceWindow())
	return run.step(ctx, sagaStepDeactivate, func(ctx context.Context) error {
		if d.deps.Deactivator != nil {
			if err := d.deps.Deactivator.MarkDeactivated(ctx, identityID, purgeAfter); err != nil {
				return err
			}
		}
		if d.deps.Sessions != nil && target.AuthulaUserID != "" {
			if err := d.deps.Sessions.KillSessions(ctx, target.AuthulaUserID); err != nil {
				return err
			}
		}
		if d.deps.Jobs != nil {
			if err := d.deps.Jobs.TerminateJobs(ctx, identityID); err != nil {
				return err
			}
		}
		return nil
	})
}

// Purge runs the symmetric reverse saga after the grace window: conversations, then the
// memory database, then the Garage bucket+key, then the
// per-identity dirs, then the aura identity (cascading grants + link + object-store row),
// then the Authula user. Journaled
// (kind=deprovision), idempotent, and resumable — a re-run skips the steps the journal
// marks done and re-runs the rest until the identity is fully removed with no orphans.
func (d *Deprovisioner) Purge(ctx context.Context, target DeprovisionTarget) error {
	if d.deps.IdentityDelete != nil && target.IdentityName != "" {
		switch {
		case d.deps.Conversations == nil:
			return fmt.Errorf("deprovision preflight: conversation purger is required before identity deletion")
		// An unwired memory purger is worse than a failing one: deleting the identity row
		// cascades the whole Postgres catalog, and the ArcadeDB database that outlives it
		// has no owner row left to find it by. Nothing would ever sweep it.
		case d.deps.Memory == nil:
			return fmt.Errorf("deprovision preflight: memory purger is required before identity deletion")
		}
	}
	run := newSagaRun(ctx, d.deps.Journal, sagaKindDeprovision, target.IdentityID)

	// FIRST, ahead of every data plane. The box is the only LIVE COMPUTE this identity
	// owns: it holds a shell that can still write to the workspace volume, and through the
	// materialized mounts to the very filesystem roots the dirs step removes below. Erasing
	// a plane while something can still write to it is how an orphan is made.
	if d.deps.Sandbox != nil {
		if err := run.step(ctx, sagaStepSandbox, func(ctx context.Context) error {
			return d.deps.Sandbox.DestroySandbox(ctx, target.IdentityID)
		}); err != nil {
			return err
		}
	}
	if d.deps.Conversations != nil {
		if err := run.step(ctx, sagaStepConversations, func(ctx context.Context) error {
			return d.deps.Conversations.PurgeConversations(ctx, target.IdentityID)
		}); err != nil {
			return err
		}
	}
	if d.deps.Memory != nil {
		if err := run.step(ctx, sagaStepMemory, func(ctx context.Context) error {
			return d.deps.Memory.PurgeMemory(ctx, target.IdentityID)
		}); err != nil {
			return err
		}
	}
	if d.deps.ObjectStore != nil {
		if err := run.step(ctx, sagaStepObjectStore, func(ctx context.Context) error {
			return d.deps.ObjectStore.DeprovisionObjectStore(ctx, target.IdentityID)
		}); err != nil {
			return err
		}
	}
	if d.deps.Filesystem != nil {
		if err := run.step(ctx, sagaStepDirs, func(ctx context.Context) error {
			return d.deps.Filesystem.DeprovisionIdentityDirs(ctx, target.IdentityID)
		}); err != nil {
			return err
		}
	}
	// The aura identity delete cascades capability_grants + identity_auth_link +
	// identity_object_store row (FK ON DELETE CASCADE). Skipped when the target name is
	// unknown (a target hydrated only for a resource-plane purge).
	if d.deps.IdentityDelete != nil && target.IdentityName != "" {
		if err := run.step(ctx, sagaStepIdentityRow, func(ctx context.Context) error {
			return d.deps.IdentityDelete.DeleteIdentity(ctx, target.IdentityName)
		}); err != nil {
			return err
		}
	}
	if d.deps.AuthulaDelete != nil && target.AuthulaUserID != "" {
		if err := run.step(ctx, sagaStepAuthula, func(ctx context.Context) error {
			return d.deps.AuthulaDelete.DeleteUser(ctx, target.AuthulaUserID)
		}); err != nil {
			return err
		}
	}
	return nil
}

// PurgeOne resolves an identity id to a full target and purges it. It is the forced-purge
// entry (an admin "purge now" or a test) that bypasses the grace-window scan.
func (d *Deprovisioner) PurgeOne(ctx context.Context, identityID string) error {
	target, err := d.resolve(ctx, identityID)
	if err != nil {
		return err
	}
	return d.Purge(ctx, target)
}

// PurgeExpired scans for identities past their grace window and purges each. It is the
// scheduled entry the cron identity-purge handler drives. A per-identity failure is logged
// and skipped (the batch continues); the next scan retries it (resumable). It returns the
// number successfully purged.
func (d *Deprovisioner) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	if d.deps.Deactivator == nil {
		return 0, nil
	}
	targets, err := d.deps.Deactivator.ListPurgeable(ctx, now)
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, t := range targets {
		if err := d.Purge(ctx, t); err != nil {
			slog.Error("deprovision: purge identity failed (will retry next scan)", "step", "purge")
			continue
		}
		purged++
	}
	return purged, nil
}

// resolve hydrates a full target from an identity id via the Deactivator, falling back to
// an id-only target when no Deactivator is wired (the resource-plane-only path).
func (d *Deprovisioner) resolve(ctx context.Context, identityID string) (DeprovisionTarget, error) {
	if d.deps.Deactivator == nil {
		return DeprovisionTarget{IdentityID: identityID}, nil
	}
	return d.deps.Deactivator.ResolveTarget(ctx, identityID)
}

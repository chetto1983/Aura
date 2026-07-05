package agui

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// saga_journal.go adds forward-recovery journaling to the cross-store provisioning /
// de-provisioning saga (Phase 36 D-14/D-27). The shipped Provision saga is compensation-
// based (a leg failure rolls back the earlier legs in-process) but a CRASH mid-flight
// leaves partial state with no in-process compensator to run. The journal
// (aura.provisioning_saga, migration 0028 — MUTABLE: a step transitions pending -> done/
// failed) is the durable record a re-run reads to converge: every step is idempotent
// (Garage create-by-alias, INSERT ON CONFLICT, MkdirAll, write-if-absent), so a re-run
// skips the steps the journal marks done and re-executes the rest to the SAME terminal
// state.
//
// Journaling is a narrow consumer-side port (SagaJournal) so the agui package stays free
// of the pgx pool (the composition root wires the concrete adapter; unit tests inject a
// fake or nil). It is best-effort: a journal write failure is logged, never fatal — the
// compensation path still protects the in-process failure, and idempotent steps make a
// stale journal safe on re-run.

// Saga kinds (aura.provisioning_saga.kind CHECK) and step statuses (status CHECK).
const (
	sagaKindProvision   = "provision"
	sagaKindDeprovision = "deprovision"

	sagaPending = "pending"
	sagaDone    = "done"
	sagaFailed  = "failed"
)

// Saga step labels (aura.provisioning_saga.step). Provisioning journals every step from
// the point the identity row exists (aura leg onward); de-provisioning journals its
// symmetric reverse legs. The Authula-user leg precedes the identity id (the journal's
// identity_id is NOT NULL) so it is not journaled — a crash before the aura leg leaves
// only an Authula user the duplicate-email pre-check reconciles on retry.
const (
	sagaStepIdentity      = "identity"
	sagaStepRecovery      = "recovery"
	sagaStepGarage        = "garage"
	sagaStepFilesystem    = "filesystem"
	sagaStepTelegram      = "telegram"
	sagaStepFirstLogin    = "first_login"
	sagaStepAudit         = "audit"
	sagaStepDeactivate    = "deactivate"
	sagaStepConversations = "conversations"
	sagaStepGraph         = "graph"
	sagaStepObjectStore   = "objectstore"
	sagaStepDirs          = "dirs"
	sagaStepIdentityRow   = "identity_row"
	sagaStepAuthula       = "authula"
)

// sagaNamespace is the fixed UUIDv5 namespace deriving a deterministic saga_id from an
// identity id + kind, so a re-run for the same identity reuses the same saga_id and the
// journal rows collide on the (saga_id, step) primary key (idempotent upsert). Provision
// and de-provision get distinct saga ids (kind is folded into the derivation) so their
// step journals never collide.
var sagaNamespace = uuid.MustParse("36080000-0000-4000-8000-000000000001")

// sagaID derives the deterministic saga_id for (kind, identityID).
func sagaID(kind, identityID string) string {
	return uuid.NewSHA1(sagaNamespace, []byte(kind+":"+identityID)).String()
}

// SagaJournal is the narrow consumer-side port over aura.provisioning_saga. The
// composition-root adapter implements it over the aura pool; a nil journal disables
// journaling (the saga still runs, protected by compensation + idempotency). All methods
// are best-effort from the saga's perspective.
type SagaJournal interface {
	// Record upserts the (saga_id, step) row to status (pending -> done/failed). It is
	// idempotent (INSERT ... ON CONFLICT (saga_id, step) DO UPDATE).
	Record(ctx context.Context, sagaID, identityID, kind, step, status string) error
	// CompletedSteps returns the set of steps already marked done for saga_id, so the
	// resume path can skip them.
	CompletedSteps(ctx context.Context, sagaID string) (map[string]bool, error)
}

// sagaRun is a lightweight per-execution journaling helper bound to one saga_id. It loads
// the already-completed steps once (for forward-recovery skipping) and records each step's
// pending->done/failed transition as it runs. A nil journal makes every method a pass-
// through so the saga is unchanged when journaling is not wired.
type sagaRun struct {
	journal    SagaJournal
	id         string
	identityID string
	kind       string
	completed  map[string]bool
}

// newSagaRun builds a run for (kind, identityID), loading completed steps when a journal
// is wired. A CompletedSteps error is non-fatal (empty set -> re-run every step, safe
// because steps are idempotent).
func newSagaRun(ctx context.Context, j SagaJournal, kind, identityID string) *sagaRun {
	r := &sagaRun{journal: j, id: sagaID(kind, identityID), identityID: identityID, kind: kind, completed: map[string]bool{}}
	if j == nil {
		return r
	}
	if done, err := j.CompletedSteps(ctx, r.id); err != nil {
		slog.Warn("saga journal: load completed steps failed", "kind", kind, "step", "resume")
	} else if done != nil {
		r.completed = done
	}
	return r
}

// record writes a step status best-effort (a journal error is logged, never returned).
func (r *sagaRun) record(ctx context.Context, step, status string) {
	if r == nil || r.journal == nil {
		return
	}
	if err := r.journal.Record(ctx, r.id, r.identityID, r.kind, step, status); err != nil {
		slog.Warn("saga journal: record step failed", "kind", r.kind, "step", step, "status", status)
	}
}

// step runs fn under journaling: it skips fn when the journal already marks step done
// (forward recovery), otherwise records pending, runs fn, and records done/failed. fn MUST
// be idempotent so a re-run of a step that partially completed before a crash converges.
func (r *sagaRun) step(ctx context.Context, step string, fn func(context.Context) error) error {
	if r != nil && r.completed[step] {
		return nil // already completed in a prior run — skip (idempotent forward recovery)
	}
	r.record(ctx, step, sagaPending)
	if err := fn(ctx); err != nil {
		r.record(ctx, step, sagaFailed)
		return err
	}
	r.record(ctx, step, sagaDone)
	if r != nil {
		r.completed[step] = true
	}
	return nil
}

// done marks a step done without running work (used when the step's work already happened
// inline, e.g. the aura identity leg commits its own tx before journaling).
func (r *sagaRun) done(ctx context.Context, step string) {
	r.record(ctx, step, sagaDone)
	if r != nil {
		r.completed[step] = true
	}
}

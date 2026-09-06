package cron

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

// The idempotency owner for a system scheduler row must be an identity the product does
// not delete.
//
// scheduler_tasks.identity_id is text with DEFAULT 'local', and the boot seeders create
// their sweeps with no identity at all, so the sentinel reaches this path on every boot.
// It used to map to LocalOperatorIdentity — which serve_auth.go DELETES the first time an
// operator enrolls — while idempotency_operations.identity_id is FK'd to aura.identities
// ON DELETE CASCADE. The result was a foreign key to a row that no longer existed, on the
// scheduler's own system tasks, every tick, forever. idempotency_http.go had already hit
// and fixed this for the public mutations; this is the same registry.
func TestSystemTaskIdempotencyOwnerSurvivesOperatorEnrolment(t *testing.T) {
	for _, sentinel := range []string{"local", "", "not-a-uuid"} {
		t.Run(sentinel, func(t *testing.T) {
			ctx, err := scheduledOperationContext(context.Background(),
				Task{ID: "t1", IdentityID: sentinel, Kind: KindAgentJob},
				&Claim{RunID: "run-1"})
			if err != nil {
				t.Fatalf("scheduledOperationContext: %v", err)
			}
			got := identityctx.IdentityID(ctx)
			if got == identityctx.LocalOperatorIdentity {
				t.Fatal("owner is the local seed, which serve_auth.go deletes on first enrolment")
			}
			if got != identityctx.CLIServiceIdentity {
				t.Fatalf("owner = %q, want the surviving service identity", got)
			}
		})
	}
}

// A real tenant UUID is passed through untouched — the fallback must never capture a row
// that already names its owner.
func TestTenantTaskKeepsItsOwnIdempotencyOwner(t *testing.T) {
	const tenant = "11111111-1111-4111-8111-111111111111"
	ctx, err := scheduledOperationContext(context.Background(),
		Task{ID: "t1", IdentityID: tenant, Kind: KindAgentJob}, &Claim{RunID: "run-1"})
	if err != nil {
		t.Fatalf("scheduledOperationContext: %v", err)
	}
	if got := identityctx.IdentityID(ctx); got != tenant {
		t.Fatalf("owner = %q, want the tenant preserved exactly", got)
	}
}

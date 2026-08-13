package runner

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/identityctx"
)

// The disabled state has to be genuinely disabled. A LedgerAdapter over a nil store
// would be non-nil and therefore look wired, and the gate would then pay a ledger
// read at every voluntary termination just to be told there is nothing to say.

func TestVerificationIsOffWithoutAStore(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	ctx := identityctx.WithIdentityID(context.Background(), "00000000-0000-0000-0000-000000000001")

	if ledger := r.verificationLedger(ctx); ledger != nil {
		t.Fatalf("ledger = %#v, want nil: a deployment with no pool has no gate", ledger)
	}
	if hooks := r.verificationHooks(ctx, "conv-1"); hooks != r.hookManager {
		t.Fatal("without a store the turn must reuse the process-wide hook manager unchanged")
	}
}

func TestVerificationIsOffWithoutAnIdentity(t *testing.T) {
	// Every ledger row is keyed on an identity (migration 0094's RLS is fail-closed),
	// so an unscoped ctx has nothing to key on. A bare context.Background() reaching
	// buildAgent would mean a caller skipped scopeContextToConversation.
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())

	if ledger := r.verificationLedger(context.Background()); ledger != nil {
		t.Fatalf("ledger = %#v, want nil for an unscoped context", ledger)
	}
	if hooks := r.verificationHooks(context.Background(), "conv-1"); hooks != r.hookManager {
		t.Fatal("an unscoped context must not get an evidence-writing hook")
	}
}

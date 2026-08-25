package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
)

type expiryIdentityListerFake struct {
	identities []identity.Identity
	err        error
}

func (f expiryIdentityListerFake) ListIdentities(context.Context) ([]identity.Identity, error) {
	return f.identities, f.err
}

type expiryRunnerFake struct {
	identities []string
	cutoffs    []time.Time
	limits     []int
	err        error
}

func (f *expiryRunnerFake) ExpirePendingApprovals(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	f.identities = append(f.identities, identityctx.IdentityID(ctx))
	f.cutoffs = append(f.cutoffs, cutoff)
	f.limits = append(f.limits, limit)
	return 1, f.err
}

func TestExpireApprovalsByIdentityScopesActiveOwners(t *testing.T) {
	cutoff := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	run := &expiryRunnerFake{}
	count, err := expireApprovalsByIdentity(t.Context(), expiryIdentityListerFake{identities: []identity.Identity{
		{ID: "owner-a"},
		{ID: "owner-disabled", Deactivated: true},
		{ID: ""},
		{ID: "owner-b"},
	}}, run, cutoff, 17)
	if err != nil {
		t.Fatalf("expireApprovalsByIdentity: %v", err)
	}
	if count != 2 {
		t.Fatalf("expired = %d, want 2", count)
	}
	if len(run.identities) != 2 || run.identities[0] != "owner-a" || run.identities[1] != "owner-b" {
		t.Fatalf("scoped identities = %v", run.identities)
	}
	for i := range run.cutoffs {
		if !run.cutoffs[i].Equal(cutoff) || run.limits[i] != 17 {
			t.Fatalf("call %d cutoff/limit = %v/%d", i, run.cutoffs[i], run.limits[i])
		}
	}
}

func TestApprovalExpiryInterval(t *testing.T) {
	tests := []struct {
		ttl  time.Duration
		want time.Duration
	}{
		{ttl: 0, want: 0},
		{ttl: -time.Second, want: 0},
		{ttl: 20 * time.Second, want: 10 * time.Second},
		{ttl: 48 * time.Hour, want: time.Minute},
	}
	for _, test := range tests {
		if got := approvalExpiryInterval(test.ttl); got != test.want {
			t.Errorf("approvalExpiryInterval(%v) = %v, want %v", test.ttl, got, test.want)
		}
	}
}

func TestNewApprovalExpirySweeperRunsOwnerScopedSweep(t *testing.T) {
	run := &expiryRunnerFake{}
	sweeper := newApprovalExpirySweeper(run, expiryIdentityListerFake{identities: []identity.Identity{{ID: "owner-a"}}}, 2*time.Hour)
	sweeper.SweepNow(t.Context())

	if len(run.identities) != 1 || run.identities[0] != "owner-a" {
		t.Fatalf("sweep identities = %v, want owner-a", run.identities)
	}
	if len(run.limits) != 1 || run.limits[0] != approvalExpiryBatchSize {
		t.Fatalf("sweep limits = %v, want %d", run.limits, approvalExpiryBatchSize)
	}
	if age := time.Since(run.cutoffs[0]); age < 2*time.Hour || age > 2*time.Hour+time.Second {
		t.Fatalf("sweep cutoff age = %v, want approximately 2h", age)
	}

	errorSweeper := newApprovalExpirySweeper(&expiryRunnerFake{}, expiryIdentityListerFake{err: errors.New("list failed")}, 2*time.Hour)
	errorSweeper.SweepNow(t.Context())
}

func TestExpireApprovalsByIdentitySurfacesConfigurationAndWorkerErrors(t *testing.T) {
	testErr := errors.New("expiry worker test error")
	if _, err := expireApprovalsByIdentity(t.Context(), nil, &expiryRunnerFake{}, time.Now(), 1); err == nil {
		t.Fatal("nil identity lister was accepted")
	}
	if _, err := expireApprovalsByIdentity(t.Context(), expiryIdentityListerFake{err: testErr}, &expiryRunnerFake{}, time.Now(), 1); !errors.Is(err, testErr) {
		t.Fatalf("identity list error = %v, want wrapped test error", err)
	}
	run := &expiryRunnerFake{err: testErr}
	count, err := expireApprovalsByIdentity(t.Context(), expiryIdentityListerFake{identities: []identity.Identity{{ID: "owner-a"}}}, run, time.Now(), 1)
	if count != 1 || !errors.Is(err, testErr) {
		t.Fatalf("worker result = %d, %v; want 1 and joined test error", count, err)
	}
}

package agui

import (
	"context"
	"errors"
	"testing"
	"time"
)

// deprovision_branches_test.go covers the Deprovisioner entry points + fallbacks the
// happy-path tests leave uncovered: the forced-purge PurgeOne, the custom grace window, the
// resource-plane-only resolve fallback (no Deactivator), and the PurgeExpired legs (no
// Deactivator wired, a ListPurgeable error, and a per-identity purge failure that is logged
// and skipped so the batch continues).

func TestPurgeOneResolvesThenPurges(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	f.deact.targets[testIdentityID] = targetFor(testIdentityID)
	d := NewDeprovisioner(deps)

	if err := d.PurgeOne(context.Background(), testIdentityID); err != nil {
		t.Fatalf("PurgeOne: %v", err)
	}
	if f.conv.count() != 1 || f.graph.count() != 1 {
		t.Fatalf("PurgeOne data-plane: conversations=%d graph=%d, want 1/1", f.conv.count(), f.graph.count())
	}
	if f.iddel.count() != 1 || f.authdel.count() != 1 {
		t.Fatalf("PurgeOne identity/authula delete: id=%d authula=%d, want 1/1", f.iddel.count(), f.authdel.count())
	}
}

func TestGraceWindowCustomOverride(t *testing.T) {
	const custom = 48 * time.Hour
	deps, f := fullDeprovisionDeps()
	deps.GraceWindow = custom
	f.deact.targets[testIdentityID] = targetFor(testIdentityID)
	d := NewDeprovisioner(deps)

	before := time.Now().UTC()
	if err := d.Deactivate(context.Background(), testIdentityID); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	pa, ok := f.deact.marked[testIdentityID]
	if !ok {
		t.Fatal("Deactivate did not stamp purge_after")
	}
	// The custom 48h window must be honored (well past the 7-day default's lower bound but
	// materially different from it — assert it lands within [now+custom-1m, now+custom+1m]).
	if pa.Before(before.Add(custom-time.Minute)) || pa.After(before.Add(custom+time.Minute)) {
		t.Fatalf("purge_after %v is not ~now+%v (custom grace not honored)", pa, custom)
	}
}

func TestResolveFallsBackWithoutDeactivator(t *testing.T) {
	// No Deactivator wired: Deactivate must still journal + succeed, resolving an id-only
	// target (the resource-plane-only path). Sessions/Jobs record on the id-only target.
	deps, f := fullDeprovisionDeps()
	deps.Deactivator = nil
	d := NewDeprovisioner(deps)

	if err := d.Deactivate(context.Background(), testIdentityID); err != nil {
		t.Fatalf("Deactivate without Deactivator: %v", err)
	}
	// AuthulaUserID is empty on an id-only target, so KillSessions is skipped; jobs still run.
	if f.sess.count() != 0 {
		t.Fatalf("KillSessions calls = %d, want 0 (no authula user on an id-only target)", f.sess.count())
	}
	if f.jobs.count() != 1 {
		t.Fatalf("TerminateJobs calls = %d, want 1", f.jobs.count())
	}
}

func TestPurgeExpiredNoDeactivator(t *testing.T) {
	deps, _ := fullDeprovisionDeps()
	deps.Deactivator = nil
	d := NewDeprovisioner(deps)
	purged, err := d.PurgeExpired(context.Background(), time.Now().UTC())
	if err != nil || purged != 0 {
		t.Fatalf("PurgeExpired without Deactivator = (%d,%v), want (0,nil)", purged, err)
	}
}

func TestPurgeExpiredListError(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	f.deact.listErr = errors.New("scan failed")
	d := NewDeprovisioner(deps)
	if _, err := d.PurgeExpired(context.Background(), time.Now().UTC()); err == nil {
		t.Fatal("PurgeExpired must surface a ListPurgeable error")
	}
}

func TestPurgeExpiredSkipsFailedIdentity(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	// Two targets; the graph purge fails for BOTH, so each Purge errors and is skipped —
	// PurgeExpired returns 0 purged with no error (the next scan retries).
	f.deact.purgeable = []DeprovisionTarget{
		targetFor("a1111111-1111-4111-8111-111111111111"),
		targetFor("b2222222-2222-4222-8222-222222222222"),
	}
	f.graph.err = errors.New("graph purge crash")
	d := NewDeprovisioner(deps)

	purged, err := d.PurgeExpired(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("PurgeExpired must not surface a per-identity failure: %v", err)
	}
	if purged != 0 {
		t.Fatalf("purged = %d, want 0 (both purges failed and were skipped)", purged)
	}
	// The batch continued past the first failure: the identity delete never ran for either.
	if f.iddel.count() != 0 {
		t.Fatalf("identity deletes = %d, want 0 (both purges aborted at the graph leg)", f.iddel.count())
	}
}

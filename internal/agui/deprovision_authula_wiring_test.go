package agui

import (
	"context"
	"testing"
)

// The daemon builds this saga BEFORE the Authula provider exists — cmd/aura/serve.go wires the
// cron dispatch, then the AG-UI server, and only then buildAuthDeps — so both Authula legs were
// nil on every removal the cockpit made, and the saga's nil-skip made that silent. Measured on
// the live stack 2026-09-12: six Authula users outlived the identities the cockpit had removed,
// while Postgres, ArcadeDB, Garage and the sandbox were clean. The legs are therefore wired
// AFTER construction, on the one instance both consumers share.
func TestDeprovisionPurgeRemovesAnAuthulaUserWiredAfterConstruction(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	deps.Sessions, deps.AuthulaDelete = nil, nil // the daemon's boot order, before the fix
	d := NewDeprovisioner(deps)

	d.SetAuthulaTeardown(f.sess, f.authdel)

	if err := d.Purge(context.Background(), targetFor(testIdentityID)); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if f.authdel.count() != 1 {
		t.Fatalf("authula deletes = %d, want 1: a removed identity must not outlive its account",
			f.authdel.count())
	}
	if !f.journal.stepDone(sagaID(sagaKindDeprovision, testIdentityID), sagaStepAuthula) {
		t.Error("the authula step is not journaled done")
	}
}

// Deactivate blocks login by killing the sessions, and that leg arrives with the same wiring.
func TestDeprovisionDeactivateKillsSessionsWiredAfterConstruction(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	deps.Sessions, deps.AuthulaDelete = nil, nil
	f.deact.targets[testIdentityID] = targetFor(testIdentityID)
	d := NewDeprovisioner(deps)

	d.SetAuthulaTeardown(f.sess, f.authdel)

	if err := d.Deactivate(context.Background(), testIdentityID); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if f.sess.count() != 1 {
		t.Fatalf("session kills = %d, want 1", f.sess.count())
	}
}

// A deployment with no Authula provider keeps the nil-skip: the pre-cutover behaviour is that
// the legs do nothing, not that the purge fails.
func TestDeprovisionPurgeSkipsAnUnwiredAuthulaTeardown(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	deps.Sessions, deps.AuthulaDelete = nil, nil
	d := NewDeprovisioner(deps)

	if err := d.Purge(context.Background(), targetFor(testIdentityID)); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if f.authdel.count() != 0 || f.sess.count() != 0 {
		t.Fatalf("unwired teardown ran: authula=%d sessions=%d", f.authdel.count(), f.sess.count())
	}
}

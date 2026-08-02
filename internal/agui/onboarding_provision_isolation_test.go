package agui

import (
	"context"
	"errors"
	"testing"
)

// onboarding_provision_isolation_test.go locks the single-principal refusal: the saga
// REFUSES to create an additional identity while AURA_MUSR_ISOLATION is off, so a
// deployment whose skills library, settings, MCP catalog and (under a non-strict profile)
// host filesystem are shared can never acquire a second principal to share them WITH. The
// saga only ever creates ADDITIONAL, non-local identities (the operator/local is
// bootstrap-seeded, migration 0004), so requiring the flag on is correct.

// TestProvisionRefusedWhenIsolationOff proves a service built with MUSRIsolation:false
// refuses Provision with errIsolationDisabled BEFORE any cross-store write. The fakes record
// any write; assertNoWrites proves none happened (the refusal precedes the Authula lookup).
func TestProvisionRefusedWhenIsolationOff(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	recovery := &fakeRecoveryStore{}
	// Build the service directly (NOT via sagaService, which sets isolation ON) with the flag
	// OFF, then start a real session so the refusal is reached at the pre-validate step rather
	// than short-circuited by a missing session.
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: []string{"identity.create"}},
		Profiles:     &recordingProfileWriter{},
		Authula:      au, AuraLeg: leg, Telegram: tg, BotUsername: "AuraBot", Recovery: recovery,
		MUSRIsolation: false,
	})
	start, err := svc.StartSession(context.Background(), "creator-1")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	_, err = svc.Provision(context.Background(), "creator-1", start.SessionToken, provReq(nil))
	if !errors.Is(err, errIsolationDisabled) {
		t.Fatalf("flag-off provision err = %v, want errIsolationDisabled", err)
	}
	assertNoWrites(t, au, leg, tg)
	if recovery.upserts != 0 {
		t.Fatalf("flag-off provision wrote recovery rows = %d, want 0", recovery.upserts)
	}
}

// TestProvisionProceedsPastIsolationGateWhenOn is the positive control: with MUSRIsolation:true
// the saga clears the isolation gate and reaches the existing pre-validate/backend path,
// committing the happy path with the same fakes sagaService uses. err==nil + committed rows
// prove the gate was cleared (never errIsolationDisabled).
func TestProvisionProceedsPastIsolationGateWhenOn(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"}) // sagaService builds MUSRIsolation:true
	resp, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil))
	if err != nil {
		t.Fatalf("flag-on provision must clear the isolation gate and commit, got %v", err)
	}
	if resp.IdentityID == "" || leg.liveIdentities() != 1 || au.liveAuthulaUsers() != 1 {
		t.Fatalf("flag-on provision did not commit: id=%q identities=%d authula=%d",
			resp.IdentityID, leg.liveIdentities(), au.liveAuthulaUsers())
	}
}

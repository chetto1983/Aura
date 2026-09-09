package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// onboarding_provision_credit_test.go proves the plan 02-06 credit leg: a provision run
// mints the new identity's own OpenRouter key at a zero cap and stores it encrypted
// (never returning the raw key or its hash/label to the browser), a failure anywhere
// later in the saga revokes what was minted, a nil credit port skips the plane exactly
// like the other optional resource legs, and the leg's own failure fails the whole
// saga rather than proceeding with a partly-built identity.

// creditMintCall is one recorded MintKey invocation. limit/externalUser are captured
// as fixed literals mirroring the real composition-root adapter's contract (MintKey
// always mints at a zero cap with external.user == identityID, CRED-02) — the fake
// models the whole mint+persist composition the real adapter performs in one call, so
// a unit test at this layer can assert on both halves without a live Postgres or a
// live OpenRouter account.
type creditMintCall struct {
	identityID   string
	keyName      string
	limit        float64
	externalUser string
}

type creditSaveCall struct {
	hash, label string
}

type fakeCredit struct {
	mu        sync.Mutex
	mintErr   error
	revokeErr error
	minted    []creditMintCall
	saved     []creditSaveCall
	revoked   []string
	nextHash  int
}

func (f *fakeCredit) MintKey(_ context.Context, identityID, keyName string) (MintedKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.minted = append(f.minted, creditMintCall{identityID: identityID, keyName: keyName, limit: 0, externalUser: identityID})
	if f.mintErr != nil {
		return MintedKey{}, f.mintErr
	}
	f.nextHash++
	hash := fmt.Sprintf("hash-%d", f.nextHash)
	label := "sk-or-v1-fake..." + hash
	// Mirrors the real adapter's second half (identitykey.Store.Save): the fake records
	// the "save" alongside the "mint" so a test at this layer can assert both without a
	// live database.
	f.saved = append(f.saved, creditSaveCall{hash: hash, label: label})
	return MintedKey{Hash: hash, Label: label}, nil
}

func (f *fakeCredit) RevokeKey(_ context.Context, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, hash)
	return f.revokeErr
}

func (f *fakeCredit) mintCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.minted)
}

func (f *fakeCredit) revokeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.revoked)
}

// sagaServiceWithCredit mirrors sagaService (onboarding_provision_fakes_test.go) but
// additionally wires the credit port + journal this file's tests need — kept local
// rather than widening sagaService's signature, which every other provisioning test
// file already calls with its existing arity.
func sagaServiceWithCredit(t *testing.T, au *fakeAuthula, leg *fakeAuraLeg, tg *fakeTelegram, credit OpenRouterKeyMinter, journal SagaJournal, creatorGrants []string) (*onboardingService, string) {
	t.Helper()
	recovery := &fakeRecoveryStore{}
	leg.recovery = recovery
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: creatorGrants},
		Profiles:     &recordingProfileWriter{},
		Authula:      au, AuraLeg: leg, Telegram: tg, BotUsername: "AuraBot", Recovery: recovery,
		Memory:        newFakeMemoryProvisioner(),
		Credit:        credit,
		Journal:       journal,
		MUSRIsolation: true,
	})
	start, err := svc.StartSession(context.Background(), "creator-1")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return svc, start.SessionToken
}

// TestProvisionMintsKeyAtZeroCap proves the mint call carries a zero limit and the new
// identity's own id as external.user, and that the store records exactly one save
// carrying the returned hash and label (CRED-02).
func TestProvisionMintsKeyAtZeroCap(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	fc := &fakeCredit{}
	svc, tok := sagaServiceWithCredit(t, au, leg, tg, fc, nil, []string{"identity.create"})

	resp, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if fc.mintCount() != 1 {
		t.Fatalf("mint calls = %d, want 1", fc.mintCount())
	}
	call := fc.minted[0]
	if call.limit != 0 {
		t.Fatalf("mint limit = %v, want 0", call.limit)
	}
	if call.externalUser != resp.IdentityID {
		t.Fatalf("mint external user = %q, want the new identity id %q", call.externalUser, resp.IdentityID)
	}
	if len(fc.saved) != 1 {
		t.Fatalf("store saves = %d, want 1", len(fc.saved))
	}
	if fc.saved[0].hash == "" || fc.saved[0].label == "" {
		t.Fatalf("saved record missing hash/label: %#v", fc.saved[0])
	}
}

// TestProvisionStoresKeyEncryptedNotReturned proves the provision response's marshalled
// bytes carry nothing about the key — no field named key/hash/label, case-insensitive —
// so a field added later to the DTO is caught by this test rather than by review
// (T-02-06c).
func TestProvisionStoresKeyEncryptedNotReturned(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	fc := &fakeCredit{}
	svc, tok := sagaServiceWithCredit(t, au, leg, tg, fc, nil, []string{"identity.create"})

	resp, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	// Check the top-level FIELD NAMES, not the whole byte string: the QR SVG value
	// legitimately carries an `aria-label` attribute, which would false-positive a raw
	// substring scan of the marshalled bytes without ever putting a key/hash/label on
	// the wire.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	for field := range fields {
		lower := strings.ToLower(field)
		for _, forbidden := range []string{"key", "hash", "label"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("provision response DTO carries a field named %q (matches %q): %s", field, forbidden, raw)
			}
		}
	}
}

// TestProvisionCompensatesMintOnLaterFailure proves the orphan-key case: a provision
// that fails at a leg AFTER the credit leg revokes the minted key exactly once, with
// the hash the minter returned — an interrupted provision must never leave a key alive
// at the provider that nobody in Aura knows about (T-02-31).
func TestProvisionCompensatesMintOnLaterFailure(t *testing.T) {
	boom := errors.New("injected: audit write failure")
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{auditErr: boom}, &fakeTelegram{}
	fc := &fakeCredit{}
	svc, tok := sagaServiceWithCredit(t, au, leg, tg, fc, nil, []string{"identity.create"})

	if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
		t.Fatal("want error on audit failure")
	}
	if fc.mintCount() != 1 {
		t.Fatalf("mint calls = %d, want 1", fc.mintCount())
	}
	if fc.revokeCount() != 1 {
		t.Fatalf("revoke calls = %d, want exactly 1 (the orphan key must be revoked)", fc.revokeCount())
	}
	wantHash := fc.saved[0].hash
	if fc.revoked[0] != wantHash {
		t.Fatalf("revoked hash = %q, want the minted hash %q", fc.revoked[0], wantHash)
	}
}

// TestProvisionCreditLegIsJournaled proves the saga journal carries the new step name
// for a successful run.
func TestProvisionCreditLegIsJournaled(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	fc := &fakeCredit{}
	journal := newFakeJournal()
	svc, tok := sagaServiceWithCredit(t, au, leg, tg, fc, journal, []string{"identity.create"})

	resp, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	sid := sagaID(sagaKindProvision, resp.IdentityID)
	if !journal.stepDone(sid, sagaStepOpenRouterKey) {
		t.Fatalf("saga journal step %q not marked done", sagaStepOpenRouterKey)
	}
}

// TestProvisionWithNilCreditPortSkipsPlane proves a nil credit port provisions without
// a key and without an error — and the identity that results is one every turn refuses
// (D-13/CRED-05), which is the correct consequence and is asserted here as "no mint
// call happened", not as an absence of error alone.
func TestProvisionWithNilCreditPortSkipsPlane(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	if svc.credit != nil {
		t.Fatal("sagaService must leave credit nil for this test to be meaningful")
	}

	resp, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil))
	if err != nil {
		t.Fatalf("Provision with a nil credit port: %v", err)
	}
	if resp.IdentityID == "" {
		t.Fatal("provision returned no identity id")
	}
	if leg.liveIdentities() != 1 {
		t.Fatalf("live identities = %d, want 1 (a nil credit port must not fail the saga)", leg.liveIdentities())
	}
}

// TestProvisionMintFailureFailsTheSaga proves a minter that errors fails the provision
// with a named error and compensates every leg before it — it must not proceed to a
// partly-built identity.
func TestProvisionMintFailureFailsTheSaga(t *testing.T) {
	boom := errors.New("injected: mint failure")
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	fc := &fakeCredit{mintErr: boom}
	svc, tok := sagaServiceWithCredit(t, au, leg, tg, fc, nil, []string{"identity.create"})

	if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
		t.Fatal("want error on mint failure")
	}
	if leg.liveIdentities() != 0 || au.liveAuthulaUsers() != 0 || tg.mintedCount() != 0 || leg.auditCount() != 0 {
		t.Fatalf("mint-fail orphans: identities=%d authula=%d tokens=%d audit=%d",
			leg.liveIdentities(), au.liveAuthulaUsers(), tg.mintedCount(), leg.auditCount())
	}
	if fc.revokeCount() != 0 {
		t.Fatalf("revoke calls = %d, want 0 (nothing was minted to revoke)", fc.revokeCount())
	}
}

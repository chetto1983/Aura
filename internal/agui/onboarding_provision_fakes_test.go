package agui

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

// onboarding_provision_fakes_test.go holds the shared in-memory saga-leg fakes + helpers
// (fakeAuthula/fakeAuraLeg/fakeTelegram/fakeRecoveryStore + sagaService/assertNoWrites)
// reused across the provisioning unit tests. Split out of onboarding_provision_test.go on
// touch to hold both files under the 600-LOC ceiling.

type fakeAuthula struct {
	mu            sync.Mutex
	existing      bool // UserByEmail result
	lookupErr     error
	hashErr       error
	createUserErr error
	createAcctErr error
	enforceErr    error
	hashes        int
	created       []string // user IDs created
	deleted       []string // user IDs deleted (COMP_B)
	enforced      []string // user IDs the D-15 first-login policy was applied to
	nextID        int
}

func (f *fakeAuthula) UserByEmail(_ context.Context, _ string) (bool, error) {
	return f.existing, f.lookupErr
}

func (f *fakeAuthula) HashPassword(p string) (string, error) {
	if f.hashErr != nil {
		return "", f.hashErr
	}
	f.mu.Lock()
	f.hashes++
	f.mu.Unlock()
	return "hashed:" + p, nil
}

func (f *fakeAuthula) CreateUser(_ context.Context, email string) (AuthulaUser, error) {
	if f.createUserErr != nil {
		return AuthulaUser{}, f.createUserErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := "authula-user-" + strconv.Itoa(f.nextID)
	f.created = append(f.created, id)
	return AuthulaUser{ID: id, Email: email}, nil
}

func (f *fakeAuthula) CreateAccount(_ context.Context, _, _, _ string) error { return f.createAcctErr }

func (f *fakeAuthula) EnforceFirstLogin(_ context.Context, userID string) error {
	if f.enforceErr != nil {
		return f.enforceErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enforced = append(f.enforced, userID)
	return nil
}

func (f *fakeAuthula) firstLoginEnforced() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.enforced)
}

func (f *fakeAuthula) DeleteUser(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, userID)
	return nil
}

func (f *fakeAuthula) liveAuthulaUsers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.created) - len(f.deleted)
}

type fakeAuraLeg struct {
	mu              sync.Mutex
	createErr       error
	auditErr        error
	recovery        *fakeRecoveryStore
	telegram        *fakeTelegram
	created         []string // identity names created
	deleted         []string // identity names deleted (compensation)
	audited         []string // identity ids with an audit row
	pendingAtDelete bool
	nextID          int
}

func (f *fakeAuraLeg) CreateIdentityWithGrants(_ context.Context, p AuraLegParams) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := "identity-" + strconv.Itoa(f.nextID)
	f.created = append(f.created, p.IdentityName)
	return id, nil
}

func (f *fakeAuraLeg) WriteAuditRow(_ context.Context, _ AuraLegParams, newIdentityID string) error {
	if f.auditErr != nil {
		return f.auditErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audited = append(f.audited, newIdentityID)
	return nil
}

func (f *fakeAuraLeg) DeleteIdentity(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, name)
	if f.telegram != nil && f.telegram.mintedCount() != 0 {
		f.pendingAtDelete = true
	}
	if f.recovery != nil {
		f.recovery.deleteCascade()
	}
	return nil
}

func (f *fakeAuraLeg) liveIdentities() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.created) - len(f.deleted)
}

func (f *fakeAuraLeg) auditCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.audited)
}

type fakeTelegram struct {
	mu              sync.Mutex
	insertErr       error
	commitBeforeErr bool
	pending         map[string]bool
	deleted         []string
	consumed        bool
}

func (f *fakeTelegram) InsertPending(_ context.Context, tok, _ string, _ time.Time) error {
	if f.commitBeforeErr {
		f.mu.Lock()
		if f.pending == nil {
			f.pending = map[string]bool{}
		}
		f.pending[tok] = true
		f.mu.Unlock()
	}
	if f.insertErr != nil {
		return f.insertErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == nil {
		f.pending = map[string]bool{}
	}
	f.pending[tok] = true
	return nil
}

func (f *fakeTelegram) DeletePending(_ context.Context, tok string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, tok)
	if !f.pending[tok] {
		return errors.New("unknown pending token")
	}
	delete(f.pending, tok)
	return nil
}

func (f *fakeTelegram) PendingConsumed(_ context.Context, _ string) (bool, error) {
	return f.consumed, nil
}

func (f *fakeTelegram) mintedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending)
}

type fakeRecoveryStore struct {
	identityID, question, hash, version string
	err                                 error
	upserts, deleted                    int
}

func (f *fakeRecoveryStore) UpsertRecovery(_ context.Context, identityID, question, answerHash, answerHashVersion string) error {
	if f.err != nil {
		return f.err
	}
	f.upserts++
	f.identityID, f.question, f.hash, f.version = identityID, question, answerHash, answerHashVersion
	return nil
}

func (f *fakeRecoveryStore) deleteCascade() { f.deleted = f.upserts }

func (f *fakeRecoveryStore) liveRecoveryRows() int { return f.upserts - f.deleted }

func sagaService(t *testing.T, au *fakeAuthula, leg *fakeAuraLeg, tg *fakeTelegram, creatorGrants []string) (*onboardingService, string) {
	t.Helper()
	recovery := &fakeRecoveryStore{}
	leg.recovery = recovery
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: creatorGrants},
		Extractor:    &countingExtractor{},
		Profiles:     &recordingProfileWriter{},
		Authula:      au, AuraLeg: leg, Telegram: tg, BotUsername: "AuraBot", Recovery: recovery,
		// Isolation ON so the saga-body tests clear the CR-01 provision-time refusal gate
		// (errIsolationDisabled); the gate itself is covered in onboarding_provision_isolation_test.go.
		MUSRIsolation: true,
	})
	start, err := svc.StartSession(context.Background(), "creator-1")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return svc, start.SessionToken
}

func assertNoWrites(t *testing.T, au *fakeAuthula, leg *fakeAuraLeg, tg *fakeTelegram) {
	t.Helper()
	if au.hashes != 0 {
		t.Errorf("pre-write path hashed a password (%d)", au.hashes)
	}
	if au.liveAuthulaUsers() != 0 || len(au.created) != 0 {
		t.Errorf("escalation/forbidden created an Authula user (%d)", len(au.created))
	}
	if leg.liveIdentities() != 0 || len(leg.created) != 0 {
		t.Errorf("escalation/forbidden created an identity (%d)", len(leg.created))
	}
	if tg.mintedCount() != 0 {
		t.Errorf("escalation/forbidden minted a token (%d)", tg.mintedCount())
	}
	if leg.auditCount() != 0 {
		t.Errorf("escalation/forbidden wrote an audit row (%d)", leg.auditCount())
	}
}

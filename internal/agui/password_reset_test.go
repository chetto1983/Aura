package agui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPasswordResetStartSendsTelegramCode(t *testing.T) {
	store := newFakePasswordResetStore(t)
	messenger := &fakeRecoveryMessenger{}
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: messenger,
		Resetter:  &fakePasswordResetter{},
		Clock:     func() time.Time { return now },
	})

	resp, err := svc.Start(context.Background(), PasswordResetStartRequest{
		Email:     "  reset@example.com  ",
		RequestIP: "198.51.100.10:53391",
		UserAgent: "AuraTest/1.0",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q, want ok", resp.Status)
	}
	if store.lookupEmail != "reset@example.com" {
		t.Fatalf("lookup email = %q, want trimmed email", store.lookupEmail)
	}
	if store.challenge.IdentityID != store.record.IdentityID {
		t.Fatalf("challenge identity = %q, want %q", store.challenge.IdentityID, store.record.IdentityID)
	}
	if store.challenge.ExpiresAt != now.Add(10*time.Minute) {
		t.Fatalf("challenge expiry = %s, want %s", store.challenge.ExpiresAt, now.Add(10*time.Minute))
	}
	if store.challenge.RequestIPHash == "" || strings.Contains(store.challenge.RequestIPHash, "198.51.100.10") {
		t.Fatalf("request IP was not hashed safely: %q", store.challenge.RequestIPHash)
	}
	if store.challenge.UserAgentHash == "" || strings.Contains(store.challenge.UserAgentHash, "AuraTest") {
		t.Fatalf("user agent was not hashed safely: %q", store.challenge.UserAgentHash)
	}
	if messenger.identityID != store.record.IdentityID {
		t.Fatalf("messenger identity = %q, want %q", messenger.identityID, store.record.IdentityID)
	}
	if messenger.code == "" {
		t.Fatal("messenger did not receive a recovery code")
	}
	if strings.Contains(messenger.code, "reset@example.com") {
		t.Fatalf("recovery code leaked the email: %q", messenger.code)
	}
	if !VerifyOpaqueSecret(messenger.code, store.challenge.CodeHash) {
		t.Fatal("stored challenge hash does not verify the sent code")
	}
	if len(store.events) != 1 || store.events[0].Event != "reset_start" {
		t.Fatalf("events = %+v, want one reset_start audit event", store.events)
	}
	assertNoPasswordResetEventLeak(t, store.events, "reset@example.com", messenger.code, "198.51.100.10", "AuraTest")
}

func TestPasswordResetVerifyAndComplete(t *testing.T) {
	store := newFakePasswordResetStore(t)
	resetter := &fakePasswordResetter{}
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: &fakeRecoveryMessenger{},
		Resetter:  resetter,
		Clock:     func() time.Time { return time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC) },
	})

	verify, err := svc.Verify(context.Background(), PasswordResetVerifyRequest{
		Email:  "reset@example.com",
		Code:   "123456",
		Answer: "Blue bicycle",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verify.ResetToken != store.resetToken {
		t.Fatalf("reset token = %q, want %q", verify.ResetToken, store.resetToken)
	}
	if store.verifiedIdentity != store.record.IdentityID || store.verifiedCode != "123456" {
		t.Fatalf("VerifyChallenge got identity=%q code=%q", store.verifiedIdentity, store.verifiedCode)
	}
	if len(store.events) != 1 || store.events[0].Event != "reset_verify" {
		t.Fatalf("events after verify = %+v, want one reset_verify audit event", store.events)
	}
	assertNoPasswordResetEventLeak(t, store.events, "reset@example.com", "123456", "Blue bicycle")

	complete, err := svc.Complete(context.Background(), PasswordResetCompleteRequest{
		ResetToken: store.resetToken,
		Password:   "new-pass-123",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if complete.Status != "password_updated" {
		t.Fatalf("complete status = %q, want password_updated", complete.Status)
	}
	if resetter.identityID != store.record.IdentityID || resetter.password != "new-pass-123" {
		t.Fatalf("resetter got identity=%q password=%q", resetter.identityID, resetter.password)
	}
	if store.consumedTokenHash != HashLookupToken(store.resetToken) {
		t.Fatalf("consumed token hash = %q, want exact HashLookupToken(resetToken)", store.consumedTokenHash)
	}
	if len(store.events) != 2 || store.events[1].Event != "reset_complete" {
		t.Fatalf("events = %+v, want reset_verify then reset_complete audit events", store.events)
	}
	assertNoPasswordResetEventLeak(t, store.events, store.resetToken, "new-pass-123")
}

func TestPasswordResetVerifyDeniedAuditsAndCountsKnownIdentity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*fakePasswordResetStore)
		req       PasswordResetVerifyRequest
		wantCount int
	}{
		{
			name: "wrong answer counts active challenge attempt",
			req: PasswordResetVerifyRequest{
				Email: "reset@example.com", Code: "raw-code", Answer: "wrong answer",
				RequestIP: "203.0.113.8:4000", UserAgent: "SensitiveBrowser/9",
			},
			wantCount: 1,
		},
		{
			name: "unsupported hash version counts active challenge attempt",
			mutate: func(store *fakePasswordResetStore) {
				store.record.AnswerHashVersion = "argon2id-v0"
			},
			req: PasswordResetVerifyRequest{
				Email: "reset@example.com", Code: "raw-code", Answer: "Blue bicycle",
				RequestIP: "203.0.113.8:4000", UserAgent: "SensitiveBrowser/9",
			},
			wantCount: 1,
		},
		{
			name: "wrong code audits denial without counting answer attempt",
			mutate: func(store *fakePasswordResetStore) {
				store.verifyErr = ErrPasswordResetDenied
			},
			req: PasswordResetVerifyRequest{
				Email: "reset@example.com", Code: "raw-code", Answer: "Blue bicycle",
				RequestIP: "203.0.113.8:4000", UserAgent: "SensitiveBrowser/9",
			},
		},
		{
			name: "missing active challenge from attempt hook remains denied",
			mutate: func(store *fakePasswordResetStore) {
				store.challengeAttemptErr = ErrPasswordResetDenied
			},
			req: PasswordResetVerifyRequest{
				Email: "reset@example.com", Code: "raw-code", Answer: "wrong answer",
				RequestIP: "203.0.113.8:4000", UserAgent: "SensitiveBrowser/9",
			},
		},
		{
			name: "missing recovery audits denial",
			mutate: func(store *fakePasswordResetStore) {
				store.record.TelegramUserID = 0
			},
			req: PasswordResetVerifyRequest{
				Email: "reset@example.com", Code: "raw-code", Answer: "Blue bicycle",
				RequestIP: "203.0.113.8:4000", UserAgent: "SensitiveBrowser/9",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakePasswordResetStore(t)
			if tc.mutate != nil {
				tc.mutate(store)
			}
			svc := NewPasswordResetService(PasswordResetDeps{
				Store:     store,
				Messenger: &fakeRecoveryMessenger{},
				Resetter:  &fakePasswordResetter{},
			})

			_, err := svc.Verify(context.Background(), tc.req)
			if !errors.Is(err, ErrPasswordResetDenied) {
				t.Fatalf("Verify err = %v, want ErrPasswordResetDenied", err)
			}
			if store.challengeAttempts != tc.wantCount {
				t.Fatalf("challenge attempts = %d, want %d", store.challengeAttempts, tc.wantCount)
			}
			if len(store.events) != 1 || store.events[0].Event != "reset_verify_denied" {
				t.Fatalf("events = %+v, want one reset_verify_denied audit event", store.events)
			}
			assertNoPasswordResetEventLeak(t, store.events, "reset@example.com", "raw-code", "Blue bicycle", "wrong answer", "203.0.113.8", "SensitiveBrowser")
		})
	}
}

func TestPasswordResetVerifyAttemptStorageFailureIsUnavailable(t *testing.T) {
	store := newFakePasswordResetStore(t)
	store.challengeAttemptErr = errors.New("attempt store failed for reset@example.com raw-code wrong answer")
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: &fakeRecoveryMessenger{},
		Resetter:  &fakePasswordResetter{},
	})

	_, err := svc.Verify(context.Background(), PasswordResetVerifyRequest{
		Email:  "reset@example.com",
		Code:   "raw-code",
		Answer: "wrong answer",
	})
	if !errors.Is(err, errPasswordResetUnavailable) {
		t.Fatalf("Verify err = %v, want unavailable", err)
	}
	for _, secret := range []string{"reset@example.com", "raw-code", "wrong answer"} {
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Fatalf("attempt storage error leaked %q: %v", secret, err)
		}
	}
	if len(store.events) != 0 {
		t.Fatalf("events = %+v, want none when attempt accounting storage fails", store.events)
	}
}

func TestPasswordResetSanitizesDeniedFlow(t *testing.T) {
	store := newFakePasswordResetStore(t)
	store.lookupErr = ErrPasswordResetDenied
	messenger := &fakeRecoveryMessenger{}
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: messenger,
		Resetter:  &fakePasswordResetter{},
	})

	resp, err := svc.Start(context.Background(), PasswordResetStartRequest{
		Email:     "missing@example.com",
		RequestIP: "203.0.113.9",
		UserAgent: "SensitiveBrowser/9",
	})
	if err != nil {
		t.Fatalf("denied Start returned error: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("denied status = %q, want ok", resp.Status)
	}
	if messenger.code != "" {
		t.Fatalf("denied start sent a Telegram code: %q", messenger.code)
	}
	if len(store.events) != 1 || store.events[0].Event != "reset_start_denied" {
		t.Fatalf("events = %+v, want reset_start_denied audit", store.events)
	}
	assertNoPasswordResetEventLeak(t, store.events, "missing@example.com", "203.0.113.9", "SensitiveBrowser")

	_, err = svc.Verify(context.Background(), PasswordResetVerifyRequest{
		Email:  "missing@example.com",
		Code:   "raw-code",
		Answer: "wrong answer",
	})
	if !errors.Is(err, ErrPasswordResetDenied) {
		t.Fatalf("Verify denied err = %v, want ErrPasswordResetDenied", err)
	}
	for _, secret := range []string{"missing@example.com", "raw-code", "wrong answer"} {
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Fatalf("denied error leaked %q: %v", secret, err)
		}
	}
}

func TestPasswordResetStartDeniesUnsupportedRecoveryWithoutEnumeration(t *testing.T) {
	store := newFakePasswordResetStore(t)
	store.record.TelegramUserID = 0
	messenger := &fakeRecoveryMessenger{}
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: messenger,
		Resetter:  &fakePasswordResetter{},
	})

	resp, err := svc.Start(context.Background(), PasswordResetStartRequest{
		Email:     "reset@example.com",
		RequestIP: "203.0.113.10",
		UserAgent: "SensitiveBrowser/10",
	})
	if err != nil {
		t.Fatalf("unsupported recovery Start returned error: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("unsupported recovery status = %q, want ok", resp.Status)
	}
	if messenger.code != "" {
		t.Fatalf("unsupported recovery sent a Telegram code: %q", messenger.code)
	}
	if len(store.events) != 1 || store.events[0].Event != "reset_start_denied" {
		t.Fatalf("events = %+v, want reset_start_denied audit", store.events)
	}
	assertNoPasswordResetEventLeak(t, store.events, "reset@example.com", "203.0.113.10", "SensitiveBrowser")
}

func newFakePasswordResetStore(t *testing.T) *fakePasswordResetStore {
	t.Helper()
	answerHash, answerVersion, err := (RecoveryHasher{}).HashAnswer("blue bicycle")
	if err != nil {
		t.Fatalf("HashAnswer: %v", err)
	}
	return &fakePasswordResetStore{
		record: RecoveryRecord{
			IdentityID:        "identity-1",
			Email:             "reset@example.com",
			AuthulaUserID:     "authula-1",
			Question:          "Favorite bike?",
			AnswerHash:        answerHash,
			AnswerHashVersion: answerVersion,
			TelegramUserID:    123456789,
		},
		resetToken: "reset-token-secret",
	}
}

type fakePasswordResetStore struct {
	record              RecoveryRecord
	lookupEmail         string
	lookupErr           error
	challenge           PasswordResetChallenge
	verifiedIdentity    string
	verifiedCode        string
	verifyErr           error
	resetToken          string
	consumedTokenHash   string
	consumeErr          error
	challengeAttempts   int
	challengeAttemptErr error
	events              []RecoveryEvent
}

func (f *fakePasswordResetStore) LookupByEmail(_ context.Context, email string) (RecoveryRecord, error) {
	f.lookupEmail = email
	if f.lookupErr != nil {
		return RecoveryRecord{}, f.lookupErr
	}
	return f.record, nil
}

func (f *fakePasswordResetStore) StartChallenge(_ context.Context, challenge PasswordResetChallenge) error {
	f.challenge = challenge
	return nil
}

func (f *fakePasswordResetStore) VerifyChallenge(_ context.Context, identityID, code string) (string, error) {
	f.verifiedIdentity = identityID
	f.verifiedCode = code
	if f.verifyErr != nil {
		return "", f.verifyErr
	}
	return f.resetToken, nil
}

func (f *fakePasswordResetStore) ConsumeResetTokenHash(_ context.Context, tokenHash string) (string, error) {
	f.consumedTokenHash = tokenHash
	if f.consumeErr != nil {
		return "", f.consumeErr
	}
	return f.record.IdentityID, nil
}

func (f *fakePasswordResetStore) RecordChallengeAttempt(_ context.Context, identityID string) error {
	if f.challengeAttemptErr != nil {
		return f.challengeAttemptErr
	}
	if identityID == f.record.IdentityID {
		f.challengeAttempts++
	}
	return nil
}

func (f *fakePasswordResetStore) RecordRecoveryEvent(_ context.Context, event RecoveryEvent) error {
	f.events = append(f.events, event)
	return nil
}

type fakeRecoveryMessenger struct {
	identityID string
	code       string
	err        error
}

func (f *fakeRecoveryMessenger) SendRecoveryCode(_ context.Context, identityID, code string) error {
	f.identityID = identityID
	f.code = code
	return f.err
}

type fakePasswordResetter struct {
	identityID string
	password   string
	err        error
}

func (f *fakePasswordResetter) SetPassword(_ context.Context, identityID, password string) error {
	f.identityID = identityID
	f.password = password
	return f.err
}

func assertNoPasswordResetEventLeak(t *testing.T, events []RecoveryEvent, secrets ...string) {
	t.Helper()
	for _, event := range events {
		got := event.Event + event.IdentityID + event.EmailHash + event.RequestIPHash + event.UserAgentHash
		for _, secret := range secrets {
			if secret != "" && strings.Contains(got, secret) {
				t.Fatalf("audit event leaked %q in %+v", secret, event)
			}
		}
	}
}

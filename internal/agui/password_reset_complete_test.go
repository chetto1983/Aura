package agui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

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
	if store.claimedTokenHash != HashLookupToken(store.resetToken) {
		t.Fatalf("claimed token hash = %q, want exact HashLookupToken(resetToken)", store.claimedTokenHash)
	}
	if store.consumedTokenHash != HashLookupToken(store.resetToken) {
		t.Fatalf("consumed token hash = %q, want exact HashLookupToken(resetToken)", store.consumedTokenHash)
	}
	if len(store.events) != 2 || store.events[1].Event != "reset_complete" {
		t.Fatalf("events = %+v, want reset_verify then reset_complete audit events", store.events)
	}
	assertNoPasswordResetEventLeak(t, store.events, store.resetToken, "new-pass-123")
}

func TestPasswordResetCompleteDoesNotMutateWhenTokenClaimDenied(t *testing.T) {
	store := newFakePasswordResetStore(t)
	store.claimErr = ErrPasswordResetDenied
	resetter := &fakePasswordResetter{}
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: &fakeRecoveryMessenger{},
		Resetter:  resetter,
	})

	_, err := svc.Complete(context.Background(), PasswordResetCompleteRequest{
		ResetToken: store.resetToken,
		Password:   "new-pass-123",
	})
	if !errors.Is(err, ErrPasswordResetDenied) {
		t.Fatalf("Complete err = %v, want ErrPasswordResetDenied", err)
	}
	if resetter.password != "" {
		t.Fatalf("claim denial mutated password with %q", resetter.password)
	}
	if store.consumedTokenHash != "" {
		t.Fatalf("claim denial consumed token hash %q", store.consumedTokenHash)
	}
}

func TestPasswordResetCompleteDoesNotConsumeTokenWhenResetterFails(t *testing.T) {
	store := newFakePasswordResetStore(t)
	resetter := &fakePasswordResetter{err: errors.New("authula unavailable for new-pass-123")}
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: &fakeRecoveryMessenger{},
		Resetter:  resetter,
	})

	_, err := svc.Complete(context.Background(), PasswordResetCompleteRequest{
		ResetToken: store.resetToken,
		Password:   "new-pass-123",
	})
	if !errors.Is(err, errPasswordResetUnavailable) {
		t.Fatalf("Complete err = %v, want unavailable", err)
	}
	if store.claimedTokenHash != HashLookupToken(store.resetToken) {
		t.Fatalf("claimed token hash = %q, want exact HashLookupToken(resetToken)", store.claimedTokenHash)
	}
	if store.consumedTokenHash != "" {
		t.Fatalf("resetter failure consumed token hash %q", store.consumedTokenHash)
	}
	if store.releasedClaims != 1 {
		t.Fatalf("released claims = %d, want 1 so resetter failure remains retryable", store.releasedClaims)
	}
	if len(store.events) != 0 {
		t.Fatalf("events = %+v, want none when password update fails", store.events)
	}
	for _, secret := range []string{store.resetToken, "new-pass-123"} {
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Fatalf("Complete error leaked %q: %v", secret, err)
		}
	}
}

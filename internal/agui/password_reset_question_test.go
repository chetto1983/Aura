package agui

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestPasswordResetQuestionRevealsAfterCodePossession proves the question is returned only after
// PeekChallenge accepts the code, and that a neutral audit event is written without leaking secrets.
func TestPasswordResetQuestionRevealsAfterCodePossession(t *testing.T) {
	store := newFakePasswordResetStore(t)
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: &fakeRecoveryMessenger{},
		Resetter:  &fakePasswordResetter{},
	})

	resp, err := svc.Question(context.Background(), PasswordResetQuestionRequest{
		Email:     "  reset@example.com  ",
		Code:      "123456",
		RequestIP: "198.51.100.11:5555",
		UserAgent: "AuraTest/2.0",
	})
	if err != nil {
		t.Fatalf("Question: %v", err)
	}
	if resp.Question != store.record.Question {
		t.Fatalf("question = %q, want %q", resp.Question, store.record.Question)
	}
	if store.peekedIdentity != store.record.IdentityID || store.peekedCode != "123456" {
		t.Fatalf("PeekChallenge got identity=%q code=%q", store.peekedIdentity, store.peekedCode)
	}
	if store.lookupEmail != "reset@example.com" {
		t.Fatalf("lookup email = %q, want trimmed", store.lookupEmail)
	}
	if len(store.events) != 1 || store.events[0].Event != "reset_question" {
		t.Fatalf("events = %+v, want one reset_question audit event", store.events)
	}
	assertNoPasswordResetEventLeak(t, store.events, "reset@example.com", "123456", "198.51.100.11", "AuraTest")
}

// TestPasswordResetQuestionNeutralOnBadCode proves a wrong code yields the neutral denial without
// revealing the question, mirroring the enumeration-resistant verify path.
func TestPasswordResetQuestionNeutralOnBadCode(t *testing.T) {
	store := newFakePasswordResetStore(t)
	store.peekErr = ErrPasswordResetDenied
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: &fakeRecoveryMessenger{},
		Resetter:  &fakePasswordResetter{},
	})

	resp, err := svc.Question(context.Background(), PasswordResetQuestionRequest{
		Email: "reset@example.com",
		Code:  "000000",
	})
	if !errors.Is(err, ErrPasswordResetDenied) {
		t.Fatalf("Question err = %v, want ErrPasswordResetDenied", err)
	}
	if resp.Question != "" {
		t.Fatalf("bad code revealed a question: %q", resp.Question)
	}
	if len(store.events) != 1 || store.events[0].Event != "reset_question_denied" {
		t.Fatalf("events = %+v, want reset_question_denied audit", store.events)
	}
}

// TestPasswordResetQuestionNeutralOnUnknownEmail proves an unknown email is denied before the code
// is ever peeked, so the endpoint cannot be used as an account-existence oracle.
func TestPasswordResetQuestionNeutralOnUnknownEmail(t *testing.T) {
	store := newFakePasswordResetStore(t)
	store.lookupErr = ErrPasswordResetDenied
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: &fakeRecoveryMessenger{},
		Resetter:  &fakePasswordResetter{},
	})

	resp, err := svc.Question(context.Background(), PasswordResetQuestionRequest{
		Email: "ghost@example.com",
		Code:  "123456",
	})
	if !errors.Is(err, ErrPasswordResetDenied) {
		t.Fatalf("Question err = %v, want ErrPasswordResetDenied", err)
	}
	if resp.Question != "" {
		t.Fatalf("unknown email revealed a question: %q", resp.Question)
	}
	if store.peekedCode != "" {
		t.Fatalf("unknown email still peeked the code: %q", store.peekedCode)
	}
	if len(store.events) != 1 || store.events[0].Event != "reset_question_denied" {
		t.Fatalf("events = %+v, want reset_question_denied audit", store.events)
	}
}

// TestPasswordResetCompleteRejectsSamePassword proves the same-password sentinel propagates through
// Complete and the reset token is released (not consumed) so the user can retry with a new password.
func TestPasswordResetCompleteRejectsSamePassword(t *testing.T) {
	store := newFakePasswordResetStore(t)
	resetter := &fakePasswordResetter{err: ErrPasswordResetSamePassword}
	svc := NewPasswordResetService(PasswordResetDeps{
		Store:     store,
		Messenger: &fakeRecoveryMessenger{},
		Resetter:  resetter,
	})

	_, err := svc.Complete(context.Background(), PasswordResetCompleteRequest{
		ResetToken: store.resetToken,
		Password:   "same-as-current",
	})
	if !errors.Is(err, ErrPasswordResetSamePassword) {
		t.Fatalf("Complete err = %v, want ErrPasswordResetSamePassword", err)
	}
	if store.consumedTokenHash != "" {
		t.Fatalf("same-password rejection consumed token hash %q", store.consumedTokenHash)
	}
	if store.releasedClaims != 1 {
		t.Fatalf("released claims = %d, want 1 so the user can retry with a different password", store.releasedClaims)
	}
	if strings.Contains(err.Error(), "same-as-current") {
		t.Fatalf("same-password error leaked the password: %v", err)
	}
}

package agui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestProvisionShapeValidationFailsBeforeWrites(t *testing.T) {
	cases := map[string]struct {
		grants []string
		mutate func(*OnboardingProvisionRequest)
	}{
		"empty password":     {[]string{"identity.create"}, func(req *OnboardingProvisionRequest) { req.Password = "" }},
		"oversized email":    {[]string{"identity.create"}, func(req *OnboardingProvisionRequest) { req.Email = strings.Repeat("a", onboardingEmailMaxLen+1) }},
		"oversized password": {[]string{"identity.create"}, func(req *OnboardingProvisionRequest) { req.Password = strings.Repeat("p", onboardingPasswordMaxLen+1) }},
		"oversized question": {[]string{"identity.create"}, func(req *OnboardingProvisionRequest) {
			req.SecurityQuestion = strings.Repeat("q", onboardingSecurityQuestionMaxLen+1)
		}},
		"oversized answer": {[]string{"identity.create"}, func(req *OnboardingProvisionRequest) {
			req.SecurityAnswer = strings.Repeat("a", onboardingSecurityAnswerMaxLen+1)
		}},
		// Every case names the creator as holding identity.create, so nothing but the shape
		// check the case is named after can refuse it. These two used to seed '*', which
		// confers nothing since migration 0121 retired the wildcard -- the grant check is an
		// exact set lookup (onboarding_provision_grants.go). They still tested the shape,
		// because Provision validates it before it reads the creator's grants; the
		// errOnboardingForbidden guard below is what stops a reordering from turning them
		// into authority refusals that the bare err != nil would still have passed.
		"too many caps": {[]string{"identity.create"}, func(req *OnboardingProvisionRequest) { req.Capabilities = manyCaps(onboardingMaxCaps + 1) }},
		"oversized capability name": {[]string{"identity.create"}, func(req *OnboardingProvisionRequest) {
			req.Capabilities = []string{strings.Repeat("c", onboardingCapNameMaxLen+1)}
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
			recovery := &fakeRecoveryStore{}
			svc, tok := sagaService(t, au, leg, tg, tc.grants)
			svc.recovery = recovery
			req := provReq(nil)
			tc.mutate(&req)
			_, err := svc.Provision(context.Background(), "creator-1", tok, req)
			if err == nil {
				t.Fatal("Provision succeeded, want validation error")
			}
			if errors.Is(err, errOnboardingForbidden) {
				t.Fatalf("Provision refused on authority (%v), want the shape validation this case names", err)
			}
			assertNoWrites(t, au, leg, tg)
			if recovery.upserts != 0 {
				t.Fatalf("recovery writes = %d, want 0", recovery.upserts)
			}
		})
	}
}

func TestProvisionShapeValidationRunsBeforeSessionLookup(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, _ := sagaService(t, au, leg, tg, []string{"identity.create"})
	req := provReq(nil)
	req.LinkTelegram = false
	_, err := svc.Provision(context.Background(), "creator-1", "never-started", req)
	if err == nil {
		t.Fatal("Provision succeeded, want validation error")
	}
	if errors.Is(err, errOnboardingSessionNotFound) || errors.Is(err, errProvisioningUnavailable) {
		t.Fatalf("Provision err = %v, want validation before session/backend checks", err)
	}
	assertNoWrites(t, au, leg, tg)
}

func TestProvisionShapeValidationWinsOverUnavailableBackend(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	svc.authula = nil
	req := provReq(nil)
	req.LinkTelegram = false
	_, err := svc.Provision(context.Background(), "creator-1", tok, req)
	if err == nil {
		t.Fatal("Provision succeeded, want validation error")
	}
	if errors.Is(err, errProvisioningUnavailable) {
		t.Fatalf("Provision err = %v, want validation before backend availability", err)
	}
	assertNoWrites(t, au, leg, tg)
}

func manyCaps(n int) []string {
	caps := make([]string, n)
	for i := range caps {
		caps[i] = "cap." + strconv.Itoa(i)
	}
	return caps
}

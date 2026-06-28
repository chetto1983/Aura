package agui

import (
	"context"
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
		"too many caps": {[]string{"*"}, func(req *OnboardingProvisionRequest) { req.Capabilities = manyCaps(onboardingMaxCaps + 1) }},
		"oversized capability name": {[]string{"*"}, func(req *OnboardingProvisionRequest) {
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
			if _, err := svc.Provision(context.Background(), "creator-1", tok, req); err == nil {
				t.Fatal("Provision succeeded, want validation error")
			}
			assertNoWrites(t, au, leg, tg)
			if recovery.upserts != 0 {
				t.Fatalf("recovery writes = %d, want 0", recovery.upserts)
			}
		})
	}
}

func manyCaps(n int) []string {
	caps := make([]string, n)
	for i := range caps {
		caps[i] = "cap." + strconv.Itoa(i)
	}
	return caps
}

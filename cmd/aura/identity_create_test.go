package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
)

// identity_create_test.go covers parseIdentityCreateFlags and the error-mapping/dispatch
// seam (runIdentityCreate + identityCreateErrorMessage) with NO pool, NO Docker, and NO
// Authula — mirroring parseRecoverOperatorFlags's untagged, daemon-free test seam.

func TestParseIdentityCreateFlags(t *testing.T) {
	t.Run("valid accumulates repeatable capability in order", func(t *testing.T) {
		f, err := parseIdentityCreateFlags([]string{
			"-email", "b@example.test",
			"-security-question", "favorite color",
			"-capability", "agent.run",
			"-capability", "document.search",
			"-capability", "identity.create",
		})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		want := []string{"agent.run", "document.search", "identity.create"}
		if len(f.capabilities) != len(want) {
			t.Fatalf("capabilities = %v, want %v", f.capabilities, want)
		}
		for i, c := range want {
			if f.capabilities[i] != c {
				t.Fatalf("capabilities[%d] = %q, want %q (request order)", i, f.capabilities[i], c)
			}
		}
	})

	t.Run("operator defaults to the seeded local operator", func(t *testing.T) {
		f, err := parseIdentityCreateFlags([]string{"-email", "b@example.test", "-security-question", "q"})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if f.operator != localSeededIdentityID {
			t.Fatalf("operator = %q, want default %q", f.operator, localSeededIdentityID)
		}
	})

	t.Run("missing email is refused before any password prompt could run", func(t *testing.T) {
		_, err := parseIdentityCreateFlags([]string{"-security-question", "q"})
		if err == nil {
			t.Fatal("err = nil, want a refusal naming -email")
		}
	})

	t.Run("missing security question is refused", func(t *testing.T) {
		_, err := parseIdentityCreateFlags([]string{"-email", "b@example.test"})
		if err == nil {
			t.Fatal("err = nil, want a refusal naming -security-question")
		}
	})

	t.Run("unknown flag is refused and names the flag", func(t *testing.T) {
		_, err := parseIdentityCreateFlags([]string{"-bogus"})
		if err == nil {
			t.Fatal("err = nil, want a refusal naming the unknown flag")
		}
	})

	t.Run("trailing positional argument is refused", func(t *testing.T) {
		_, err := parseIdentityCreateFlags([]string{
			"-email", "b@example.test", "-security-question", "q", "extra-arg",
		})
		if err == nil {
			t.Fatal("err = nil, want a refusal naming the unexpected positional argument")
		}
	})
}

// stubOnboardingCreator satisfies onboardingCreator for TestIdentityCreateErrors — no
// pool, no Docker, no Authula. It records what it was called with so the test can assert
// the exact StartSession-then-Provision sequence (D-07) and that the two secrets flow
// through untouched.
type stubOnboardingCreator struct {
	startErr      error
	startResp     agui.OnboardingStart
	provisionErr  error
	provisionResp agui.OnboardingProvisionResponse

	gotRequester string
	gotToken     string
	gotReq       agui.OnboardingProvisionRequest
}

func (s *stubOnboardingCreator) StartSession(_ context.Context, creatorIdentityID string) (agui.OnboardingStart, error) {
	s.gotRequester = creatorIdentityID
	return s.startResp, s.startErr
}

func (s *stubOnboardingCreator) Provision(_ context.Context, requesterIdentityID, token string, in agui.OnboardingProvisionRequest) (agui.OnboardingProvisionResponse, error) {
	s.gotRequester = requesterIdentityID
	s.gotToken = token
	s.gotReq = in
	return s.provisionResp, s.provisionErr
}

func TestIdentityCreateErrors(t *testing.T) {
	f := identityCreateFlags{
		email:            "b@example.test",
		securityQuestion: "favorite color",
		capabilities:     []string{"agent.run"},
		operator:         localSeededIdentityID,
	}

	t.Run("StartSession failure short-circuits before Provision", func(t *testing.T) {
		sentinel := errors.New("onboarding: this deployment is configured for a single identity")
		stub := &stubOnboardingCreator{startErr: sentinel}
		_, err := runIdentityCreate(context.Background(), stub, f, "pw", "answer")
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want %v", err, sentinel)
		}
		if stub.gotToken != "" {
			t.Fatalf("Provision was called (gotToken=%q) after StartSession failed, want short-circuit", stub.gotToken)
		}
		if got := identityCreateErrorMessage(err); got != "identity create: "+sentinel.Error() {
			t.Fatalf("identityCreateErrorMessage = %q, want the isolation guidance passed through unchanged", got)
		}
	})

	t.Run("duplicate identity gets a distinct message via errors.Is", func(t *testing.T) {
		stub := &stubOnboardingCreator{
			startResp:    agui.OnboardingStart{SessionToken: "tok-1"},
			provisionErr: agui.ErrOnboardingDuplicate,
		}
		_, err := runIdentityCreate(context.Background(), stub, f, "pw", "answer")
		if !errors.Is(err, agui.ErrOnboardingDuplicate) {
			t.Fatalf("err = %v, want ErrOnboardingDuplicate", err)
		}
		got := identityCreateErrorMessage(err)
		if got == "identity create: "+agui.ErrOnboardingDuplicate.Error() {
			t.Fatalf("duplicate message = %q, want a distinct message (not the raw sentinel string)", got)
		}
		wantDistinct := "identity create: an identity with this email already exists"
		if got != wantDistinct {
			t.Fatalf("duplicate message = %q, want %q", got, wantDistinct)
		}
	})

	t.Run("escalation is distinguishable from duplicate and from a backend failure", func(t *testing.T) {
		stub := &stubOnboardingCreator{
			startResp:    agui.OnboardingStart{SessionToken: "tok-2"},
			provisionErr: agui.ErrOnboardingEscalation,
		}
		_, err := runIdentityCreate(context.Background(), stub, f, "pw", "answer")
		got := identityCreateErrorMessage(err)
		dup := identityCreateErrorMessage(agui.ErrOnboardingDuplicate)
		if got == dup {
			t.Fatalf("escalation message %q collided with duplicate message %q, want distinct", got, dup)
		}
	})

	t.Run("StartSession-then-Provision order and secrets flow through untouched", func(t *testing.T) {
		stub := &stubOnboardingCreator{
			startResp:     agui.OnboardingStart{SessionToken: "tok-3"},
			provisionResp: agui.OnboardingProvisionResponse{IdentityID: "id-3", DeepLink: "https://t.me/bot?start=tok-3"},
		}
		resp, err := runIdentityCreate(context.Background(), stub, f, "S3cret-pw", "S3cret-answer")
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if resp.IdentityID != "id-3" {
			t.Fatalf("IdentityID = %q, want id-3", resp.IdentityID)
		}
		if stub.gotToken != "tok-3" {
			t.Fatalf("Provision token = %q, want the StartSession-minted token tok-3", stub.gotToken)
		}
		if stub.gotReq.Password != "S3cret-pw" || stub.gotReq.SecurityAnswer != "S3cret-answer" {
			t.Fatalf("secrets did not flow through to the request untouched")
		}
		if !stub.gotReq.LinkTelegram {
			t.Fatal("LinkTelegram = false, want true (D-06: Telegram stays a required port)")
		}
		if len(stub.gotReq.Capabilities) != 1 || stub.gotReq.Capabilities[0] != "agent.run" {
			t.Fatalf("Capabilities = %v, want [agent.run]", stub.gotReq.Capabilities)
		}
	})
}

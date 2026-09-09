package agui

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
)

func provReq(caps []string) OnboardingProvisionRequest {
	return OnboardingProvisionRequest{Email: "newbie@aura.local", Password: "s3cret-temp-pw", SecurityQuestion: "First school?", SecurityAnswer: "Blue School", Capabilities: caps, LinkTelegram: true}
}

func TestProvisionStoresRecoveryQuestionAndHash(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	recovery := &fakeRecoveryStore{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	svc.recovery = recovery

	req := provReq(nil)
	req.SecurityQuestion = "First school?"
	req.SecurityAnswer = "  Blue   School "
	resp, err := svc.Provision(context.Background(), "creator-1", tok, req)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if recovery.identityID != resp.IdentityID {
		t.Fatalf("recovery identity = %q, want %q", recovery.identityID, resp.IdentityID)
	}
	if recovery.question != "First school?" {
		t.Fatalf("question = %q", recovery.question)
	}
	if recovery.hash == "" || strings.Contains(recovery.hash, "Blue") {
		t.Fatalf("answer hash leaked raw answer: %q", recovery.hash)
	}
	if recovery.version != recoveryAnswerHashVersion {
		t.Fatalf("version = %q, want %q", recovery.version, recoveryAnswerHashVersion)
	}
}

func TestValidateOnboardingProvisionRequiresRecovery(t *testing.T) {
	cases := map[string]func(*OnboardingProvisionRequest){
		"whitespace email":          func(req *OnboardingProvisionRequest) { req.Email = " \t\n " },
		"missing security question": func(req *OnboardingProvisionRequest) { req.SecurityQuestion = "" },
		"missing security answer":   func(req *OnboardingProvisionRequest) { req.SecurityAnswer = "" },
		"linkTelegram=false":        func(req *OnboardingProvisionRequest) { req.LinkTelegram = false },
	}
	for name, mutate := range cases {
		req := provReq(nil)
		mutate(&req)
		if err := validateOnboardingProvision(req); err == nil {
			t.Fatalf("%s should fail", name)
		}
	}
}

func TestProvisionPrerequisitesFailBeforeWrites(t *testing.T) {
	cases := map[string]func(*onboardingService, *OnboardingProvisionRequest){
		"linkTelegram=false": func(_ *onboardingService, req *OnboardingProvisionRequest) { req.LinkTelegram = false },
		"nil authula":        func(s *onboardingService, _ *OnboardingProvisionRequest) { s.authula = nil },
		"nil aura leg":       func(s *onboardingService, _ *OnboardingProvisionRequest) { s.auraLeg = nil },
		"nil telegram":       func(s *onboardingService, _ *OnboardingProvisionRequest) { s.telegram = nil },
		"empty bot username": func(s *onboardingService, _ *OnboardingProvisionRequest) { s.botName = "" },
		"nil recovery":       func(s *onboardingService, _ *OnboardingProvisionRequest) { s.recovery = nil },
		"whitespace email":   func(_ *onboardingService, req *OnboardingProvisionRequest) { req.Email = " \t\n " },
		"blank question":     func(_ *onboardingService, req *OnboardingProvisionRequest) { req.SecurityQuestion = " " },
		"blank answer":       func(_ *onboardingService, req *OnboardingProvisionRequest) { req.SecurityAnswer = " \t\n " },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
			recovery := &fakeRecoveryStore{}
			svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
			svc.recovery = recovery
			req := provReq(nil)
			mutate(svc, &req)
			_, err := svc.Provision(context.Background(), "creator-1", tok, req)
			if err == nil {
				t.Fatal("Provision succeeded, want error")
			}
			if name == "linkTelegram=false" && errors.Is(err, errProvisioningUnavailable) {
				t.Fatalf("Provision err = %v, want validation error before availability gate", err)
			}
			if name != "linkTelegram=false" && name != "whitespace email" && name != "blank question" && name != "blank answer" && !errors.Is(err, errProvisioningUnavailable) {
				t.Fatalf("Provision err = %v, want provisioning unavailable", err)
			}
			assertNoWrites(t, au, leg, tg)
			if recovery.upserts != 0 {
				t.Fatalf("recovery writes = %d, want 0", recovery.upserts)
			}
		})
	}
}

func TestProvisionSagaHappyPath(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create", "agent.run"})

	resp, err := svc.Provision(context.Background(), "creator-1", tok, provReq([]string{"agent.run"}))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if resp.IdentityID == "" {
		t.Fatal("provision returned no identity id")
	}
	if leg.liveIdentities() != 1 || au.liveAuthulaUsers() != 1 || tg.mintedCount() != 1 {
		t.Fatalf("legs not all committed: identities=%d authula=%d tokens=%d",
			leg.liveIdentities(), au.liveAuthulaUsers(), tg.mintedCount())
	}
	if leg.auditCount() != 1 {
		t.Fatalf("audit rows = %d, want exactly 1 on success", leg.auditCount())
	}
	if au.firstLoginEnforced() != 1 {
		t.Fatalf("D-15 first-login policy applied %d times, want exactly 1", au.firstLoginEnforced())
	}
	if resp.DeepLink == "" || !strings.Contains(resp.DeepLink, "t.me/AuraBot?start=") {
		t.Errorf("deep-link = %q, want a t.me/AuraBot?start=<token> URL", resp.DeepLink)
	}
	if resp.QRSVG == "" || !strings.HasPrefix(resp.QRSVG, "<svg") {
		t.Error("provision must return a server-rendered QR SVG")
	}
}

func TestProvisionSagaCompensation(t *testing.T) {
	boom := errors.New("injected failure")

	t.Run("B1 CreateUser fails -> 0 of everything", func(t *testing.T) {
		au := &fakeAuthula{createUserErr: boom}
		leg, tg := &fakeAuraLeg{}, &fakeTelegram{}
		svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
		if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
			t.Fatal("want error on B1 failure")
		}
		if au.liveAuthulaUsers() != 0 || leg.liveIdentities() != 0 || tg.mintedCount() != 0 || leg.auditCount() != 0 {
			t.Fatalf("B1 orphans: authula=%d identities=%d tokens=%d audit=%d",
				au.liveAuthulaUsers(), leg.liveIdentities(), tg.mintedCount(), leg.auditCount())
		}
	})

	t.Run("B2 CreateAccount fails -> COMP_B deletes the user", func(t *testing.T) {
		au := &fakeAuthula{createAcctErr: boom}
		leg, tg := &fakeAuraLeg{}, &fakeTelegram{}
		svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
		if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
			t.Fatal("want error on B2 failure")
		}
		if au.liveAuthulaUsers() != 0 {
			t.Fatalf("B2: %d orphan Authula users (COMP_B must delete)", au.liveAuthulaUsers())
		}
		if leg.liveIdentities() != 0 || tg.mintedCount() != 0 || leg.auditCount() != 0 {
			t.Fatalf("B2: aura/token/audit orphans identities=%d tokens=%d audit=%d",
				leg.liveIdentities(), tg.mintedCount(), leg.auditCount())
		}
	})

	t.Run("A aura-leg fails -> COMP_B deletes the user", func(t *testing.T) {
		au := &fakeAuthula{}
		leg := &fakeAuraLeg{createErr: boom}
		tg := &fakeTelegram{}
		svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
		if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
			t.Fatal("want error on A failure")
		}
		if au.liveAuthulaUsers() != 0 {
			t.Fatalf("A: %d orphan Authula users (COMP_B must delete)", au.liveAuthulaUsers())
		}
		if leg.liveIdentities() != 0 || tg.mintedCount() != 0 || leg.auditCount() != 0 {
			t.Fatalf("A: orphans identities=%d tokens=%d audit=%d",
				leg.liveIdentities(), tg.mintedCount(), leg.auditCount())
		}
	})

	t.Run("C telegram mint ambiguously fails -> DeletePending + DeleteIdentity + COMP_B", func(t *testing.T) {
		au := &fakeAuthula{}
		leg := &fakeAuraLeg{}
		tg := &fakeTelegram{insertErr: boom, commitBeforeErr: true}
		leg.telegram = tg
		svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
		recovery := svc.recovery.(*fakeRecoveryStore)
		if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
			t.Fatal("want error on C failure")
		}
		if au.liveAuthulaUsers() != 0 || leg.liveIdentities() != 0 || tg.mintedCount() != 0 || recovery.liveRecoveryRows() != 0 || leg.auditCount() != 0 {
			t.Fatalf("C orphans: authula=%d identities=%d tokens=%d recovery=%d audit=%d",
				au.liveAuthulaUsers(), leg.liveIdentities(), tg.mintedCount(), recovery.liveRecoveryRows(), leg.auditCount())
		}
		if leg.pendingAtDelete {
			t.Fatal("DeleteIdentity ran before the pending Telegram token was removed")
		}
	})

	t.Run("recovery write fails -> DeleteIdentity + COMP_B", func(t *testing.T) {
		au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
		svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
		svc.recovery = &fakeRecoveryStore{err: boom}
		if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
			t.Fatal("want recovery write failure")
		}
		if au.liveAuthulaUsers() != 0 || leg.liveIdentities() != 0 || tg.mintedCount() != 0 || leg.auditCount() != 0 {
			t.Fatalf("recovery orphans: authula=%d identities=%d tokens=%d audit=%d",
				au.liveAuthulaUsers(), leg.liveIdentities(), tg.mintedCount(), leg.auditCount())
		}
	})

	t.Run("audit-write fails -> full rollback (no unaudited identity)", func(t *testing.T) {
		au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{auditErr: boom}, &fakeTelegram{}
		svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
		recovery := svc.recovery.(*fakeRecoveryStore)
		if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
			t.Fatal("want error on audit failure")
		}
		if leg.liveIdentities() != 0 || au.liveAuthulaUsers() != 0 || tg.mintedCount() != 0 || recovery.liveRecoveryRows() != 0 || leg.auditCount() != 0 {
			t.Fatalf("audit-fail orphans: identities=%d authula=%d tokens=%d recovery=%d audit=%d",
				leg.liveIdentities(), au.liveAuthulaUsers(), tg.mintedCount(), recovery.liveRecoveryRows(), leg.auditCount())
		}
		if len(tg.deleted) != 1 || tg.deleted[0] == "" {
			t.Fatalf("deleted pending tokens = %#v, want exactly one non-empty token", tg.deleted)
		}
	})
}

// TestNoEscalation pinned the PRE-Phase-2 contract: the request's capability list was
// validated as a subset of the creator's own grants, and '*'/undeclared/malformed names
// were all rejected as escalation attempts. Phase 2 (D-01/RBAC-03) retires that contract on
// purpose — 02-02-PLAN.md Task 2's own action text says so verbatim: "this is no longer a
// subset check, it never was here". Under the new contract the request's capability list no
// longer SELECTS anything: every provisioned identity receives exactly identity.UserSet(),
// unconditionally, and the request is inspected ONLY to refuse an administrative name
// (identity.create/identity.delete) rather than let it be silently narrowed. A non-
// administrative name in the request — wildcard, undeclared, or malformed grammar — is
// simply ignored now, so the five pre-Phase-2 subtests asserting a rejection for those
// inputs assert something that is no longer true and would have to fail forever.
//
// CLAUDE.md forbids modifying a test to make it pass unless the test itself is broken; a
// test pinning a contract this plan explicitly retires qualifies. This rewrite keeps every
// one of the original's no-write assertions on every refusal branch, keeps the one subtest
// whose contract is UNCHANGED (operator without identity.create is still forbidden), and
// adds coverage for the behavior that replaced the retired subset check: an administrative
// name is refused, and everything else is ignored in favor of the uniform grant.
func TestNoEscalation(t *testing.T) {
	t.Run("administrative capability in request refused, no write", func(t *testing.T) {
		for _, admin := range identity.Administrative() {
			t.Run(admin, func(t *testing.T) {
				au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
				svc, tok := sagaService(t, au, leg, tg, []string{"identity.create", "agent.run"})
				_, err := svc.Provision(context.Background(), "creator-1", tok, provReq([]string{admin}))
				if !errors.Is(err, ErrOnboardingEscalation) {
					t.Fatalf("%s request err = %v, want escalation", admin, err)
				}
				assertNoWrites(t, au, leg, tg)
			})
		}
	})

	t.Run("operator without identity.create forbidden, no write", func(t *testing.T) {
		au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
		svc, tok := sagaService(t, au, leg, tg, []string{"agent.run"})
		_, err := svc.Provision(context.Background(), "creator-1", tok, provReq([]string{"agent.run"}))
		if !errors.Is(err, errOnboardingForbidden) {
			t.Fatalf("no-identity.create err = %v, want forbidden", err)
		}
		assertNoWrites(t, au, leg, tg)
	})

	// The wildcard, an undeclared name, and every malformed-grammar case from the retired
	// subtests are all NON-administrative — under D-01/RBAC-03 the request no longer
	// selects the grant, so none of these refuse. Table over what used to be five separate
	// rejections; every case here now succeeds and grants exactly identity.UserSet().
	t.Run("non-administrative request content is ignored; grant is always the uniform set", func(t *testing.T) {
		for _, requested := range [][]string{
			{"*"},
			{"graph.write"},
			{""},
			{"Agent.Run"},
			{"agent run"},
			{"-agent.run"},
			{"agent.run", "graph.read"}, // a mix of well-formed-but-irrelevant names
			nil,                         // empty list truth: RBAC-03 "empty" — still lands the full set
		} {
			t.Run("requested="+strings.Join(requested, ","), func(t *testing.T) {
				au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
				svc, tok := sagaService(t, au, leg, tg, []string{"identity.create", "agent.run"})
				_, err := svc.Provision(context.Background(), "creator-1", tok, provReq(requested))
				if err != nil {
					t.Fatalf("Provision(requested=%v) = %v, want nil — non-administrative content must not refuse", requested, err)
				}
				if leg.liveIdentities() != 1 {
					t.Fatal("provision did not create the identity")
				}
				got := leg.lastGrantedCapabilities()
				want := identity.UserSet()
				if !slices.Equal(got, want) {
					t.Fatalf("granted = %v, want exactly identity.UserSet() = %v regardless of the request", got, want)
				}
			})
		}
	})
}

func TestProvisionConcurrentSameSessionSingleCommit(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})

	const attempts = 8
	start := make(chan struct{})
	errs := make(chan error, attempts)
	for range attempts {
		go func() {
			<-start
			req := provReq(nil)
			_, err := svc.Provision(context.Background(), "creator-1", tok, req)
			errs <- err
		}()
	}
	close(start)

	successes := 0
	sessionRejected := 0
	for range attempts {
		err := <-errs
		switch {
		case err == nil:
			successes++
		case errors.Is(err, errOnboardingSessionNotFound):
			sessionRejected++
		default:
			t.Fatalf("concurrent provision returned unexpected err: %v", err)
		}
	}
	if successes != 1 || sessionRejected != attempts-1 {
		t.Fatalf("concurrent provision successes=%d rejected=%d, want 1/%d", successes, sessionRejected, attempts-1)
	}
	if leg.liveIdentities() != 1 || au.liveAuthulaUsers() != 1 || tg.mintedCount() != 1 || leg.auditCount() != 1 {
		t.Fatalf("concurrent provision writes: identities=%d authula=%d tokens=%d audit=%d, want exactly one of each",
			leg.liveIdentities(), au.liveAuthulaUsers(), tg.mintedCount(), leg.auditCount())
	}
}

func TestProvisionNoSecretInLogs(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const secret = "Sup3rSecret-Passw0rd!"
	const recoverySecret = "School Mascot Secret"

	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	req := provReq(nil)
	req.Password = secret
	req.SecurityAnswer = recoverySecret
	if _, err := svc.Provision(context.Background(), "creator-1", tok, req); err != nil {
		t.Fatalf("provision: %v", err)
	}

	au2 := &fakeAuthula{createAcctErr: errors.New("authula refused password " + secret)}
	leg2, tg2 := &fakeAuraLeg{}, &fakeTelegram{}
	svc2, tok2 := sagaService(t, au2, leg2, tg2, []string{"identity.create"})
	req2 := provReq(nil)
	req2.Password = secret
	req2.SecurityAnswer = recoverySecret
	if _, err := svc2.Provision(context.Background(), "creator-1", tok2, req2); err == nil {
		t.Fatal("want B2 failure")
	} else if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), recoverySecret) {
		t.Fatalf("provision error leaked a secret: %v", err)
	}

	au3, leg3, tg3 := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc3, tok3 := sagaService(t, au3, leg3, tg3, []string{"identity.create"})
	svc3.recovery = &fakeRecoveryStore{err: errors.New("recovery refused answer " + recoverySecret)}
	req3 := provReq(nil)
	req3.Password = secret
	req3.SecurityAnswer = recoverySecret
	if _, err := svc3.Provision(context.Background(), "creator-1", tok3, req3); err == nil {
		t.Fatal("want recovery failure")
	} else if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), recoverySecret) {
		t.Fatalf("recovery error leaked a secret: %v", err)
	}

	if strings.Contains(buf.String(), secret) {
		t.Fatalf("the Authula password leaked into a log line:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), recoverySecret) {
		t.Fatalf("the recovery answer leaked into a log line:\n%s", buf.String())
	}
}

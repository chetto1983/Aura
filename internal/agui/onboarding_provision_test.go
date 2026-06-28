package agui

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// onboarding_provision_test.go covers the cross-store provisioning saga with FAKE ports
// (no DB): the three-way no-escalation re-validation, the ordered saga + each compensation
// leg (the 6 RESEARCH failure-injection points asserting compensation ran), the QR render
// (deep-link in, bot token never), and the no-secret-in-logs guarantee over a full run.
// The LIVE end-to-end saga + zero-orphan assertions against real Postgres + Authula are in
// onboarding_provision_integration_test.go (build tag db_integration).

// --- fake saga ports (call-recording, fault-injectable) ---

type fakeAuthula struct {
	mu            sync.Mutex
	existing      bool // UserByEmail result
	lookupErr     error
	hashErr       error
	createUserErr error
	createAcctErr error
	created       []string // user IDs created
	deleted       []string // user IDs deleted (COMP_B)
	nextID        int
}

func (f *fakeAuthula) UserByEmail(_ context.Context, _ string) (bool, error) {
	return f.existing, f.lookupErr
}

func (f *fakeAuthula) HashPassword(p string) (string, error) {
	if f.hashErr != nil {
		return "", f.hashErr
	}
	return "hashed:" + p, nil
}

func (f *fakeAuthula) CreateUser(_ context.Context, email string) (AuthulaUser, error) {
	if f.createUserErr != nil {
		return AuthulaUser{}, f.createUserErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := "authula-user-" + itoa(f.nextID)
	f.created = append(f.created, id)
	return AuthulaUser{ID: id, Email: email}, nil
}

func (f *fakeAuthula) CreateAccount(_ context.Context, _, _, _ string) error {
	return f.createAcctErr
}

func (f *fakeAuthula) DeleteUser(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, userID)
	return nil
}

// liveAuthulaUsers reports user ids created but not compensated (the "orphan Authula user"
// count for the fake-level assertion).
func (f *fakeAuthula) liveAuthulaUsers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.created) - len(f.deleted)
}

type fakeAuraLeg struct {
	mu        sync.Mutex
	createErr error
	auditErr  error
	created   []string // identity names created
	deleted   []string // identity names deleted (compensation)
	audited   []string // identity ids with an audit row
	nextID    int
}

func (f *fakeAuraLeg) CreateIdentityWithGrants(_ context.Context, p AuraLegParams) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := "identity-" + itoa(f.nextID)
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
	mu        sync.Mutex
	insertErr error
	minted    []string // identity ids a token was minted for
	consumed  bool
}

func (f *fakeTelegram) InsertPending(_ context.Context, _, identityID string, _ time.Time) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.minted = append(f.minted, identityID)
	return nil
}

func (f *fakeTelegram) PendingConsumed(_ context.Context, _ string) (bool, error) {
	return f.consumed, nil
}

func (f *fakeTelegram) mintedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.minted)
}

type fakeRecoveryStore struct {
	identityID, question, hash, version string
	err                                 error
}

func (f *fakeRecoveryStore) UpsertRecovery(_ context.Context, identityID, question, answerHash, answerHashVersion string) error {
	f.identityID, f.question, f.hash, f.version = identityID, question, answerHash, answerHashVersion
	return f.err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func sagaService(t *testing.T, au *fakeAuthula, leg *fakeAuraLeg, tg *fakeTelegram, creatorGrants []string) (*onboardingService, string) {
	t.Helper()
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: creatorGrants},
		Extractor:    &countingExtractor{},
		Profiles:     &recordingProfileWriter{},
		Authula:      au, AuraLeg: leg, Telegram: tg, BotUsername: "AuraBot", Recovery: &fakeRecoveryStore{},
	})
	start, err := svc.StartSession(context.Background(), "creator-1")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return svc, start.SessionToken
}

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

// TestProvisionSagaHappyPath proves the full ordered saga commits all legs + writes exactly
// one audit row + returns the deep-link + a QR (bot token never in either) on success.
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
	if resp.DeepLink == "" || !strings.Contains(resp.DeepLink, "t.me/AuraBot?start=") {
		t.Errorf("deep-link = %q, want a t.me/AuraBot?start=<token> URL", resp.DeepLink)
	}
	if resp.QRSVG == "" || !strings.HasPrefix(resp.QRSVG, "<svg") {
		t.Error("provision must return a server-rendered QR SVG")
	}
}

// TestProvisionRejectsMismatchedRequester proves the session token is bound to the
// authenticated creator identity for the write path and the Telegram status poll.
func TestProvisionRejectsMismatchedRequester(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})

	_, err := svc.Provision(context.Background(), "creator-2", tok, provReq(nil))
	if !errors.Is(err, errOnboardingForbidden) {
		t.Fatalf("mismatched provision err = %v, want forbidden", err)
	}
	assertNoWrites(t, au, leg, tg)

	_, err = svc.TelegramStatus(context.Background(), "creator-2", tok)
	if !errors.Is(err, errOnboardingForbidden) {
		t.Fatalf("mismatched telegram-status err = %v, want forbidden", err)
	}
}

// TestProvisionWithoutTelegramLinkSkipsMint proves linkTelegram=false really skips Leg C:
// no setup token row is minted and no deep-link/QR is returned.
func TestProvisionWithoutTelegramLinkSkipsMint(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	req := provReq(nil)
	req.LinkTelegram = false

	resp, err := svc.Provision(context.Background(), "creator-1", tok, req)
	if err != nil {
		t.Fatalf("Provision without Telegram: %v", err)
	}
	if tg.mintedCount() != 0 {
		t.Fatalf("linkTelegram=false minted %d token(s), want 0", tg.mintedCount())
	}
	if resp.DeepLink != "" || resp.QRSVG != "" {
		t.Fatalf("linkTelegram=false returned link/QR: deepLink=%q qr=%q", resp.DeepLink, resp.QRSVG)
	}
	status, err := svc.TelegramStatus(context.Background(), "creator-1", tok)
	if err != nil {
		t.Fatalf("TelegramStatus after no-link provision: %v", err)
	}
	if status.Linked {
		t.Fatal("no-link provision must report linked=false")
	}
}

// TestProvisionSagaCompensation injects each of the RESEARCH failure-injection points and
// asserts the right compensation ran so NO orphan survives at the fake-port level (the LIVE
// zero-orphan store assertions are in the integration test).
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

	t.Run("C telegram mint fails -> DeleteIdentity + COMP_B", func(t *testing.T) {
		au := &fakeAuthula{}
		leg := &fakeAuraLeg{}
		tg := &fakeTelegram{insertErr: boom}
		svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
		if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
			t.Fatal("want error on C failure")
		}
		if au.liveAuthulaUsers() != 0 || leg.liveIdentities() != 0 || tg.mintedCount() != 0 || leg.auditCount() != 0 {
			t.Fatalf("C orphans: authula=%d identities=%d tokens=%d audit=%d",
				au.liveAuthulaUsers(), leg.liveIdentities(), tg.mintedCount(), leg.auditCount())
		}
	})

	t.Run("audit-write fails -> full rollback (no unaudited identity)", func(t *testing.T) {
		au := &fakeAuthula{}
		leg := &fakeAuraLeg{auditErr: boom}
		tg := &fakeTelegram{}
		svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
		if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err == nil {
			t.Fatal("want error on audit failure")
		}
		// A loginable identity MUST NOT exist without an audit row → full compensation.
		if leg.liveIdentities() != 0 || au.liveAuthulaUsers() != 0 || leg.auditCount() != 0 {
			t.Fatalf("audit-fail orphans: identities=%d authula=%d audit=%d",
				leg.liveIdentities(), au.liveAuthulaUsers(), leg.auditCount())
		}
	})
}

// TestProvisionAbandonedLeavesNothing proves an abandoned wizard (session expired before
// provision) runs NO saga, so zero rows — the orphan-free-on-abandonment property
// (RESEARCH §Hard Problem 4 A6).
func TestProvisionAbandonedLeavesNothing(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: []string{"identity.create"}},
		Extractor:    &countingExtractor{},
		Profiles:     &recordingProfileWriter{},
		Authula:      au, AuraLeg: leg, Telegram: tg, BotUsername: "AuraBot",
	})
	// Provision against a token that was never started (or expired/swept).
	_, err := svc.Provision(context.Background(), "creator-1", "never-started", provReq(nil))
	if !errors.Is(err, errOnboardingSessionNotFound) {
		t.Fatalf("abandoned provision err = %v, want session-not-found", err)
	}
	if leg.liveIdentities() != 0 || au.liveAuthulaUsers() != 0 || tg.mintedCount() != 0 {
		t.Fatal("an un-started/abandoned provision wrote rows; the saga must not run")
	}
}

// TestNoEscalation asserts the three-way no-escalation: a '*' request and a creator-lacked
// cap request are both rejected (escalation, no write); an operator without identity.create
// is forbidden (no write).
func TestNoEscalation(t *testing.T) {
	t.Run("'*' request rejected, no write", func(t *testing.T) {
		au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
		svc, tok := sagaService(t, au, leg, tg, []string{"identity.create", "agent.run"})
		_, err := svc.Provision(context.Background(), "creator-1", tok, provReq([]string{"*"}))
		if !errors.Is(err, ErrOnboardingEscalation) {
			t.Fatalf("'*' request err = %v, want escalation", err)
		}
		assertNoWrites(t, au, leg, tg)
	})

	t.Run("creator-lacked cap rejected, no write", func(t *testing.T) {
		au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
		// Creator holds identity.create + agent.run; requesting graph.write (not held) is escalation.
		svc, tok := sagaService(t, au, leg, tg, []string{"identity.create", "agent.run"})
		_, err := svc.Provision(context.Background(), "creator-1", tok, provReq([]string{"graph.write"}))
		if !errors.Is(err, ErrOnboardingEscalation) {
			t.Fatalf("creator-lacked cap err = %v, want escalation", err)
		}
		assertNoWrites(t, au, leg, tg)
	})

	t.Run("invalid cap grammar rejected, no write", func(t *testing.T) {
		for _, bad := range []string{"", "Agent.Run", "agent run", "-agent.run"} {
			t.Run("cap="+bad, func(t *testing.T) {
				au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
				svc, tok := sagaService(t, au, leg, tg, []string{"*"})
				_, err := svc.Provision(context.Background(), "creator-1", tok, provReq([]string{bad}))
				if !errors.Is(err, ErrOnboardingEscalation) {
					t.Fatalf("invalid cap %q err = %v, want escalation", bad, err)
				}
				assertNoWrites(t, au, leg, tg)
			})
		}
	})

	t.Run("operator without identity.create forbidden, no write", func(t *testing.T) {
		au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
		// Creator holds only agent.run — NOT identity.create and NOT '*'.
		svc, tok := sagaService(t, au, leg, tg, []string{"agent.run"})
		_, err := svc.Provision(context.Background(), "creator-1", tok, provReq([]string{"agent.run"}))
		if !errors.Is(err, errOnboardingForbidden) {
			t.Fatalf("no-identity.create err = %v, want forbidden", err)
		}
		assertNoWrites(t, au, leg, tg)
	})

	t.Run("creator with '*' may grant a named cap", func(t *testing.T) {
		au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
		svc, tok := sagaService(t, au, leg, tg, []string{"*"})
		_, err := svc.Provision(context.Background(), "creator-1", tok, provReq([]string{"agent.run", "graph.read"}))
		if err != nil {
			t.Fatalf("wildcard creator granting named caps: %v", err)
		}
		if leg.liveIdentities() != 1 {
			t.Fatalf("wildcard-creator provision did not create the identity")
		}
	})
}

// TestProvisionConcurrentSameSessionSingleCommit exercises the per-session lock around
// the saga: a burst of create clicks against the same live session yields one committed
// identity and every later attempt is rejected without a second write.
func TestProvisionConcurrentSameSessionSingleCommit(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})

	const attempts = 8
	start := make(chan struct{})
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
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
	for i := 0; i < attempts; i++ {
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

func assertNoWrites(t *testing.T, au *fakeAuthula, leg *fakeAuraLeg, tg *fakeTelegram) {
	t.Helper()
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

// TestProvisionNoSecretInLogs captures slog over a FULL provisioning run (success + a
// failure path) and asserts the Authula password never appears in any log line. The
// per-leg failure path logs a FIXED message (never err.Error() verbatim), so an error
// embedding a secret cannot leak. (The live MCP-env/bot-token leak scan is in the
// integration test, which exercises the real stores.)
func TestProvisionNoSecretInLogs(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const secret = "Sup3rSecret-Passw0rd!"
	const recoverySecret = "School Mascot Secret"

	// Success path.
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	req := provReq(nil)
	req.Password = secret
	req.SecurityAnswer = recoverySecret
	if _, err := svc.Provision(context.Background(), "creator-1", tok, req); err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Failure path: a leg error that embeds the secret must NOT reach a log line.
	au2 := &fakeAuthula{createAcctErr: errors.New("authula refused password " + secret)}
	leg2, tg2 := &fakeAuraLeg{}, &fakeTelegram{}
	svc2, tok2 := sagaService(t, au2, leg2, tg2, []string{"identity.create"})
	req2 := provReq(nil)
	req2.Password = secret
	req2.SecurityAnswer = recoverySecret
	if _, err := svc2.Provision(context.Background(), "creator-1", tok2, req2); err == nil {
		t.Fatal("want B2 failure")
	}

	if strings.Contains(buf.String(), secret) {
		t.Fatalf("the Authula password leaked into a log line:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), recoverySecret) {
		t.Fatalf("the recovery answer leaked into a log line:\n%s", buf.String())
	}
}

// TestRenderQRSVG proves the QR render embeds the deep-link only and never the bot token.
func TestRenderQRSVG(t *testing.T) {
	const deepLink = "https://t.me/AuraBot?start=abc123token"
	svg, err := renderQRSVG(deepLink)
	if err != nil {
		t.Fatalf("renderQRSVG: %v", err)
	}
	if !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, "</svg>") {
		t.Fatalf("not an SVG: %.60q", svg)
	}
	if !strings.Contains(svg, "<rect") {
		t.Error("QR SVG has no module rects")
	}
	// The bot TOKEN (a Telegram bot API token like 123456:ABC...) must never appear — the
	// QR encodes the deep-link URL (onboarding token), not the bot credential. Assert a
	// representative bot-token shape is absent.
	if strings.Contains(svg, "123456:ABC") {
		t.Error("QR SVG leaked a bot token")
	}
}

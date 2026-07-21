package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/onboarding"
)

// onboarding_api_test.go covers the interview side (Task 1): the one-LLM-extraction-per-
// step guarantee (TestNoDuplicatePrompt), the D-06 capability picker (creator grants minus
// '*'), the skip/empty-answer behaviors, and the thin REST handlers (start/step) through
// the real Server.Mux with a fake service. The provisioning saga is covered in
// onboarding_provision_test.go.

// countingExtractor is a call-counting answerExtractor: it records how many times Extract
// is invoked so the no-duplicate-LLM-turn property is provable. It returns a fixed Answers
// keyed off the step so the session advances deterministically.
type countingExtractor struct {
	mu    sync.Mutex
	calls int
}

func (e *countingExtractor) Extract(_ context.Context, step onboarding.Step, raw string) (onboarding.Answers, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	switch step {
	case onboarding.StepIdentity:
		return onboarding.Answers{Name: "Davide"}, nil
	case onboarding.StepWork:
		return onboarding.Answers{Expertise: []string{"backend"}, Stack: []string{"Go"}}, nil
	case onboarding.StepProjects:
		return onboarding.Answers{Projects: []string{"Aura"}}, nil
	case onboarding.StepSocial:
		return onboarding.Answers{Interests: []string{"AI"}}, nil
	case onboarding.StepStyle:
		return onboarding.Answers{TonePreference: "friendly", ResponseLength: "normal"}, nil
	default:
		return onboarding.Answers{CustomInstructions: raw}, nil
	}
}

func (e *countingExtractor) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// fakeCaps is a CapabilitySource returning a fixed grant set for any identity.
type fakeCaps struct {
	grants []string
	err    error
}

func (f fakeCaps) ListCapabilities(_ context.Context, _ string) ([]string, error) {
	return f.grants, f.err
}

// recordingProfileWriter records profile-store calls (by identity) so "skip writes no
// profile" and "confirm writes one" stay provable. Both StoreConfirmed and StoreSkipped
// count as a write (each persists a sentinel); a no-draft persistProfile writes nothing.
type recordingProfileWriter struct {
	writes []string
}

func (w *recordingProfileWriter) StoreConfirmed(_ context.Context, identity string, _ onboarding.Answers, _ string) error {
	w.writes = append(w.writes, identity)
	return nil
}

func (w *recordingProfileWriter) StoreSkipped(_ context.Context, identity string) error {
	w.writes = append(w.writes, identity)
	return nil
}

func (w *recordingProfileWriter) Status(context.Context, string) (onboarding.OnboardingState, error) {
	return onboarding.OnboardingState{}, nil
}

func newInterviewService(ext AnswerExtractor, caps CapabilitySource, pw onboarding.ProfileMemoryStore) *onboardingService {
	return newOnboardingService(OnboardingDeps{
		Capabilities: caps,
		Extractor:    ext,
		Profiles:     pw,
	})
}

// TestNoDuplicatePrompt proves exactly one LLM extraction per free-text answer, no second
// turn on replay, and an edit re-renders the draft without a re-prompt (mock the LLM,
// count calls). This is the core ONBD-02 / RESEARCH §Hard Problem 4 guarantee.
func TestNoDuplicatePrompt(t *testing.T) {
	ext := &countingExtractor{}
	svc := newInterviewService(ext, fakeCaps{grants: []string{"agent.run"}}, &recordingProfileWriter{})
	ctx := context.Background()

	start, err := svc.StartSession(ctx, "creator-1")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	tok := start.SessionToken

	// Walk the 5 interview steps with a free-text answer each → exactly 5 extractions.
	for i, text := range []string{"Davide", "backend dev, Go", "Aura", "AI agents", "friendly, short"} {
		resp, err := svc.Step(ctx, "creator-1", tok, OnboardingStepRequest{Intent: "answer", Text: text})
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		_ = resp
	}
	if got := ext.count(); got != 5 {
		t.Fatalf("LLM extractions = %d, want exactly 5 (one per free-text answer)", got)
	}

	// We are now at the draft step (StatusDraft). An edit must NOT trigger a 6th extraction
	// (it re-renders deterministically from the SAME answers).
	editResp, err := svc.Step(ctx, "creator-1", tok, OnboardingStepRequest{
		Intent:  "edit",
		Answers: &OnboardingAnswers{TonePreference: "direct"},
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := ext.count(); got != 5 {
		t.Fatalf("after edit LLM extractions = %d, want still 5 (edit must not re-prompt)", got)
	}
	if strings.TrimSpace(editResp.Draft) == "" {
		t.Error("edit must return a re-rendered draft")
	}

	// A confirm completes without any extraction.
	confirmResp, err := svc.Step(ctx, "creator-1", tok, OnboardingStepRequest{Intent: "confirm"})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if got := ext.count(); got != 5 {
		t.Fatalf("after confirm LLM extractions = %d, want still 5", got)
	}
	if confirmResp.Status != string(onboarding.StatusCompleted) {
		t.Errorf("confirm status = %q, want %q", confirmResp.Status, onboarding.StatusCompleted)
	}
}

// TestOnboardingStartCapabilityOptions proves the picker excludes '*': a creator holding
// '*' plus two named caps offers only the two named caps (no-escalation D-06).
func TestOnboardingStartCapabilityOptions(t *testing.T) {
	svc := newInterviewService(&countingExtractor{},
		fakeCaps{grants: []string{"*", "agent.run", "graph.read"}}, &recordingProfileWriter{})
	start, err := svc.StartSession(context.Background(), "creator-1")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	want := map[string]bool{"agent.run": true, "graph.read": true}
	if len(start.CapabilityOptions) != 2 {
		t.Fatalf("capabilityOptions = %v, want exactly the 2 named caps (no '*')", start.CapabilityOptions)
	}
	for _, c := range start.CapabilityOptions {
		if c == "*" {
			t.Fatal("capabilityOptions leaked the '*' wildcard")
		}
		if !want[c] {
			t.Errorf("unexpected capability option %q", c)
		}
	}
	if start.Step != string(onboarding.StepIdentity) {
		t.Errorf("first step = %q, want %q", start.Step, onboarding.StepIdentity)
	}
	if strings.TrimSpace(start.Content) == "" {
		t.Error("start must return the first prompt content")
	}
}

// TestOnboardingStepSkip proves a skip ends the interview (StatusSkipped) and writes no
// profile (the file write happens only on a confirmed interview at provision time).
func TestOnboardingStepSkip(t *testing.T) {
	pw := &recordingProfileWriter{}
	svc := newInterviewService(&countingExtractor{}, fakeCaps{grants: []string{"agent.run"}}, pw)
	ctx := context.Background()
	start, err := svc.StartSession(ctx, "creator-1")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	resp, err := svc.Step(ctx, "creator-1", start.SessionToken, OnboardingStepRequest{Intent: "skip"})
	if err != nil {
		t.Fatalf("skip: %v", err)
	}
	if resp.Status != string(onboarding.StatusSkipped) {
		t.Errorf("skip status = %q, want %q", resp.Status, onboarding.StatusSkipped)
	}
	if len(pw.writes) != 0 {
		t.Errorf("skip wrote a profile (%v); skip must write none", pw.writes)
	}
}

func TestProfileOnboardingCompleteWritesCurrentIdentityProfile(t *testing.T) {
	pw := &recordingProfileWriter{}
	tg := &fakeTelegram{}
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: []string{"agent.run"}},
		Extractor:    &countingExtractor{},
		Profiles:     pw,
		Telegram:     tg,
		BotUsername:  "AuraBot",
	})
	ctx := context.Background()
	start, err := svc.StartProfileSession(ctx, "operator-1")
	if err != nil {
		t.Fatalf("StartProfileSession: %v", err)
	}
	for _, text := range []string{"Davide", "backend dev, Go", "Aura", "AI agents", "friendly, short"} {
		if _, err := svc.Step(ctx, "operator-1", start.SessionToken, OnboardingStepRequest{Intent: "answer", Text: text}); err != nil {
			t.Fatalf("answer: %v", err)
		}
	}
	if _, err := svc.Step(ctx, "operator-1", start.SessionToken, OnboardingStepRequest{Intent: "confirm"}); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	done, err := svc.CompleteProfile(ctx, "operator-1", start.SessionToken)
	if err != nil {
		t.Fatalf("CompleteProfile: %v", err)
	}
	if !done.Completed || done.Skipped {
		t.Fatalf("CompleteProfile = %+v, want completed", done)
	}
	if !strings.HasPrefix(done.DeepLink, "https://t.me/AuraBot?start=") || strings.TrimSpace(done.QRSVG) == "" {
		t.Fatalf("CompleteProfile Telegram link = deepLink:%q qr:%t, want deep-link and QR", done.DeepLink, strings.TrimSpace(done.QRSVG) != "")
	}
	if tg.mintedCount() != 1 {
		t.Fatalf("telegram pending tokens = %d, want 1", tg.mintedCount())
	}
	if len(pw.writes) != 1 || pw.writes[0] != "operator-1" {
		t.Fatalf("profile writes = %v, want current operator", pw.writes)
	}
}

func TestProfileOnboardingCompleteWritesSkippedMetadata(t *testing.T) {
	pw := &recordingProfileWriter{}
	tg := &fakeTelegram{}
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: []string{"agent.run"}},
		Extractor:    &countingExtractor{},
		Profiles:     pw,
		Telegram:     tg,
		BotUsername:  "AuraBot",
	})
	ctx := context.Background()
	start, err := svc.StartProfileSession(ctx, "operator-1")
	if err != nil {
		t.Fatalf("StartProfileSession: %v", err)
	}
	if _, err := svc.Step(ctx, "operator-1", start.SessionToken, OnboardingStepRequest{Intent: "skip"}); err != nil {
		t.Fatalf("skip: %v", err)
	}
	done, err := svc.CompleteProfile(ctx, "operator-1", start.SessionToken)
	if err != nil {
		t.Fatalf("CompleteProfile: %v", err)
	}
	if done.Completed || !done.Skipped {
		t.Fatalf("CompleteProfile = %+v, want skipped", done)
	}
	if !strings.HasPrefix(done.DeepLink, "https://t.me/AuraBot?start=") || strings.TrimSpace(done.QRSVG) == "" {
		t.Fatalf("CompleteProfile Telegram link = deepLink:%q qr:%t, want deep-link and QR", done.DeepLink, strings.TrimSpace(done.QRSVG) != "")
	}
	if tg.mintedCount() != 1 {
		t.Fatalf("telegram pending tokens = %d, want 1", tg.mintedCount())
	}
	if len(pw.writes) != 1 || pw.writes[0] != "operator-1" {
		t.Fatalf("profile writes = %v, want current operator skip marker", pw.writes)
	}
}

func TestCreateTelegramLinkMintsCurrentIdentityRecoveryQR(t *testing.T) {
	tg := &fakeTelegram{}
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: []string{"agent.run"}},
		Extractor:    &countingExtractor{},
		Profiles:     &recordingProfileWriter{},
		Telegram:     tg,
		BotUsername:  "AuraBot",
	})

	link, err := svc.CreateTelegramLink(context.Background(), "operator-1")
	if err != nil {
		t.Fatalf("CreateTelegramLink: %v", err)
	}
	if link.SessionToken == "" {
		t.Fatal("CreateTelegramLink returned no poll session token")
	}
	if !strings.HasPrefix(link.DeepLink, "https://t.me/AuraBot?start=") || strings.TrimSpace(link.QRSVG) == "" {
		t.Fatalf("CreateTelegramLink = deepLink:%q qr:%t, want deep-link and QR", link.DeepLink, strings.TrimSpace(link.QRSVG) != "")
	}
	if tg.mintedCount() != 1 {
		t.Fatalf("telegram pending tokens = %d, want 1", tg.mintedCount())
	}

	status, err := svc.TelegramStatus(context.Background(), "operator-1", link.SessionToken)
	if err != nil {
		t.Fatalf("TelegramStatus before consume: %v", err)
	}
	if status.Linked {
		t.Fatal("TelegramStatus before consume = linked, want false")
	}
	tg.consumed = true
	status, err = svc.TelegramStatus(context.Background(), "operator-1", link.SessionToken)
	if err != nil {
		t.Fatalf("TelegramStatus after consume: %v", err)
	}
	if !status.Linked {
		t.Fatal("TelegramStatus after consume = false, want linked")
	}
	if _, err := svc.TelegramStatus(context.Background(), "operator-2", link.SessionToken); !errors.Is(err, errOnboardingForbidden) {
		t.Fatalf("mismatched requester status err = %v, want forbidden", err)
	}
}

// TestOnboardingStepEmptyAnswer proves an empty answer is recorded without error and
// without an LLM extraction (the session no-ops on empty).
func TestOnboardingStepEmptyAnswer(t *testing.T) {
	ext := &countingExtractor{}
	svc := newInterviewService(ext, fakeCaps{grants: []string{"agent.run"}}, &recordingProfileWriter{})
	ctx := context.Background()
	start, err := svc.StartSession(ctx, "creator-1")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	resp, err := svc.Step(ctx, "creator-1", start.SessionToken, OnboardingStepRequest{Intent: "answer", Text: "   "})
	if err != nil {
		t.Fatalf("empty answer: %v", err)
	}
	if ext.count() != 0 {
		t.Errorf("empty answer triggered %d extraction(s), want 0", ext.count())
	}
	// The step still advances (an empty identity answer moves to the work step).
	if resp.Step != string(onboarding.StepWork) {
		t.Errorf("after empty identity answer step = %q, want %q", resp.Step, onboarding.StepWork)
	}
}

// TestOnboardingStepUnknownSession proves a missing/expired token is the typed
// session-not-found sentinel (mapped to 404 by the handler).
func TestOnboardingStepUnknownSession(t *testing.T) {
	svc := newInterviewService(&countingExtractor{}, fakeCaps{grants: []string{"agent.run"}}, &recordingProfileWriter{})
	_, err := svc.Step(context.Background(), "creator-1", "nope", OnboardingStepRequest{Intent: "answer", Text: "x"})
	if err == nil {
		t.Fatal("unknown session must error")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("err = %v, want session-not-found", err)
	}
}

// TestOnboardingStepRejectsMismatchedRequester proves the opaque session token is not a
// bearer credential by itself: the current authenticated principal must match the
// identity that started the session, and a rejected request must not invoke extraction.
func TestOnboardingStepRejectsMismatchedRequester(t *testing.T) {
	ext := &countingExtractor{}
	svc := newInterviewService(ext, fakeCaps{grants: []string{"agent.run"}}, &recordingProfileWriter{})
	start, err := svc.StartSession(context.Background(), "creator-1")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	_, err = svc.Step(context.Background(), "creator-2", start.SessionToken, OnboardingStepRequest{Intent: "answer", Text: "x"})
	if !errors.Is(err, errOnboardingForbidden) {
		t.Fatalf("mismatched requester err = %v, want forbidden", err)
	}
	if ext.count() != 0 {
		t.Fatalf("mismatched requester invoked extraction %d time(s), want 0", ext.count())
	}
}

// onboardingTestServer builds a Server with the fake onboarding service wired and the
// principal stashed (RequireAuth is bypassed in the test mux; we set the principal so the
// start handler reads a creator).
func onboardingTestServer(t *testing.T, svc OnboardingService) http.Handler {
	t.Helper()
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetOnboardingService(svc)
	// Wrap the mux so every request carries an authenticated principal (the parent-mux
	// RequireAuth does this in production; the agui Server.Mux assumes it upstream).
	return withTestPrincipal(s.Mux(), "00000000-0000-0000-0000-000000000001")
}

func withTestPrincipal(next http.Handler, id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, withPrincipal(r, id))
	})
}

// TestOnboardingStartHandler503 proves an unwired service answers 503 (the DI seam).
func TestOnboardingStartHandler503(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	srv := httptest.NewServer(withTestPrincipal(s.Mux(), "00000000-0000-0000-0000-000000000001"))
	t.Cleanup(srv.Close)
	resp, err := http.Post(srv.URL+"/api/onboarding/start", "application/json", nil)
	if err != nil {
		t.Fatalf("POST start: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unwired start = %d, want 503", resp.StatusCode)
	}
}

// TestOnboardingStartHandler proves the start handler returns the service's start payload.
func TestOnboardingStartHandler(t *testing.T) {
	fake := &fakeOnboarding{start: OnboardingStart{
		SessionToken: "tok-1", Step: "identity", Content: "hi", Status: "active",
		CapabilityOptions: []string{"agent.run"},
	}}
	srv := httptest.NewServer(onboardingTestServer(t, fake))
	t.Cleanup(srv.Close)
	resp, err := http.Post(srv.URL+"/api/onboarding/start", "application/json", nil)
	if err != nil {
		t.Fatalf("POST start: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start = %d, want 200", resp.StatusCode)
	}
	var got OnboardingStart
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionToken != "tok-1" || len(got.CapabilityOptions) != 1 {
		t.Errorf("start payload = %+v", got)
	}
}

// TestOnboardingStepHandlerValidation proves the step handler rejects an unknown intent
// (400) and routes the {sessionToken} path param to the service.
func TestOnboardingStepHandlerValidation(t *testing.T) {
	fake := &fakeOnboarding{stepResp: OnboardingStepResponse{Content: "next", Step: "work", Status: "active"}}
	srv := httptest.NewServer(onboardingTestServer(t, fake))
	t.Cleanup(srv.Close)

	// Unknown intent → 400, service not called.
	badResp, err := http.Post(srv.URL+"/api/onboarding/tok-9/step", "application/json",
		strings.NewReader(`{"intent":"bogus"}`))
	if err != nil {
		t.Fatalf("POST step bad: %v", err)
	}
	defer badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown intent = %d, want 400", badResp.StatusCode)
	}

	// Valid intent → 200, token routed.
	okResp, err := http.Post(srv.URL+"/api/onboarding/tok-9/step", "application/json",
		strings.NewReader(`{"intent":"answer","text":"hi"}`))
	if err != nil {
		t.Fatalf("POST step ok: %v", err)
	}
	defer okResp.Body.Close()
	if okResp.StatusCode != http.StatusOK {
		t.Fatalf("valid step = %d, want 200", okResp.StatusCode)
	}
	if fake.gotToken != "tok-9" || fake.gotIntent != "answer" || fake.gotRequester != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("service got requester=%q token=%q intent=%q, want principal/tok-9/answer", fake.gotRequester, fake.gotToken, fake.gotIntent)
	}
}

// TestOnboardingStepHandlerNotFound proves the session-not-found sentinel maps to 404.
func TestOnboardingStepHandlerNotFound(t *testing.T) {
	fake := &fakeOnboarding{stepErr: errOnboardingSessionNotFound}
	srv := httptest.NewServer(onboardingTestServer(t, fake))
	t.Cleanup(srv.Close)
	resp, err := http.Post(srv.URL+"/api/onboarding/gone/step", "application/json",
		strings.NewReader(`{"intent":"answer","text":"hi"}`))
	if err != nil {
		t.Fatalf("POST step: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expired session = %d, want 404", resp.StatusCode)
	}
}

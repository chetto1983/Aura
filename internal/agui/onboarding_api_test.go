package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/onboarding"
)

// onboarding_api_test.go covers the seed side (Amendment #95): the D-06 capability picker
// (creator grants minus '*'), the stateless seed submit (SubmitProfile) including the
// derived skip, and the thin REST handlers through the real Server.Mux with a fake
// service. The provisioning saga is covered in onboarding_provision_test.go.

// fakeCaps is a CapabilitySource returning a fixed grant set for any identity.
type fakeCaps struct {
	grants []string
	err    error
}

func (f fakeCaps) ListCapabilities(_ context.Context, _ string) ([]string, error) {
	return f.grants, f.err
}

// recordingProfileWriter records profile-store calls (by identity) plus the Answers the
// mapper will consume, so "a blank seed writes only the skip sentinel", "the write is
// scoped to the caller" and "the name is byte-identical to the one typed" stay provable.
type recordingProfileWriter struct {
	writes    []string
	confirmed []onboarding.Answers
	skipped   []string
	err       error
}

func (w *recordingProfileWriter) StoreConfirmed(_ context.Context, identity string, a onboarding.Answers) error {
	if w.err != nil {
		return w.err
	}
	w.writes = append(w.writes, identity)
	w.confirmed = append(w.confirmed, a)
	return nil
}

func (w *recordingProfileWriter) StoreSkipped(_ context.Context, identity string) error {
	if w.err != nil {
		return w.err
	}
	w.writes = append(w.writes, identity)
	w.skipped = append(w.skipped, identity)
	return nil
}

func (w *recordingProfileWriter) Status(context.Context, string) (onboarding.OnboardingState, error) {
	return onboarding.OnboardingState{}, nil
}

// newSeedService builds the concrete service with only the seed-side ports wired (plus the
// Telegram mint SubmitProfile requires), mirroring a deployment without the provisioning
// saga.
func newSeedService(caps CapabilitySource, pw onboarding.ProfileMemoryStore, tg TelegramMint) *onboardingService {
	return newOnboardingService(OnboardingDeps{
		Capabilities: caps,
		Profiles:     pw,
		Telegram:     tg,
		BotUsername:  "AuraBot",
	})
}

// fullSeed is the amendment's worked example: every field of the typed form filled.
var fullSeed = OnboardingSeed{
	Name: "Davide", Lang: "it", Location: "Caraglio",
	Timezone: "Europe/Rome", Role: "founder", Company: "PmSync",
}

// TestOnboardingStartCapabilityOptions proves the picker excludes '*': a creator holding
// '*' plus two named caps offers only the two named caps (no-escalation D-06). It also
// pins that start carries nothing but the token + options — the interview's step/content/
// status fields are gone (Amendment #95).
func TestOnboardingStartCapabilityOptions(t *testing.T) {
	svc := newSeedService(fakeCaps{grants: []string{"*", "agent.run", "graph.read"}}, &recordingProfileWriter{}, &fakeTelegram{})
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
	if start.SessionToken == "" {
		t.Error("start must mint a session token")
	}
}

// TestSeedToAnswersMapsEverySixFields pins the wire→domain mapping field by field, and
// that it is a straight copy: an LLM-free path means the stored name is byte-identical to
// the typed one (the defect Amendment #95 exists for).
func TestSeedToAnswersMapsEverySixFields(t *testing.T) {
	got := fullSeed.toAnswers()
	want := onboarding.Answers{
		Name: "Davide", Role: "founder", Company: "PmSync",
		Location: "Caraglio", Lang: "it", Timezone: "Europe/Rome",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("toAnswers = %+v, want %+v", got, want)
	}
	// Every other Answers field must stay zero-valued: MapProfile's empty-field guards are
	// what keep the submission at nine writes.
	if len(got.Expertise)+len(got.Stack)+len(got.Projects)+len(got.Goals)+len(got.Interests)+len(got.People)+len(got.Vetoes) != 0 {
		t.Errorf("toAnswers populated a non-seed slice field: %+v", got)
	}
	if got.TonePreference != "" || got.ResponseLength != "" || got.CustomInstructions != "" ||
		got.VoiceMode != nil || got.CanProactiveMessage != nil {
		t.Errorf("toAnswers populated a non-seed style field: %+v", got)
	}
}

// TestSeedToAnswersPreservesNameVerbatim proves nothing on the path trims, folds or
// rewrites the typed name — including the surrounding whitespace and accents an LLM
// extractor would have normalized away.
func TestSeedToAnswersPreservesNameVerbatim(t *testing.T) {
	for _, name := range []string{"Davide", " Davide ", "DAVIDE", "José-María", "李雷"} {
		if got := (OnboardingSeed{Name: name}).toAnswers().Name; got != name {
			t.Errorf("toAnswers(%q).Name = %q, want byte-identical", name, got)
		}
	}
}

// TestOnboardingSeedBlank pins the derived skip signal: only an all-whitespace seed is a
// skip, and any single filled field makes it a confirm.
func TestOnboardingSeedBlank(t *testing.T) {
	if !(OnboardingSeed{}).blank() {
		t.Error("zero seed must be blank")
	}
	if !(OnboardingSeed{Name: "  ", Lang: "\t", Location: " ", Timezone: " ", Role: " ", Company: " "}).blank() {
		t.Error("all-whitespace seed must be blank")
	}
	for name, seed := range map[string]OnboardingSeed{
		"name":     {Name: "Davide"},
		"lang":     {Lang: "it"},
		"location": {Location: "Caraglio"},
		"timezone": {Timezone: "Europe/Rome"},
		"role":     {Role: "founder"},
		"company":  {Company: "PmSync"},
	} {
		if seed.blank() {
			t.Errorf("seed with only %s set must not be blank", name)
		}
	}
}

// TestValidateOnboardingSeedRejectsOverlongFields proves every field is capped, so an
// oversized body is a clean 400 rather than an entity name nobody can read back.
func TestValidateOnboardingSeedRejectsOverlongFields(t *testing.T) {
	if err := validateOnboardingSeed(fullSeed); err != nil {
		t.Fatalf("valid seed rejected: %v", err)
	}
	cases := map[string]OnboardingSeed{
		"name":     {Name: strings.Repeat("a", onboardingSeedFieldMaxLen+1)},
		"role":     {Role: strings.Repeat("a", onboardingSeedFieldMaxLen+1)},
		"company":  {Company: strings.Repeat("a", onboardingSeedFieldMaxLen+1)},
		"location": {Location: strings.Repeat("a", onboardingSeedFieldMaxLen+1)},
		"lang":     {Lang: strings.Repeat("a", onboardingLangMaxLen+1)},
		"timezone": {Timezone: strings.Repeat("a", onboardingTimezoneMaxLen+1)},
	}
	for field, seed := range cases {
		if err := validateOnboardingSeed(seed); err == nil {
			t.Errorf("overlong %s accepted, want rejection", field)
		}
	}
	// The cap is inclusive: exactly-at-the-limit is valid.
	if err := validateOnboardingSeed(OnboardingSeed{Name: strings.Repeat("a", onboardingSeedFieldMaxLen)}); err != nil {
		t.Errorf("name at the cap rejected: %v", err)
	}
}

// TestValidateOnboardingSeedRequiresANameOnceAnythingIsFilled pins the one conditional
// requirement. MapProfile keys every fact off the name and falls back to the placeholder
// subject "operator" when it is empty, so a nameless-but-filled seed would write
// `operator works_for PmSync` with no PERSON entity for it to resolve against — the
// orphan shape Amendment #95 exists to end. A blank seed stays valid: that is the skip.
func TestValidateOnboardingSeedRequiresANameOnceAnythingIsFilled(t *testing.T) {
	if err := validateOnboardingSeed(OnboardingSeed{}); err != nil {
		t.Fatalf("a blank seed is the skip and must stay valid: %v", err)
	}
	for name, seed := range map[string]OnboardingSeed{
		"role only":     {Role: "founder"},
		"company only":  {Company: "PmSync"},
		"location only": {Location: "Caraglio"},
		"lang only":     {Lang: "it"},
		"blank name":    {Name: "   ", Role: "founder"},
	} {
		if err := validateOnboardingSeed(seed); err == nil {
			t.Errorf("%s was accepted without a name — those facts would land under the placeholder subject", name)
		}
	}
	if err := validateOnboardingSeed(OnboardingSeed{Name: "Davide", Role: "founder"}); err != nil {
		t.Errorf("a named seed rejected: %v", err)
	}
}

// TestSubmitProfileWritesSeedUnderRequesterScope proves the full form is written for the
// AUTHENTICATED caller (never a token-carried identity), that the mapped Answers reach the
// store verbatim, and that the response carries the Telegram link the completion screen
// needs.
func TestSubmitProfileWritesSeedUnderRequesterScope(t *testing.T) {
	pw := &recordingProfileWriter{}
	tg := &fakeTelegram{}
	svc := newSeedService(fakeCaps{grants: []string{"agent.run"}}, pw, tg)

	done, err := svc.SubmitProfile(context.Background(), "operator-1", fullSeed)
	if err != nil {
		t.Fatalf("SubmitProfile: %v", err)
	}
	if !done.Completed || done.Skipped {
		t.Fatalf("SubmitProfile = %+v, want completed", done)
	}
	if !strings.HasPrefix(done.DeepLink, "https://t.me/AuraBot?start=") || strings.TrimSpace(done.QRSVG) == "" {
		t.Fatalf("Telegram link = deepLink:%q qr:%t, want deep-link and QR", done.DeepLink, strings.TrimSpace(done.QRSVG) != "")
	}
	if tg.mintedCount() != 1 {
		t.Fatalf("telegram pending tokens = %d, want 1", tg.mintedCount())
	}
	if len(pw.writes) != 1 || pw.writes[0] != "operator-1" {
		t.Fatalf("profile writes = %v, want exactly the current operator", pw.writes)
	}
	if len(pw.confirmed) != 1 || !reflect.DeepEqual(pw.confirmed[0], fullSeed.toAnswers()) {
		t.Fatalf("stored answers = %+v, want the submitted seed verbatim", pw.confirmed)
	}
	if len(pw.skipped) != 0 {
		t.Fatalf("a filled seed wrote the skip sentinel: %v", pw.skipped)
	}
}

// TestSubmitProfileBlankSeedWritesOnlySkipSentinel proves the derived skip: submitting
// nothing costs exactly one store call (the sentinel), so the agent still builds the
// profile from use.
func TestSubmitProfileBlankSeedWritesOnlySkipSentinel(t *testing.T) {
	pw := &recordingProfileWriter{}
	tg := &fakeTelegram{}
	svc := newSeedService(fakeCaps{grants: []string{"agent.run"}}, pw, tg)

	done, err := svc.SubmitProfile(context.Background(), "operator-1", OnboardingSeed{})
	if err != nil {
		t.Fatalf("SubmitProfile: %v", err)
	}
	if done.Completed || !done.Skipped {
		t.Fatalf("SubmitProfile = %+v, want skipped", done)
	}
	if !strings.HasPrefix(done.DeepLink, "https://t.me/AuraBot?start=") {
		t.Fatalf("skip must still mint the Telegram link, got %q", done.DeepLink)
	}
	if len(pw.confirmed) != 0 {
		t.Fatalf("blank seed wrote a profile: %+v", pw.confirmed)
	}
	if len(pw.skipped) != 1 || pw.skipped[0] != "operator-1" {
		t.Fatalf("skip sentinel writes = %v, want exactly the current operator", pw.skipped)
	}
}

// TestSubmitProfileReturnsPollableSessionToken proves the returned token resolves through
// TelegramStatus for the SAME identity and nobody else — that token is what replaces the
// deleted /profile/start round-trip.
func TestSubmitProfileReturnsPollableSessionToken(t *testing.T) {
	tg := &fakeTelegram{}
	svc := newSeedService(fakeCaps{grants: []string{"agent.run"}}, &recordingProfileWriter{}, tg)

	done, err := svc.SubmitProfile(context.Background(), "operator-1", fullSeed)
	if err != nil {
		t.Fatalf("SubmitProfile: %v", err)
	}
	if done.SessionToken == "" {
		t.Fatal("SubmitProfile returned no pollable session token")
	}
	status, err := svc.TelegramStatus(context.Background(), "operator-1", done.SessionToken)
	if err != nil {
		t.Fatalf("TelegramStatus before consume: %v", err)
	}
	if status.Linked {
		t.Fatal("TelegramStatus before consume = linked, want false")
	}
	tg.consumed = true
	if status, err = svc.TelegramStatus(context.Background(), "operator-1", done.SessionToken); err != nil || !status.Linked {
		t.Fatalf("TelegramStatus after consume = %+v err=%v, want linked", status, err)
	}
	if _, err := svc.TelegramStatus(context.Background(), "operator-2", done.SessionToken); !errors.Is(err, errOnboardingForbidden) {
		t.Fatalf("mismatched requester status err = %v, want forbidden", err)
	}
}

// TestSubmitProfileRejectsAnonymousAndUnwired proves the two guards ahead of any write: no
// authenticated principal is forbidden, and an unwired profile store is the sanitized
// backend-unavailable error — neither mints a Telegram token.
func TestSubmitProfileRejectsAnonymousAndUnwired(t *testing.T) {
	tg := &fakeTelegram{}
	svc := newSeedService(fakeCaps{grants: []string{"agent.run"}}, &recordingProfileWriter{}, tg)
	if _, err := svc.SubmitProfile(context.Background(), "", fullSeed); !errors.Is(err, errOnboardingForbidden) {
		t.Fatalf("anonymous submit err = %v, want forbidden", err)
	}

	bare := newSeedService(fakeCaps{grants: []string{"agent.run"}}, nil, tg)
	if _, err := bare.SubmitProfile(context.Background(), "operator-1", fullSeed); !errors.Is(err, errProvisioningUnavailable) {
		t.Fatalf("unwired profile store err = %v, want provisioning-unavailable", err)
	}
	if tg.mintedCount() != 0 {
		t.Fatalf("a rejected submit minted %d telegram token(s), want 0", tg.mintedCount())
	}
}

func TestCreateTelegramLinkMintsCurrentIdentityRecoveryQR(t *testing.T) {
	tg := &fakeTelegram{}
	svc := newSeedService(fakeCaps{grants: []string{"agent.run"}}, &recordingProfileWriter{}, tg)

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
		SessionToken:      "tok-1",
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

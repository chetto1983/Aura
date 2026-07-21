package telegram

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	profileflow "github.com/chetto1983/aura/internal/onboarding"
	tele "gopkg.in/telebot.v4"
)

func ptrBool(b bool) *bool { return &b }

// fakeMemoryStore is an in-memory profileflow.ProfileMemoryStore for the telegram
// onboarding tests (Amendment #87): it records the confirmed Answers / raw draft and
// the completed/skipped sentinel, replacing the filesystem profile.Store the
// pre-supersession tests read back. statusErr forces the Status read-error branch.
type fakeMemoryStore struct {
	mu        sync.Mutex
	states    map[string]profileflow.OnboardingState
	confirmed map[string]profileflow.Answers
	drafts    map[string]string
	statusErr error
}

func newFakeMemoryStore() *fakeMemoryStore {
	return &fakeMemoryStore{
		states:    map[string]profileflow.OnboardingState{},
		confirmed: map[string]profileflow.Answers{},
		drafts:    map[string]string{},
	}
}

func (f *fakeMemoryStore) StoreConfirmed(_ context.Context, identityID string, a profileflow.Answers, rawDraft string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[identityID] = profileflow.OnboardingState{Completed: true}
	f.confirmed[identityID] = a
	f.drafts[identityID] = rawDraft
	return nil
}

func (f *fakeMemoryStore) StoreSkipped(_ context.Context, identityID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[identityID] = profileflow.OnboardingState{Skipped: true}
	return nil
}

func (f *fakeMemoryStore) Status(_ context.Context, identityID string) (profileflow.OnboardingState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return profileflow.OnboardingState{}, f.statusErr
	}
	return f.states[identityID], nil
}

// markCompleted seeds the completed sentinel so a test can model an already-onboarded
// identity without driving the full interview.
func (f *fakeMemoryStore) markCompleted(identityID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[identityID] = profileflow.OnboardingState{Completed: true}
}

func (f *fakeMemoryStore) confirmedAnswers(identityID string) (profileflow.Answers, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.confirmed[identityID]
	return a, ok
}

func (f *fakeMemoryStore) state(identityID string) profileflow.OnboardingState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.states[identityID]
}

// driveToStyle sends blank answers for identity+work+projects+social, returning
// the last reply (which should be the style question). Returns false if any step
// fails or is not handled.
func driveToStyle(t *testing.T, po *profileOnboarding, chatID int64) bool {
	t.Helper()
	steps := []string{"Davide — dev", "", "", ""}
	for i, text := range steps {
		_, ok := po.handleText(context.Background(), chatID, text)
		if !ok {
			t.Errorf("step %d handleText not handled", i)
			return false
		}
	}
	return true
}

func TestProfileOnboardingConfirmWritesProfile(t *testing.T) {
	ctx := context.Background()
	store := newFakeMemoryStore()
	po := newProfileOnboarding(store, profileAccountFake{acct: profileAccount()})

	start, handled := po.maybeStart(ctx, 42, 555)
	if !handled || !strings.Contains(start.text, "chiami") {
		t.Fatalf("maybeStart = (%+v, %v), want identity question", start, handled)
	}
	if !driveToStyle(t, po, 42) {
		t.Fatal("could not drive to style step")
	}
	draft, handled := po.handleText(ctx, 42, "italiano Europe/Rome tono tecnico risposte brevi voce")
	if !handled || !strings.Contains(draft.text, "confermare") || draft.markup == nil {
		t.Fatalf("style answer = (%+v, %v), want draft with buttons", draft, handled)
	}
	assertProfileCallbackDataBounded(t, draft.markup)

	done, handled := po.handleCallback(ctx, 42, profileCallbackData(42, profileActionConfirm))
	if !handled || !strings.Contains(done.text, "Profilo salvato") {
		t.Fatalf("confirm callback = (%+v, %v), want success reply", done, handled)
	}
	a, ok := store.confirmedAnswers(profileAccount().IdentityID)
	if !ok {
		t.Fatal("StoreConfirmed was not called")
	}
	if !strings.Contains(a.Name, "Davide") {
		t.Fatalf("confirmed name = %q, want it to contain Davide", a.Name)
	}
	if a.Lang != "it" || a.Timezone != "Europe/Rome" || a.VoiceMode == nil || !*a.VoiceMode {
		t.Fatalf("confirmed answers = %+v, want it/Europe/Rome/voice", a)
	}
	if st := store.state(profileAccount().IdentityID); !st.Completed || st.Skipped {
		t.Fatalf("state = %+v, want completed and not skipped", st)
	}
}

func TestProfileOnboardingSkipWritesSkippedMetadata(t *testing.T) {
	ctx := context.Background()
	store := newFakeMemoryStore()
	po := newProfileOnboarding(store, profileAccountFake{acct: profileAccount()})

	if _, handled := po.maybeStart(ctx, 42, 555); !handled {
		t.Fatal("maybeStart should start onboarding")
	}
	out, handled := po.handleCallback(ctx, 42, profileCallbackData(42, profileActionSkip))
	if !handled || !strings.Contains(out.text, "saltato") {
		t.Fatalf("skip callback = (%+v, %v), want skipped reply", out, handled)
	}
	st := store.state(profileAccount().IdentityID)
	if !st.Skipped || st.Completed {
		t.Fatalf("state = %+v, want skipped and not completed", st)
	}
	if _, ok := store.confirmedAnswers(profileAccount().IdentityID); ok {
		t.Fatal("skipped onboarding must not store a confirmed profile")
	}
}

func TestProfileOnboardingEditRevisesDraftBeforeConfirm(t *testing.T) {
	ctx := context.Background()
	store := newFakeMemoryStore()
	po := newProfileOnboarding(store, profileAccountFake{acct: profileAccount()})

	po.maybeStart(ctx, 42, 555)
	if !driveToStyle(t, po, 42) {
		t.Fatal("could not drive to style step")
	}
	po.handleText(ctx, 42, "italiano Europe/Rome risposte concise")
	edit, handled := po.handleCallback(ctx, 42, profileCallbackData(42, profileActionEdit))
	if !handled || !strings.Contains(edit.text, "modifiche") {
		t.Fatalf("edit callback = (%+v, %v), want edit prompt", edit, handled)
	}
	revised, handled := po.handleText(ctx, 42, "risposte brevi")
	if !handled || !strings.Contains(revised.text, "short") {
		t.Fatalf("edit text = (%+v, %v), want revised short draft", revised, handled)
	}
	done, handled := po.handleCallback(ctx, 42, profileCallbackData(42, profileActionConfirm))
	if !handled || !strings.Contains(done.text, "Profilo salvato") {
		t.Fatalf("confirm after edit = (%+v, %v), want success", done, handled)
	}
	a, ok := store.confirmedAnswers(profileAccount().IdentityID)
	if !ok {
		t.Fatal("StoreConfirmed was not called")
	}
	if a.ResponseLength != "short" {
		t.Fatalf("confirmed response length = %q, want short (edit must override concise)", a.ResponseLength)
	}
}

func TestProfileOnboardingStaleCallbackAckNoop(t *testing.T) {
	po := newProfileOnboarding(newFakeMemoryStore(), profileAccountFake{acct: profileAccount()})

	out, handled := po.handleCallback(context.Background(), 42, profileCallbackData(42, profileActionConfirm))
	if !handled || out.text != "" || !strings.Contains(out.ack, "scadut") {
		t.Fatalf("stale callback = (%+v, %v), want ack-only no-op", out, handled)
	}
}

func TestProfileOnboardingNoStoreDegrades(t *testing.T) {
	po := newProfileOnboarding(nil, profileAccountFake{acct: profileAccount()})

	out, handled := po.maybeStart(context.Background(), 42, 555)
	if !handled || !strings.Contains(out.text, "Profilo non disponibile") {
		t.Fatalf("no-store maybeStart = (%+v, %v), want clear degradation", out, handled)
	}
}

type profileAccountFake struct {
	acct Account
	err  error
}

func (f profileAccountFake) GetAccountByTelegramID(context.Context, int64) (Account, error) {
	if f.err != nil {
		return Account{}, f.err
	}
	return f.acct, nil
}

func profileAccount() Account {
	return Account{TelegramUserID: 555, IdentityID: "00000000-0000-0000-0000-000000000001", Username: "dav"}
}

func assertProfileCallbackDataBounded(t *testing.T, markup *tele.ReplyMarkup) {
	t.Helper()
	if markup == nil {
		t.Fatal("markup is nil")
	}
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			if len(button.Data) > callbackDataMaxBytes {
				t.Fatalf("callback data %q is %d bytes, want <= %d", button.Data, len(button.Data), callbackDataMaxBytes)
			}
		}
	}
}

// fakeExtractor returns canned structured Answers per step, proving the
// Work/Projects/Social answers flow into the rendered Agent.md.
type fakeExtractor struct{}

func (fakeExtractor) Extract(_ context.Context, step profileflow.Step, _ string) (profileflow.Answers, error) {
	switch step {
	case profileflow.StepIdentity:
		return profileflow.Answers{Name: "Davide", Role: "dev", Company: "Aura"}, nil
	case profileflow.StepWork:
		return profileflow.Answers{Expertise: []string{"backend"}, Stack: []string{"Go", "Neo4j"}}, nil
	case profileflow.StepProjects:
		return profileflow.Answers{Projects: []string{"Aura"}, Goals: []string{"ship Phase 14"}}, nil
	case profileflow.StepSocial:
		return profileflow.Answers{Interests: []string{"AI agents"}, People: []string{"Andrea — business partner"}}, nil
	case profileflow.StepStyle:
		return profileflow.Answers{TonePreference: "friendly", ResponseLength: "normal"}, nil
	case profileflow.StepDraft:
		// Simulates the LLM correcting "agenti Ai" → "Agenti AI" during an edit.
		return profileflow.Answers{Stack: []string{"Agenti AI"}}, nil
	default:
		return profileflow.Answers{}, nil
	}
}

func TestProfileOnboarding_RichInterviewWritesProfile(t *testing.T) {
	store := newFakeMemoryStore()
	p := newProfileOnboarding(store, profileAccountFake{acct: Account{TelegramUserID: 1, IdentityID: "id1", Username: "dav"}})
	p.extractor = fakeExtractor{}

	chatID, uid := int64(1), int64(1)
	if _, ok := p.maybeStart(context.Background(), chatID, uid); !ok {
		t.Fatal("maybeStart should start onboarding for a profile-less identity")
	}
	// 5 answers: identity, work, projects, social, style.
	for _, ans := range []string{"Davide dev Aura", "Go Neo4j", "Aura", "AI; Andrea", "diretto breve"} {
		p.handleText(context.Background(), chatID, ans)
	}
	p.handleCallback(context.Background(), chatID, profileCallbackData(chatID, profileActionConfirm))

	a, ok := store.confirmedAnswers("id1")
	if !ok {
		t.Fatal("profile not stored")
	}
	if a.Name != "Davide" {
		t.Errorf("name = %q, want Davide", a.Name)
	}
	if !slices.Contains(a.Projects, "Aura") {
		t.Errorf("projects = %v, want to contain Aura", a.Projects)
	}
	if !slices.Contains(a.People, "Andrea — business partner") {
		t.Errorf("people = %v, want to contain Andrea — business partner", a.People)
	}
	if !slices.Contains(a.Stack, "Go") || !slices.Contains(a.Stack, "Neo4j") {
		t.Errorf("stack = %v, want Go and Neo4j", a.Stack)
	}
	if a.TonePreference != "friendly" {
		t.Errorf("tone = %q, want friendly", a.TonePreference)
	}
}

// TestProfileOnboarding_EditViaExtractorReplacesStack verifies that when the
// user taps Modifica and sends a correction, the LLM extractor is called (not
// the keyword parser), the corrected value REPLACES the old one in the draft,
// and the saved Agent.md contains the corrected value and NOT the old typo.
func TestProfileOnboarding_EditViaExtractorReplacesStack(t *testing.T) {
	ctx := context.Background()
	store := newFakeMemoryStore()
	acct := Account{TelegramUserID: 2, IdentityID: "id2", Username: "dav2"}
	p := newProfileOnboarding(store, profileAccountFake{acct: acct})
	p.extractor = fakeExtractor{}

	chatID, uid := int64(2), int64(2)
	if _, ok := p.maybeStart(ctx, chatID, uid); !ok {
		t.Fatal("maybeStart should start onboarding")
	}

	// Drive through identity/work(with typo)/projects/social/style answers.
	// fakeExtractor returns Stack: ["Go", "Neo4j"] for StepWork — the typo is
	// injected directly to simulate an earlier bad extraction.
	for _, ans := range []string{"Davide dev Aura", "Go Neo4j", "Aura", "AI; Andrea", "diretto breve"} {
		p.handleText(ctx, chatID, ans)
	}

	// Inject the typo directly so we can verify replacement.
	p.mu.Lock()
	ps := p.sessions[chatID]
	p.mu.Unlock()
	ps.mu.Lock()
	ps.session.Answers.Stack = []string{"agenti Ai"}
	_, _ = ps.session.Apply(profileflow.Input{Intent: profileflow.IntentAnswer, Answers: profileflow.Answers{Lang: "it"}})
	ps.mu.Unlock()

	// Tap Modifica.
	edit, handled := p.handleCallback(ctx, chatID, profileCallbackData(chatID, profileActionEdit))
	if !handled || !strings.Contains(edit.text, "modifiche") {
		t.Fatalf("edit callback = (%+v, %v), want edit prompt", edit, handled)
	}

	// Send correction text; fakeExtractor.StepDraft returns Stack: ["Agenti AI"].
	revised, handled := p.handleText(ctx, chatID, "lo stack è Agenti AI")
	if !handled {
		t.Fatalf("edit handleText not handled: %+v", revised)
	}
	if strings.Contains(revised.text, "agenti Ai") {
		t.Fatalf("revised draft still contains old typo:\n%s", revised.text)
	}
	if !strings.Contains(revised.text, "Agenti AI") {
		t.Fatalf("revised draft missing corrected value:\n%s", revised.text)
	}

	// Confirm.
	done, handled := p.handleCallback(ctx, chatID, profileCallbackData(chatID, profileActionConfirm))
	if !handled || !strings.Contains(done.text, "Profilo salvato") {
		t.Fatalf("confirm = (%+v, %v), want saved", done, handled)
	}

	a, ok := store.confirmedAnswers(acct.IdentityID)
	if !ok {
		t.Fatal("StoreConfirmed was not called")
	}
	if slices.Contains(a.Stack, "agenti Ai") {
		t.Fatalf("saved stack still contains old typo: %v", a.Stack)
	}
	if !slices.Contains(a.Stack, "Agenti AI") {
		t.Fatalf("saved stack missing corrected value: %v", a.Stack)
	}
}

// TestAnswersFromTextKeywordFallback pins the Italian-keyword FALLBACK branch of
// answersFromText — the parser used when the LLM extractor is absent or errors
// (handleText:152,155). The other tests only exercise the extractor path, so this
// is the only coverage of the keyword map itself: language ("ital"), timezone
// ("europe/rome"/"roma"), tone ("tecnic"/"technical"), length (the short>concise
// precedence), and voice ("voce"/"voice"). The no-match case must yield the
// zero-value Answers so a free-text answer never silently injects a preference.
func TestAnswersFromTextKeywordFallback(t *testing.T) {
	cases := []struct {
		name string
		text string
		want profileflow.Answers
	}{
		{
			name: "full_italian_phrase",
			text: "italiano Europe/Rome tono tecnico risposte brevi voce",
			want: profileflow.Answers{
				Lang:           "it",
				Timezone:       "Europe/Rome",
				TonePreference: "technical",
				ResponseLength: "short",
				VoiceMode:      ptrBool(true),
			},
		},
		{
			name: "roma_alias_and_concisa",
			text: "Vivo a Roma e preferisco risposte concise",
			want: profileflow.Answers{Timezone: "Europe/Rome", ResponseLength: "concise"},
		},
		{
			name: "english_keywords",
			text: "short answers, technical tone, voice on",
			want: profileflow.Answers{TonePreference: "technical", ResponseLength: "short", VoiceMode: ptrBool(true)},
		},
		{
			name: "short_beats_concise_precedence",
			text: "voglio risposte brevi e concise",
			want: profileflow.Answers{ResponseLength: "short"},
		},
		{
			name: "no_keyword_match_is_zero_value",
			text: "Buongiorno, mi presento oggi",
			want: profileflow.Answers{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := answersFromText(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("answersFromText(%q) = %+v, want %+v", tc.text, got, tc.want)
			}
		})
	}
}

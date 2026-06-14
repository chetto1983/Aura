package telegram

import (
	"context"
	"strings"
	"testing"

	profileflow "github.com/chetto1983/aura/internal/onboarding"
	"github.com/chetto1983/aura/internal/profile"
	tele "gopkg.in/telebot.v4"
)

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
	store := profile.NewStore(t.TempDir())
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
	loaded, err := store.ReadProfile(profileAccount().IdentityID)
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}
	if !strings.Contains(loaded.AgentMD, "Name: Davide") ||
		!strings.Contains(loaded.AgentMD, "Prefer Italian responses") {
		t.Fatalf("stored Agent.md missing onboarding content:\n%s", loaded.AgentMD)
	}
	if loaded.Preferences.Lang != "it" || loaded.Preferences.Timezone != "Europe/Rome" || !loaded.Preferences.VoiceMode {
		t.Fatalf("stored preferences = %+v, want it/Europe/Rome/voice", loaded.Preferences)
	}
	if !loaded.Metadata.OnboardingCompleted || loaded.Metadata.OnboardingSkipped {
		t.Fatalf("metadata = %+v, want completed and not skipped", loaded.Metadata)
	}
}

func TestProfileOnboardingSkipWritesSkippedMetadata(t *testing.T) {
	ctx := context.Background()
	store := profile.NewStore(t.TempDir())
	po := newProfileOnboarding(store, profileAccountFake{acct: profileAccount()})

	if _, handled := po.maybeStart(ctx, 42, 555); !handled {
		t.Fatal("maybeStart should start onboarding")
	}
	out, handled := po.handleCallback(ctx, 42, profileCallbackData(42, profileActionSkip))
	if !handled || !strings.Contains(out.text, "saltato") {
		t.Fatalf("skip callback = (%+v, %v), want skipped reply", out, handled)
	}
	loaded, err := store.ReadProfile(profileAccount().IdentityID)
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}
	if !loaded.Metadata.OnboardingSkipped || loaded.Metadata.OnboardingCompleted {
		t.Fatalf("metadata = %+v, want skipped and not completed", loaded.Metadata)
	}
	if strings.TrimSpace(loaded.AgentMD) != "" {
		t.Fatalf("skipped onboarding should not inject Agent.md, got:\n%s", loaded.AgentMD)
	}
}

func TestProfileOnboardingEditRevisesDraftBeforeConfirm(t *testing.T) {
	ctx := context.Background()
	store := profile.NewStore(t.TempDir())
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
	loaded, err := store.ReadProfile(profileAccount().IdentityID)
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}
	if !strings.Contains(loaded.AgentMD, "Response length: short") {
		t.Fatalf("stored Agent.md missing edited response length:\n%s", loaded.AgentMD)
	}
}

func TestProfileOnboardingStaleCallbackAckNoop(t *testing.T) {
	po := newProfileOnboarding(profile.NewStore(t.TempDir()), profileAccountFake{acct: profileAccount()})

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
	default:
		return profileflow.Answers{}, nil
	}
}

func TestProfileOnboarding_RichInterviewWritesProfile(t *testing.T) {
	dir := t.TempDir()
	store := profile.NewStore(dir)
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

	got, err := store.ReadProfile("id1")
	if err != nil {
		t.Fatalf("profile not written: %v", err)
	}
	for _, want := range []string{"## Projects & Goals", "- Aura", "## People", "Andrea — business partner", "## Expertise & Tools", "Stack: Go, Neo4j"} {
		if !strings.Contains(got.AgentMD, want) {
			t.Errorf("Agent.md missing %q:\n%s", want, got.AgentMD)
		}
	}
}

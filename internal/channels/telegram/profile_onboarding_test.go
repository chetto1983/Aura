package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/profile"
	tele "gopkg.in/telebot.v4"
)

func TestProfileOnboardingConfirmWritesProfile(t *testing.T) {
	ctx := context.Background()
	store := profile.NewStore(t.TempDir())
	po := newProfileOnboarding(store, profileAccountFake{acct: profileAccount()})

	start, handled := po.maybeStart(ctx, 42, 555)
	if !handled || !strings.Contains(start.text, "chiamarti") {
		t.Fatalf("maybeStart = (%+v, %v), want first question", start, handled)
	}
	if out, handled := po.handleText(ctx, 42, "Davide"); !handled || !strings.Contains(out.text, "lingua") {
		t.Fatalf("name answer = (%+v, %v), want preferences question", out, handled)
	}
	draft, handled := po.handleText(ctx, 42, "italiano Europe/Rome tono tecnico risposte brevi voce")
	if !handled || !strings.Contains(draft.text, "confermare") || draft.markup == nil {
		t.Fatalf("preferences answer = (%+v, %v), want draft with buttons", draft, handled)
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
	po.handleText(ctx, 42, "Davide")
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

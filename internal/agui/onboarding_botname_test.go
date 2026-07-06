package agui

import (
	"context"
	"strings"
	"testing"
)

// onboarding_botname_test.go covers the resolve-on-use bot-username seam
// (BotUsernameResolver): a live resolver's non-empty result wins over the boot-frozen
// snapshot so a token saved mid-session yields a working deep-link WITHOUT a restart, an
// empty resolver result falls back to the snapshot, and both-empty leaves minting
// unavailable. resolveBotName is exercised directly and end-to-end through CompleteProfile.

func TestResolveBotNameResolverWins(t *testing.T) {
	svc := newOnboardingService(OnboardingDeps{
		BotUsername:         "SnapshotBot",
		BotUsernameResolver: func(context.Context) string { return "  LiveBot  " },
	})
	if got := svc.resolveBotName(context.Background()); got != "LiveBot" {
		t.Fatalf("resolveBotName = %q, want trimmed live value LiveBot", got)
	}
}

func TestResolveBotNameFallsBackToSnapshot(t *testing.T) {
	svc := newOnboardingService(OnboardingDeps{
		BotUsername:         "SnapshotBot",
		BotUsernameResolver: func(context.Context) string { return "   " },
	})
	if got := svc.resolveBotName(context.Background()); got != "SnapshotBot" {
		t.Fatalf("resolveBotName = %q, want snapshot fallback SnapshotBot", got)
	}
}

func TestResolveBotNameNoResolverUsesSnapshot(t *testing.T) {
	svc := newOnboardingService(OnboardingDeps{BotUsername: "SnapshotBot"})
	if got := svc.resolveBotName(context.Background()); got != "SnapshotBot" {
		t.Fatalf("resolveBotName = %q, want snapshot SnapshotBot", got)
	}
}

// TestCompleteProfileUsesLiveResolverWhenSnapshotEmpty is the no-restart property: the boot
// snapshot is empty (a daemon that started with no token), but a token saved mid-session
// makes the resolver return a live name, so CompleteProfile mints a deep-link carrying THAT
// name instead of 502-ing on an unavailable bot.
func TestCompleteProfileUsesLiveResolverWhenSnapshotEmpty(t *testing.T) {
	pw := &recordingProfileWriter{}
	tg := &fakeTelegram{}
	svc := newOnboardingService(OnboardingDeps{
		Capabilities:        fakeCaps{grants: []string{"agent.run"}},
		Extractor:           &countingExtractor{},
		Profiles:            pw,
		Telegram:            tg,
		BotUsername:         "", // boot snapshot empty (no token at boot)
		BotUsernameResolver: func(context.Context) string { return "LiveBot" },
	})
	start, err := svc.StartProfileSession(context.Background(), "operator-1")
	if err != nil {
		t.Fatalf("StartProfileSession: %v", err)
	}
	for _, text := range []string{"Davide", "backend dev, Go", "Aura", "AI agents", "friendly, short"} {
		if _, err := svc.Step(context.Background(), "operator-1", start.SessionToken, OnboardingStepRequest{Intent: "answer", Text: text}); err != nil {
			t.Fatalf("answer: %v", err)
		}
	}
	if _, err := svc.Step(context.Background(), "operator-1", start.SessionToken, OnboardingStepRequest{Intent: "confirm"}); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	done, err := svc.CompleteProfile(context.Background(), "operator-1", start.SessionToken)
	if err != nil {
		t.Fatalf("CompleteProfile with a live resolver must succeed, got %v", err)
	}
	if !strings.Contains(done.DeepLink, "https://t.me/LiveBot?start=") {
		t.Fatalf("deep-link = %q, want the live-resolved bot name LiveBot", done.DeepLink)
	}
	if tg.mintedCount() != 1 {
		t.Fatalf("minted %d pending tokens, want 1", tg.mintedCount())
	}
}

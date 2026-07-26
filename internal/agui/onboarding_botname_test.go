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
// unavailable. resolveBotName is exercised directly and end-to-end through SubmitProfile.

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

// TestSubmitProfileUsesLiveResolverWhenSnapshotEmpty is the no-restart property: the boot
// snapshot is empty (a daemon that started with no token), but a token saved mid-session
// makes the resolver return a live name, so the seed submit mints a deep-link carrying THAT
// name instead of 502-ing on an unavailable bot.
func TestSubmitProfileUsesLiveResolverWhenSnapshotEmpty(t *testing.T) {
	pw := &recordingProfileWriter{}
	tg := &fakeTelegram{}
	svc := newOnboardingService(OnboardingDeps{
		Capabilities:        fakeCaps{grants: []string{"agent.run"}},
		Profiles:            pw,
		Telegram:            tg,
		BotUsername:         "", // boot snapshot empty (no token at boot)
		BotUsernameResolver: func(context.Context) string { return "LiveBot" },
	})
	done, err := svc.SubmitProfile(context.Background(), "operator-1", fullSeed)
	if err != nil {
		t.Fatalf("SubmitProfile with a live resolver must succeed, got %v", err)
	}
	if !strings.Contains(done.DeepLink, "https://t.me/LiveBot?start=") {
		t.Fatalf("deep-link = %q, want the live-resolved bot name LiveBot", done.DeepLink)
	}
	if tg.mintedCount() != 1 {
		t.Fatalf("minted %d pending tokens, want 1", tg.mintedCount())
	}
}

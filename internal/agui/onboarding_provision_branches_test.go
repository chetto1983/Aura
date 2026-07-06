package agui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/onboarding"
	"github.com/chetto1983/aura/internal/profile"
)

// onboarding_provision_branches_test.go covers the CreateTelegramLink / CompleteProfile /
// mintTelegramLink / persistProfile error branches the happy-path tests in
// onboarding_api_test.go leave uncovered: unwired/erroring Telegram mint, a provisioned or
// unwired session, a profile-write failure (which compensates the minted link), and the
// persistProfile best-effort guards. Every case drives the concrete onboardingService with
// the shared saga-leg fakes, no live store.

// errProfileWriter fails WriteProfile so the profile-write error legs are reachable.
type errProfileWriter struct{ err error }

func (e errProfileWriter) WriteProfile(string, profile.Profile) error { return e.err }

// completedProfileService builds a service whose profile session has been walked to
// StatusCompleted (a confirmed interview with a non-empty draft), returning the service +
// its live session token. The profile writer + telegram fake are caller-supplied so the
// error legs can be injected.
func completedProfileService(t *testing.T, pw ProfileWriter, tg TelegramMint) (*onboardingService, string) {
	t.Helper()
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: []string{"agent.run"}},
		Extractor:    &countingExtractor{},
		Profiles:     pw,
		Telegram:     tg,
		BotUsername:  "AuraBot",
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
	return svc, start.SessionToken
}

func TestCreateTelegramLinkEmptyRequester(t *testing.T) {
	svc := newOnboardingService(OnboardingDeps{Telegram: &fakeTelegram{}, BotUsername: "AuraBot"})
	if _, err := svc.CreateTelegramLink(context.Background(), ""); !errors.Is(err, errOnboardingForbidden) {
		t.Fatalf("empty requester err = %v, want forbidden", err)
	}
}

func TestCreateTelegramLinkMintUnwired(t *testing.T) {
	// No Telegram port wired → mintTelegramLink returns the unwired sentinel.
	svc := newOnboardingService(OnboardingDeps{BotUsername: "AuraBot"})
	if _, err := svc.CreateTelegramLink(context.Background(), "operator-1"); !errors.Is(err, errProvisioningUnavailable) {
		t.Fatalf("unwired telegram err = %v, want provisioning-unavailable", err)
	}
}

func TestCreateTelegramLinkMintError(t *testing.T) {
	tg := &fakeTelegram{insertErr: errors.New("mint failed")}
	svc := newOnboardingService(OnboardingDeps{Telegram: tg, BotUsername: "AuraBot"})
	if _, err := svc.CreateTelegramLink(context.Background(), "operator-1"); err == nil {
		t.Fatal("CreateTelegramLink must surface a mint failure")
	}
	if tg.mintedCount() != 0 {
		t.Fatalf("a failed mint left %d pending tokens, want 0 (compensated)", tg.mintedCount())
	}
}

func TestCompleteProfileProvisionedSessionRejected(t *testing.T) {
	// CreateTelegramLink creates a provisioned (non-interview) entry; CompleteProfile on that
	// token must reject with session-not-found (the saga cannot run twice).
	tg := &fakeTelegram{}
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: []string{"agent.run"}},
		Extractor:    &countingExtractor{},
		Profiles:     &recordingProfileWriter{},
		Telegram:     tg, BotUsername: "AuraBot",
	})
	link, err := svc.CreateTelegramLink(context.Background(), "operator-1")
	if err != nil {
		t.Fatalf("CreateTelegramLink: %v", err)
	}
	if _, err := svc.CompleteProfile(context.Background(), "operator-1", link.SessionToken); !errors.Is(err, errOnboardingSessionNotFound) {
		t.Fatalf("CompleteProfile on a provisioned session err = %v, want session-not-found", err)
	}
}

func TestCompleteProfileNilProfilesUnavailable(t *testing.T) {
	// Profiles unwired → CompleteProfile refuses before minting.
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: []string{"agent.run"}},
		Extractor:    &countingExtractor{},
		Telegram:     &fakeTelegram{}, BotUsername: "AuraBot",
	})
	start, err := svc.StartProfileSession(context.Background(), "operator-1")
	if err != nil {
		t.Fatalf("StartProfileSession: %v", err)
	}
	if _, err := svc.CompleteProfile(context.Background(), "operator-1", start.SessionToken); !errors.Is(err, errProvisioningUnavailable) {
		t.Fatalf("CompleteProfile with nil profiles err = %v, want provisioning-unavailable", err)
	}
}

func TestCompleteProfileMintErrorDoesNotWriteProfile(t *testing.T) {
	pw := &recordingProfileWriter{}
	tg := &fakeTelegram{insertErr: errors.New("mint down")}
	svc, tok := completedProfileService(t, pw, tg)
	if _, err := svc.CompleteProfile(context.Background(), "operator-1", tok); err == nil {
		t.Fatal("CompleteProfile must surface the mint failure")
	}
	if len(pw.writes) != 0 {
		t.Fatalf("profile written (%v) despite a mint failure — Telegram is minted BEFORE the profile marker", pw.writes)
	}
}

func TestCompleteProfileWriteErrorCompensatesTelegram(t *testing.T) {
	tg := &fakeTelegram{}
	svc, tok := completedProfileService(t, errProfileWriter{err: errors.New("disk full")}, tg)
	if _, err := svc.CompleteProfile(context.Background(), "operator-1", tok); err == nil {
		t.Fatal("CompleteProfile must surface a profile-write failure")
	}
	// A failed profile write must compensate the just-minted Telegram link (no dangling pending).
	if tg.mintedCount() != 0 {
		t.Fatalf("profile-write failure left %d pending Telegram tokens, want 0 (compensated)", tg.mintedCount())
	}
}

func TestPersistProfileBranches(t *testing.T) {
	t.Run("nil session no-op", func(t *testing.T) {
		pw := &recordingProfileWriter{}
		svc := newOnboardingService(OnboardingDeps{Profiles: pw})
		svc.persistProfile(&sessionEntry{}, "id-1") // entry.session == nil
		if len(pw.writes) != 0 {
			t.Fatalf("persistProfile wrote %v for a nil session, want none", pw.writes)
		}
	})

	t.Run("empty draft writes nothing", func(t *testing.T) {
		pw := &recordingProfileWriter{}
		svc := newOnboardingService(OnboardingDeps{Profiles: pw})
		entry := &sessionEntry{session: onboarding.NewSession("id-1", "id-1")} // no DraftAgentMD
		svc.persistProfile(entry, "id-1")
		if len(pw.writes) != 0 {
			t.Fatalf("persistProfile wrote %v for an empty draft, want none", pw.writes)
		}
	})

	t.Run("write error is logged not fatal", func(t *testing.T) {
		svc := newOnboardingService(OnboardingDeps{Profiles: errProfileWriter{err: errors.New("disk full")}})
		sess := onboarding.NewSession("id-1", "id-1")
		sess.DraftAgentMD = "# Agent"
		// Best-effort: a write failure must NOT panic and returns nothing (logged only).
		svc.persistProfile(&sessionEntry{session: sess}, "id-1")
	})

	t.Run("happy write persists confirmed draft", func(t *testing.T) {
		pw := &recordingProfileWriter{}
		svc := newOnboardingService(OnboardingDeps{Profiles: pw})
		sess := onboarding.NewSession("id-9", "id-9")
		sess.DraftAgentMD = "# Agent"
		svc.persistProfile(&sessionEntry{session: sess}, "id-9")
		if len(pw.writes) != 1 || pw.writes[0] != "id-9" {
			t.Fatalf("persistProfile writes = %v, want [id-9]", pw.writes)
		}
	})
}

func TestMintTelegramLinkUnwired(t *testing.T) {
	svc := newOnboardingService(OnboardingDeps{BotUsername: ""}) // no telegram, no bot name
	_, _, _, err := svc.mintTelegramLink(context.Background(), "id-1")
	if !errors.Is(err, errProvisioningUnavailable) {
		t.Fatalf("mintTelegramLink unwired err = %v, want provisioning-unavailable", err)
	}
}

func TestMintTelegramLinkSuccess(t *testing.T) {
	tg := &fakeTelegram{}
	svc := newOnboardingService(OnboardingDeps{Telegram: tg, BotUsername: "AuraBot"})
	tok, deepLink, qrSVG, err := svc.mintTelegramLink(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("mintTelegramLink: %v", err)
	}
	if tok == "" || !strings.HasPrefix(deepLink, "https://t.me/AuraBot?start=") || strings.TrimSpace(qrSVG) == "" {
		t.Fatalf("mintTelegramLink = tok:%q deepLink:%q qr:%t, want a token, deep-link and QR", tok, deepLink, strings.TrimSpace(qrSVG) != "")
	}
}

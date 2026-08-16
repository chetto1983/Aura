package agui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/onboarding"
)

// onboarding_provision_branches_test.go covers the CreateTelegramLink / SubmitProfile /
// mintTelegramLink / persistProfile error branches the happy-path tests in
// onboarding_api_test.go leave uncovered: unwired/erroring Telegram mint, a profile-write
// failure (which compensates the minted link), and the persistProfile best-effort guards.
// Every case drives the concrete onboardingService with the shared saga-leg fakes, no live
// store.

// errProfileWriter fails the memory store so the profile-write error legs are reachable.
type errProfileWriter struct{ err error }

func (e errProfileWriter) StoreConfirmed(context.Context, string, onboarding.Answers) error {
	return e.err
}
func (e errProfileWriter) StoreSkipped(context.Context, string) error { return e.err }
func (e errProfileWriter) Status(context.Context, string) (onboarding.OnboardingState, error) {
	return onboarding.OnboardingState{}, e.err
}

func (e errProfileWriter) MarkNudged(context.Context, string) error { return e.err }

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

// seedService builds a service with only the seed-side ports wired, so the SubmitProfile
// error legs can be driven with an injected profile writer / telegram fake.
func seedService(pw onboarding.Store, tg TelegramMint) *onboardingService {
	return newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: []string{"agent.run"}},
		Profiles:     pw,
		Telegram:     tg,
		BotUsername:  "AuraBot",
	})
}

// TestSubmitProfileSurvivesAnUnavailableTelegramLink pins Amendment #95's "skipping stays
// available and stays cheap" gate: Telegram is a nicety on this path, never a gate. An
// instance with no @BotFather token can NEVER mint a link, so failing the submission there
// would leave the sentinel unwritten and re-open first-run setup on every login forever.
//
// This REPLACES TestSubmitProfileMintErrorDoesNotWriteProfile, which pinned the opposite
// ordering ("a link that could not be minted never leaves onboarding marked done"). That
// invariant was inherited verbatim from the deleted interview and is incompatible with the
// gate above; the compensation half of it — a mint that fails leaves no pending row — is
// still asserted here and in TestSubmitProfileWriteErrorCompensatesTelegramPending.
func TestSubmitProfileSurvivesAnUnavailableTelegramLink(t *testing.T) {
	for name, svcFor := range map[string]func(onboarding.Store) (*onboardingService, *fakeTelegram){
		"mint fails": func(pw onboarding.Store) (*onboardingService, *fakeTelegram) {
			tg := &fakeTelegram{insertErr: errors.New("mint down")}
			return seedService(pw, tg), tg
		},
		"no bot configured": func(pw onboarding.Store) (*onboardingService, *fakeTelegram) {
			tg := &fakeTelegram{}
			return newOnboardingService(OnboardingDeps{Profiles: pw, Telegram: tg}), tg
		},
	} {
		t.Run(name, func(t *testing.T) {
			for seedName, seed := range map[string]OnboardingSeed{"confirm": fullSeed, "skip": {}} {
				t.Run(seedName, func(t *testing.T) {
					pw := &recordingProfileWriter{}
					svc, tg := svcFor(pw)
					done, err := svc.SubmitProfile(context.Background(), "operator-1", seed)
					if err != nil {
						t.Fatalf("SubmitProfile must still record the profile without a link: %v", err)
					}
					if done.DeepLink != "" || done.SessionToken != "" {
						t.Fatalf("SubmitProfile = %+v, want no link and no poll token", done)
					}
					if len(pw.writes) != 1 || pw.writes[0] != "operator-1" {
						t.Fatalf("profile writes = %v, want exactly the current operator", pw.writes)
					}
					if tg.mintedCount() != 0 {
						t.Fatalf("an unavailable link left %d pending tokens, want 0", tg.mintedCount())
					}
				})
			}
		})
	}
}

// TestSubmitProfileWriteErrorCompensatesTelegramPending proves the other half of that
// ordering: a profile write that fails after the mint rolls the pending Telegram row back.
func TestSubmitProfileWriteErrorCompensatesTelegramPending(t *testing.T) {
	for name, seed := range map[string]OnboardingSeed{"confirm": fullSeed, "skip": {}} {
		t.Run(name, func(t *testing.T) {
			tg := &fakeTelegram{}
			svc := seedService(errProfileWriter{err: errors.New("disk full")}, tg)
			if _, err := svc.SubmitProfile(context.Background(), "operator-1", seed); err == nil {
				t.Fatal("SubmitProfile must surface a profile-write failure")
			}
			if tg.mintedCount() != 0 {
				t.Fatalf("profile-write failure left %d pending Telegram tokens, want 0 (compensated)", tg.mintedCount())
			}
		})
	}
}

func TestPersistProfileBranches(t *testing.T) {
	t.Run("unwired profiles no-op", func(t *testing.T) {
		svc := newOnboardingService(OnboardingDeps{})
		// No store to write to: the guard must return before dereferencing it.
		svc.persistProfile(context.Background(), fullSeed, "id-1")
	})

	t.Run("blank seed writes nothing", func(t *testing.T) {
		pw := &recordingProfileWriter{}
		svc := newOnboardingService(OnboardingDeps{Profiles: pw})
		svc.persistProfile(context.Background(), OnboardingSeed{}, "id-1")
		if len(pw.writes) != 0 {
			t.Fatalf("persistProfile wrote %v for a blank seed, want none — no sentinel means the new identity still sees its own form", pw.writes)
		}
	})

	t.Run("write error is logged not fatal", func(t *testing.T) {
		svc := newOnboardingService(OnboardingDeps{Profiles: errProfileWriter{err: errors.New("disk full")}})
		// Best-effort: a write failure must NOT panic and must not fail the committed saga.
		svc.persistProfile(context.Background(), fullSeed, "id-1")
	})

	t.Run("happy write seeds the new identity", func(t *testing.T) {
		pw := &recordingProfileWriter{}
		svc := newOnboardingService(OnboardingDeps{Profiles: pw})
		svc.persistProfile(context.Background(), fullSeed, "id-9")
		if len(pw.writes) != 1 || pw.writes[0] != "id-9" {
			t.Fatalf("persistProfile writes = %v, want [id-9]", pw.writes)
		}
		if len(pw.confirmed) != 1 || pw.confirmed[0].Name != "Davide" {
			t.Fatalf("persistProfile stored %+v, want the creator's seed under the NEW identity", pw.confirmed)
		}
	})
}

// TestProvisionSeedsNewIdentityGraph drives the WHOLE saga and proves Correction #95.1
// item 3 end to end: the seed the creator filled in the provision body is written for the
// NEWLY CREATED identity, not the creator. With the interview gone this is the only thing
// keeping provisioning's graph seeding alive.
func TestProvisionSeedsNewIdentityGraph(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	pw := &recordingProfileWriter{}
	svc.profiles = pw

	req := provReq(nil)
	req.Seed = fullSeed
	resp, err := svc.Provision(context.Background(), "creator-1", tok, req)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(pw.writes) != 1 || pw.writes[0] != resp.IdentityID {
		t.Fatalf("profile writes = %v, want exactly the NEW identity %q", pw.writes, resp.IdentityID)
	}
	if pw.writes[0] == "creator-1" {
		t.Fatal("provision seeded the creator's graph instead of the new identity's")
	}
	if len(pw.confirmed) != 1 || pw.confirmed[0].Name != "Davide" {
		t.Fatalf("seeded answers = %+v, want the provision body's seed", pw.confirmed)
	}
	if len(pw.skipped) != 0 {
		t.Fatalf("provision wrote a skip sentinel: %v", pw.skipped)
	}
}

// TestProvisionBlankSeedWritesNothing is the complementary branch: a creator who left the
// seed blank writes NO sentinel, so the new operator still gets their own first-run form.
func TestProvisionBlankSeedWritesNothing(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	pw := &recordingProfileWriter{}
	svc.profiles = pw

	if _, err := svc.Provision(context.Background(), "creator-1", tok, provReq(nil)); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(pw.writes) != 0 {
		t.Fatalf("a blank seed wrote %v; no sentinel means the new identity still sees its own form", pw.writes)
	}
}

// TestProvisionSeedWriteFailureDoesNotRollBackTheIdentity pins the best-effort contract: a
// memory sidecar that rejects the seed must not undo an already-committed loginable
// identity, because the profile is re-seedable and the identity is not.
func TestProvisionSeedWriteFailureDoesNotRollBackTheIdentity(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	svc.profiles = errProfileWriter{err: errors.New("sidecar down")}

	req := provReq(nil)
	req.Seed = fullSeed
	resp, err := svc.Provision(context.Background(), "creator-1", tok, req)
	if err != nil {
		t.Fatalf("a failed seed write must not fail provisioning: %v", err)
	}
	if resp.IdentityID == "" || leg.liveIdentities() != 1 || au.liveAuthulaUsers() != 1 {
		t.Fatalf("seed-write failure rolled back the identity: id=%q identities=%d users=%d",
			resp.IdentityID, leg.liveIdentities(), au.liveAuthulaUsers())
	}
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

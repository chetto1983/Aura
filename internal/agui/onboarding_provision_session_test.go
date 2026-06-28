package agui

import (
	"context"
	"errors"
	"testing"
)

func TestProvisionAbandonedLeavesNothing(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: fakeCaps{grants: []string{"identity.create"}},
		Extractor:    &countingExtractor{},
		Profiles:     &recordingProfileWriter{},
		Authula:      au, AuraLeg: leg, Telegram: tg, BotUsername: "AuraBot", Recovery: &fakeRecoveryStore{},
	})
	_, err := svc.Provision(context.Background(), "creator-1", "never-started", provReq(nil))
	if !errors.Is(err, errOnboardingSessionNotFound) {
		t.Fatalf("abandoned provision err = %v, want session-not-found", err)
	}
	if leg.liveIdentities() != 0 || au.liveAuthulaUsers() != 0 || tg.mintedCount() != 0 {
		t.Fatal("an un-started/abandoned provision wrote rows; the saga must not run")
	}
}

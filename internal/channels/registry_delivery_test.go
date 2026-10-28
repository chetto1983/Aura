package channels

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestRegistryConversationDeliverySkipsOtherCapabilitiesAndReleasesSnapshot(t *testing.T) {
	identityOnly := &fakeDeliverer{name: "a-identity", delivered: true}
	target := &fakeConversationDeliverer{
		name:      "b-conversation",
		delivered: true,
	}
	reg := NewRegistry()
	reg.Register(identityOnly)
	reg.Register(target)
	if err := reg.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	type result struct {
		delivered bool
		err       error
	}
	for attempt := 1; attempt <= 2; attempt++ {
		resultCh := make(chan result, 1)
		go func() {
			delivered, err := reg.DeliverToConversation(
				context.Background(), "id-1", "conv-1", "hi",
			)
			resultCh <- result{delivered: delivered, err: err}
		}()

		select {
		case got := <-resultCh:
			if got.err != nil || !got.delivered {
				t.Fatalf("attempt %d = (%v, %v), want (true, nil)", attempt, got.delivered, got.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("attempt %d blocked: registry snapshot lock was not released", attempt)
		}
	}

	if identityOnly.delivers() != 0 {
		t.Fatalf("identity-only calls = %d, want 0", identityOnly.delivers())
	}
	if target.delivers() != 2 {
		t.Fatalf("conversation calls = %d, want 2", target.delivers())
	}
}

type fakeTelegramAccountDeliverer struct {
	fakeChannel
	delivered bool
	calls     []string
}

func (d *fakeTelegramAccountDeliverer) DeliverToTelegramAccount(_ context.Context, identityID string, telegramUserID int64, _ string) (bool, error) {
	d.calls = append(d.calls, fmt.Sprintf("text %s %d", identityID, telegramUserID))
	return d.delivered, nil
}

func (d *fakeTelegramAccountDeliverer) DeliverApprovalToTelegramAccount(_ context.Context, identityID string, telegramUserID int64, token, _, _ string) (bool, error) {
	d.calls = append(d.calls, fmt.Sprintf("approval %s %d %s", identityID, telegramUserID, token))
	return d.delivered, nil
}

// A push to one Telegram account goes through the started channel that can make it, and with
// none started it is "not mine" so the caller falls back to its route.
func TestRegistryTelegramAccountDelivery(t *testing.T) {
	ctx := context.Background()
	empty := NewRegistry()
	if ok, err := empty.DeliverToTelegramAccount(ctx, "id-1", 42, "hi"); ok || err != nil {
		t.Fatalf("no channel: text = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := empty.DeliverApprovalToTelegramAccount(ctx, "id-1", 42, "tok", "task", "agent_job"); ok || err != nil {
		t.Fatalf("no channel: approval = (%v, %v), want (false, nil)", ok, err)
	}

	identityOnly := &fakeDeliverer{name: "a-identity", delivered: true}
	telegram := &fakeTelegramAccountDeliverer{name: "telegram", delivered: true}
	reg := NewRegistry()
	reg.Register(identityOnly)
	reg.Register(telegram)
	if err := reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if ok, err := reg.DeliverToTelegramAccount(ctx, "id-1", 42, "hi"); !ok || err != nil {
		t.Fatalf("text = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := reg.DeliverApprovalToTelegramAccount(ctx, "id-1", 43, "tok", "task", "agent_job"); !ok || err != nil {
		t.Fatalf("approval = (%v, %v), want (true, nil)", ok, err)
	}
	if want := []string{"text id-1 42", "approval id-1 43 tok"}; !slices.Equal(telegram.calls, want) {
		t.Fatalf("telegram calls = %v, want %v", telegram.calls, want)
	}
	if identityOnly.delivers() != 0 {
		t.Fatalf("identity-only calls = %d, want 0", identityOnly.delivers())
	}
}

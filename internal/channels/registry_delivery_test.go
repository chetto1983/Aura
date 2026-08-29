package channels

import (
	"context"
	"testing"
	"time"
)

func TestRegistryConversationDeliverySkipsOtherCapabilitiesAndReleasesSnapshot(t *testing.T) {
	identityOnly := &fakeDeliverer{fakeChannel: fakeChannel{name: "a-identity"}, delivered: true}
	target := &fakeConversationDeliverer{
		fakeChannel: fakeChannel{name: "b-conversation"},
		delivered:   true,
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

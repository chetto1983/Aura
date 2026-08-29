package cron

import (
	"context"
	"errors"
	"testing"
)

// fakeChannelDeliverer is a scripted cron.ChannelDeliverer: it records every call and
// returns a configurable tri-state ((delivered,err)) so the 6 deliverToOrigin
// precedence branches can be driven without a real *channels.Registry.
type fakeChannelDeliverer struct {
	delivered bool
	err       error

	calls []struct {
		identityID     string
		conversationID string
		text           string
	}
}

func (f *fakeChannelDeliverer) DeliverToIdentity(_ context.Context, identityID, text string) (bool, error) {
	f.calls = append(f.calls, struct {
		identityID     string
		conversationID string
		text           string
	}{identityID: identityID, text: text})
	return f.delivered, f.err
}

func (f *fakeChannelDeliverer) DeliverToConversation(_ context.Context, identityID, conversationID, text string) (bool, error) {
	f.calls = append(f.calls, struct {
		identityID     string
		conversationID string
		text           string
	}{identityID: identityID, conversationID: conversationID, text: text})
	return f.delivered, f.err
}

// TestDeliverToOrigin drives the origin-channel precedence end-to-end through
// Dispatch (a reminder bypasses quiet-hours and reaches the notify tail where
// deliverToOrigin sits). It asserts the DESTINATION of each branch (which collaborator
// was called), never a reply string — the ground-truth discipline.
func TestDeliverToOrigin(t *testing.T) {
	t.Parallel()

	const ownedIdentity = "id-I"
	errSend := errors.New("telegram send failed")

	type want struct {
		channelCalled  bool // fakeChannelDeliverer.DeliverToIdentity invoked
		notifierCalled bool // captureNotifier.Notify invoked
		failedInserted bool // a "failed" pending_notification row queued
	}

	cases := []struct {
		name         string
		nilDeliverer bool
		notifyRoute  string
		identityID   string
		delivered    bool
		deliverErr   error
		want         want
	}{
		{
			name:        "stdout projects to exact origin conversation",
			notifyRoute: "stdout",
			identityID:  ownedIdentity,
			delivered:   true,
			want:        want{channelCalled: true},
		},
		{
			name:        "whatsapp stays on explicit notifier route",
			notifyRoute: "whatsapp",
			identityID:  ownedIdentity,
			want:        want{notifierCalled: true},
		},
		{
			name:        "telegram explicitly targets identity channel",
			notifyRoute: "telegram",
			identityID:  ownedIdentity,
			delivered:   true,
			want:        want{channelCalled: true},
		},
		{
			name:        "local stdout stays on notifier",
			notifyRoute: "stdout",
			identityID:  "local",
			want:        want{notifierCalled: true},
		},
		{
			name:        "origin channel failure queues same-route retry",
			notifyRoute: "stdout",
			identityID:  ownedIdentity,
			deliverErr:  errSend,
			want:        want{channelCalled: true, failedInserted: true},
		},
		{
			name:         "missing channel adapter uses explicit stdout notifier",
			nilDeliverer: true,
			notifyRoute:  "stdout",
			identityID:   ownedIdentity,
			want:         want{notifierCalled: true},
		},
		{
			name:        "none is silent",
			notifyRoute: "none",
			identityID:  ownedIdentity,
			want:        want{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "drink water"}
			store := &fakeNotificationStore{}
			notif := &captureNotifier{}
			deliverer := &fakeChannelDeliverer{delivered: tc.delivered, err: tc.deliverErr}

			deps := DispatchDeps{Store: store, Notifier: notif}
			if !tc.nilDeliverer {
				deps.ChannelDeliverer = deliverer
			}

			d, c := newDispatchFor(t, h, KindReminder, deps)
			task := Task{
				ID:                   "tdo",
				Kind:                 KindReminder,
				Payload:              []byte(`{"text":"drink water"}`),
				NotifyRoute:          tc.notifyRoute,
				IdentityID:           tc.identityID,
				OriginConversationID: "conv-origin",
			}
			if err := d.Dispatch(context.Background(), task, c); err != nil {
				t.Fatalf("Dispatch: %v", err)
			}

			channelCalled := len(deliverer.calls) > 0
			if channelCalled != tc.want.channelCalled {
				t.Fatalf("channel called = %v, want %v (calls=%+v)", channelCalled, tc.want.channelCalled, deliverer.calls)
			}
			if tc.want.channelCalled {
				if got := deliverer.calls[0].identityID; got != tc.identityID {
					t.Fatalf("channel delivered to identity %q, want %q", got, tc.identityID)
				}
				wantConversation := "conv-origin"
				if tc.notifyRoute == string(RouteTelegram) {
					wantConversation = "" // explicit cross-channel route uses identity delivery
				}
				if got := deliverer.calls[0].conversationID; got != wantConversation {
					t.Fatalf("channel conversation = %q, want %q for notify=%q", got, wantConversation, tc.notifyRoute)
				}
				if got := deliverer.calls[0].text; got != "drink water" {
					t.Fatalf("channel delivered text %q, want the reminder summary", got)
				}
			}

			notifierCalled := len(notif.texts) > 0
			if notifierCalled != tc.want.notifierCalled {
				t.Fatalf("Notifier called = %v, want %v (routes=%v texts=%v)", notifierCalled, tc.want.notifierCalled, notif.routes, notif.texts)
			}
			if tc.want.notifierCalled {
				if got := string(notif.routes[0]); got != tc.notifyRoute {
					t.Fatalf("Notifier route = %q, want the explicit %q (R7)", got, tc.notifyRoute)
				}
			}

			failedInserted := hasFailedRow(store.inserted)
			if failedInserted != tc.want.failedInserted {
				t.Fatalf("failed pending row inserted = %v, want %v (inserted=%+v)", failedInserted, tc.want.failedInserted, store.inserted)
			}
		})
	}
}

func hasFailedRow(rows []InsertPendingNotificationParams) bool {
	for _, r := range rows {
		if r.Status == "failed" {
			return true
		}
	}
	return false
}

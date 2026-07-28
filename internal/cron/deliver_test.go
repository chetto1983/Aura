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
		identityID string
		text       string
	}
}

func (f *fakeChannelDeliverer) DeliverToIdentity(_ context.Context, identityID, text string) (bool, error) {
	f.calls = append(f.calls, struct {
		identityID string
		text       string
	}{identityID: identityID, text: text})
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
		name                string
		preferOriginChannel bool
		nilDeliverer        bool
		notifyRoute         string
		identityID          string
		delivered           bool
		deliverErr          error
		want                want
	}{
		{
			// channel delivers: gate on, no explicit route, real identity, (true,nil) →
			// the channel handles it; the per-task Notifier is NOT called.
			name:                "channel delivers ⇒ no Notifier",
			preferOriginChannel: true,
			identityID:          ownedIdentity,
			delivered:           true,
			want:                want{channelCalled: true, notifierCalled: false},
		},
		{
			// explicit route wins (R7): a set NotifyRoute skips the channel entirely and
			// goes straight to the route — even with the kill-switch on and a real identity.
			name:                "explicit route wins ⇒ channel skipped + Notifier",
			preferOriginChannel: true,
			notifyRoute:         "whatsapp",
			identityID:          ownedIdentity,
			want:                want{channelCalled: false, notifierCalled: true},
		},
		{
			// "stdout" is the always-available fallback sink the scheduling agent fills in
			// as the implicit default route — NOT a deliberate external channel. It must
			// defer to the origin channel exactly like an unset route (R7 amended after the
			// Phase 20 live gate: the agent auto-populates notify="stdout"). Only whatsapp/email
			// pre-empt origin. Without this the headline Telegram-reminder case lands on stdout.
			name:                "stdout route defers to origin ⇒ channel + no Notifier",
			preferOriginChannel: true,
			notifyRoute:         "stdout",
			identityID:          ownedIdentity,
			delivered:           true,
			want:                want{channelCalled: true, notifierCalled: false},
		},
		{
			// fix-plan 2.1 / operator BUG-2: "telegram" is the OPPOSITE of an alternate
			// channel — it names the origin channel explicitly, and the composite Notifier
			// cannot deliver it (it holds no identity and no channel registry). Before this
			// it fell into the whatsapp/email branch and degraded to stdout, so an operator
			// whose only live channel is Telegram got scheduled output on a server console.
			name:                "telegram route defers to origin ⇒ channel + no Notifier",
			preferOriginChannel: true,
			notifyRoute:         "telegram",
			identityID:          ownedIdentity,
			delivered:           true,
			want:                want{channelCalled: true, notifierCalled: false},
		},
		{
			// un-owned identity ('local'): the gate falls back to the route BEFORE asking
			// the channel — no stray channel push for a non-onboarded identity.
			name:                "no owning channel (local) ⇒ Notifier",
			preferOriginChannel: true,
			identityID:          "local",
			want:                want{channelCalled: false, notifierCalled: true},
		},
		{
			// owns-but-fails: (false,err) queues a failed pending row (the same-channel
			// retry key) and STOPS — the Notifier is NOT called (no double-delivery, Pitfall 3).
			name:                "owns but fails ⇒ failed pending row + no Notifier",
			preferOriginChannel: true,
			identityID:          ownedIdentity,
			deliverErr:          errSend,
			want:                want{channelCalled: true, notifierCalled: false, failedInserted: true},
		},
		{
			// kill-switch off: the channel is never consulted; behavior regresses to
			// today's route-only delivery.
			name:                "kill-switch off ⇒ no channel + Notifier",
			preferOriginChannel: false,
			identityID:          ownedIdentity,
			delivered:           true,
			want:                want{channelCalled: false, notifierCalled: true},
		},
		{
			// nil ChannelDeliverer: legacy route-only behavior (the regression guard).
			name:                "nil deliverer ⇒ legacy Notifier",
			preferOriginChannel: true,
			nilDeliverer:        true,
			identityID:          ownedIdentity,
			want:                want{channelCalled: false, notifierCalled: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "drink water"}
			store := &fakeNotificationStore{}
			notif := &captureNotifier{}
			deliverer := &fakeChannelDeliverer{delivered: tc.delivered, err: tc.deliverErr}

			deps := DispatchDeps{
				Store:               store,
				Notifier:            notif,
				PreferOriginChannel: tc.preferOriginChannel,
			}
			if !tc.nilDeliverer {
				deps.ChannelDeliverer = deliverer
			}

			d, c := newDispatchFor(t, h, KindReminder, deps)
			task := Task{
				ID:          "tdo",
				Kind:        KindReminder,
				Payload:     []byte(`{"text":"drink water"}`),
				NotifyRoute: tc.notifyRoute,
				IdentityID:  tc.identityID,
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
				if got := deliverer.calls[0].text; got != "drink water" {
					t.Fatalf("channel delivered text %q, want the reminder summary", got)
				}
			}

			notifierCalled := len(notif.texts) > 0
			if notifierCalled != tc.want.notifierCalled {
				t.Fatalf("Notifier called = %v, want %v (routes=%v texts=%v)", notifierCalled, tc.want.notifierCalled, notif.routes, notif.texts)
			}
			if tc.want.notifierCalled && tc.notifyRoute != "" {
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

//go:build db_integration

package pimwatch

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// The page holding one invitation from someone the account writes to, marked important.
func (r *watchRig) invitationPage() {
	r.t.Helper()
	invitation := r.urgentMail("m1")
	invitation.Signals.Invitation = true
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: []Change{invitation}, NextCursor: "c1"})
}

// The live run of 2026-11-02 told one invitation twice: as a mail and as the event it put on the
// calendar.
func TestAnInvitationLeavesTheAlertToAHealthyCalendar(t *testing.T) {
	r := newWatchRig(t)
	r.invitationPage()

	if out := r.tick(); out.Notice != "" {
		t.Fatalf("the mail told what the calendar tells: %q", out.Notice)
	}
	verdict, err := r.store.Verdict(context.Background(), r.identity, rigAccount, ResourceMail, "m1")
	if err != nil || verdict.Tier != TierToday {
		t.Fatalf("verdict = %+v, %v; the mail must stay in the digest", verdict, err)
	}
}

func TestAnInvitationAlertsWhenNoCalendarWillTellIt(t *testing.T) {
	ctx := context.Background()
	for name, spoil := range map[string]func(r *watchRig) error{
		"a failing calendar": func(r *watchRig) error {
			_, err := r.store.MarkFailing(ctx, r.identity, rigAccount, ResourceCalendar, "provider_transient", r.clock.now())
			return err
		},
		"a provider with no calendar": func(r *watchRig) error {
			return r.store.MarkUnsupported(ctx, r.identity, rigAccount, ResourceCalendar)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newWatchRig(t)
			if err := spoil(r); err != nil {
				t.Fatalf("spoil: %v", err)
			}
			r.invitationPage()

			mustContain(t, r.tick().Notice, "m1@acme.it: Subject of m1")
		})
	}
}

func TestAnInvitationAlertsBeforeTheCalendarHasACursor(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	if err := StartResource(ctx, r.pim, r.store, r.identity, "other", ResourceMail); err != nil {
		t.Fatalf("start: %v", err)
	}
	r.invitationPage()

	out, err := r.watcher.Tick(ctx, TickInput{IdentityID: r.identity, AccountID: "other", RunID: uuid.NewString()})
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	mustContain(t, out.Notice, "m1@acme.it: Subject of m1")
}

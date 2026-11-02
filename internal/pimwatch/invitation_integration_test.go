//go:build db_integration

package pimwatch

import (
	"context"
	"strings"
	"testing"
	"time"
)

// invitationPage holds one mail the ladder sends to `now`, with this subject.
func (r *watchRig) invitationPage(subject string) {
	r.t.Helper()
	invitation := r.urgentMail("m1")
	invitation.Subject = subject
	invitation.Signals.Invitation = true
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: []Change{invitation}, NextCursor: "c1"})
}

func newMeeting(id string, start time.Time) Change {
	change := eventChange(id, "fp-"+id, start)
	change.Type = ChangeAdded
	return change
}

// The live run of 2026-11-02 told every change to one meeting twice: as a mail and as the event,
// the two in the same tick.
func TestAnInvitationAndItsEventAreToldOnce(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.invitationPage("Invito: AUDIT - lun 2 nov")
	r.calendarPage("start-calendar", "cal-1", newMeeting("e1", r.clock.now().Add(2*time.Hour)))

	out := r.tick()
	mustContain(t, out.Notice, "New: Audit")
	if strings.Contains(out.Notice, "Invito") {
		t.Fatalf("the meeting was told twice: %q", out.Notice)
	}
	mustContain(t, out.Summary, "2 decided, 1 alerted")
	mail, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceMail, "m1")
	if err != nil || mail.Tier != TierNow {
		t.Fatalf("mail = %+v, %v", mail, err)
	}
	event, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceCalendar, "e1")
	if err != nil || event.Tier != TierToday {
		t.Fatalf("event = %+v, %v; it must stay in the digest and spend no second alert", event, err)
	}
}

// Each of these is a calendar that says nothing about the meeting, so the mail is the only alert.
func TestAnInvitationKeepsItsAlertWhenTheCalendarSaysNothing(t *testing.T) {
	ctx := context.Background()
	for name, arrange := range map[string]func(r *watchRig) error{
		"nothing on the calendar": func(*watchRig) error { return nil },
		"a meeting days away": func(r *watchRig) error {
			r.calendarPage("start-calendar", "cal-1", newMeeting("e1", r.clock.now().Add(72*time.Hour)))
			return nil
		},
		"a meeting older than the watch": func(r *watchRig) error {
			r.calendarPage("start-calendar", "cal-1", eventChange("e1", "fp", r.clock.now().Add(2*time.Hour)))
			return nil
		},
		"a meeting already begun": func(r *watchRig) error {
			r.calendarPage("start-calendar", "cal-1", newMeeting("e1", r.clock.now().Add(-3*time.Minute)))
			return nil
		},
		"a failing calendar": func(r *watchRig) error {
			r.pim.changeErr[ResourceCalendar] = transient()
			return nil
		},
		"a provider with no calendar": func(r *watchRig) error {
			return r.store.MarkUnsupported(ctx, r.identity, rigAccount, ResourceCalendar)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newWatchRig(t)
			if err := arrange(r); err != nil {
				t.Fatalf("arrange: %v", err)
			}
			r.invitationPage("Invito: Audit")

			mustContain(t, r.tick().Notice, "m1@acme.it: Invito: Audit")
		})
	}
}

func TestTwoAlertsStayApartUnlessTheyAreOneMeeting(t *testing.T) {
	soon := func(r *watchRig) Change { return newMeeting("e1", r.clock.now().Add(2*time.Hour)) }

	t.Run("an invitation to another meeting", func(t *testing.T) {
		r := newWatchRig(t)
		r.invitationPage("Invito: Budget")
		r.calendarPage("start-calendar", "cal-1", soon(r))

		mustContain(t, r.tick().Notice, "m1@acme.it: Invito: Budget", "New: Audit")
	})
	t.Run("a mail that only names the meeting", func(t *testing.T) {
		r := newWatchRig(t)
		mail := r.urgentMail("m1")
		mail.Subject = "Note per Audit"
		r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: []Change{mail}, NextCursor: "c1"})
		r.calendarPage("start-calendar", "cal-1", soon(r))

		mustContain(t, r.tick().Notice, "m1@acme.it: Note per Audit", "New: Audit")
	})
}

//go:build db_integration

package pimwatch

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// noon stands the rig's clock at midday of its own day, so a test that walks the clock on does not
// run into a midnight it did not ask for.
func (r *watchRig) noon() {
	r.clock.at = dayStart(r.clock.at, time.UTC).Add(12 * time.Hour)
}

func (r *watchRig) soonEvent(id, fingerprint string) Change {
	return Change{
		Type: ChangeUpdated, ID: id, CalendarID: "cal", Subject: "Audit", From: "organizer@acme.it",
		Event: &Event{Start: EventTime{Time: r.clock.at.Add(3 * time.Hour)}, Fingerprint: fingerprint},
	}
}

// The verdict row is rewritten on each move, so a count of rows saw one alert however many went
// out, and an event moved every five minutes pushed on every tick.
func TestOneEventMovedPastTheCapPushesFiveTimesAndThenWaits(t *testing.T) {
	r := newWatchRig(t)
	r.noon()
	r.commitEvents("seen", "ev1")
	cursor := "start-calendar"
	var pushes int
	for i := range 8 {
		next := fmt.Sprintf("cal-%d", i)
		r.pim.page(ResourceCalendar, cursor, ChangesPage{Changes: []Change{r.soonEvent("ev1", next)}, NextCursor: next})
		cursor = next
		out := r.tick()
		if out.Notice != "" {
			pushes++
		}
		if i == 0 {
			mustContain(t, out.Summary, "0 capped")
		}
		if i == 7 {
			mustContain(t, out.Summary, "1 decided, 0 alerted, 1 capped")
		}
		r.clock.advance(5 * time.Minute)
	}
	if pushes != 5 {
		t.Fatalf("%d pushes of one moved event, want the cap of 5", pushes)
	}
}

func TestMailAndCalendarShareTheAccountsCap(t *testing.T) {
	r := newWatchRig(t)
	r.noon()
	r.commitEvents("seen", "ev1", "ev2", "ev3")
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: changesOf(r.urgentMail, "m", 3), NextCursor: "c1"})
	r.pim.page(ResourceCalendar, "start-calendar", ChangesPage{Changes: []Change{
		r.soonEvent("ev1", "f1"), r.soonEvent("ev2", "f1"), r.soonEvent("ev3", "f1"),
	}, NextCursor: "cal-1"})

	out := r.tick()

	mustContain(t, out.Summary, "6 decided, 5 alerted, 1 capped")
	if got := strings.Count(out.Notice, "\n- "); got != 5 {
		t.Fatalf("notice:\n%s", out.Notice)
	}

	r.pim.page(ResourceMail, "c1", ChangesPage{Changes: []Change{r.urgentMail("m9")}, NextCursor: "c2"})
	mustContain(t, r.tick().Summary, "1 decided, 0 alerted, 1 capped")
}

// commitEvents gives events a row of their own, so that a change to them reads as a move: an
// `updated` event with no row is `today`.
func (r *watchRig) commitEvents(fingerprint string, ids ...string) {
	r.t.Helper()
	drafts := make([]VerdictDraft, 0, len(ids))
	for _, id := range ids {
		drafts = append(drafts, VerdictDraft{
			ItemID: id, CalendarID: "cal", Fingerprint: fingerprint, Event: true,
			Decision: Decision{Tier: TierToday, By: ByCalendar},
		})
	}
	_, err := r.store.CommitPage(context.Background(), r.identity, PageCommit{
		AccountID: rigAccount, Resource: ResourceCalendar, Cursor: "start-calendar", Drafts: drafts,
		DayStart: r.clock.at, Now: r.clock.at, Cap: 0,
	})
	if err != nil {
		r.t.Fatalf("commit events: %v", err)
	}
}

// The day is the person's: the count starts over at midnight in Rome, whatever UTC says.
func TestANewLocalDayStartsAtZero(t *testing.T) {
	r := newWatchRig(t)
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	r.watcher.Locale = fakeLocale{LangEN, rome}
	r.clock.at = dayStart(r.clock.at, rome).AddDate(0, 0, 1).Add(-30 * time.Minute)
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: changesOf(r.urgentMail, "a", 6), NextCursor: "c1"})
	mustContain(t, r.tick().Summary, "6 decided, 5 alerted, 1 capped")

	r.clock.advance(time.Hour)
	r.pim.page(ResourceMail, "c1", ChangesPage{Changes: changesOf(r.urgentMail, "b", 2), NextCursor: "c2"})
	mustContain(t, r.tick().Summary, "2 decided, 2 alerted, 0 capped")
}

func TestAReplayedPageDoesNotCountTwice(t *testing.T) {
	r := newWatchRig(t)
	r.noon()
	page := changesOf(r.urgentMail, "m", 3)
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: page, NextCursor: "c1"})
	r.pim.page(ResourceMail, "c1", ChangesPage{Changes: page, NextCursor: "c2"})

	mustContain(t, r.tick().Summary, "3 decided, 3 alerted, 0 capped")
	mustContain(t, r.tick().Summary, "0 decided, 0 alerted, 0 capped")

	r.pim.page(ResourceMail, "c2", ChangesPage{Changes: changesOf(r.urgentMail, "n", 3), NextCursor: "c3"})
	mustContain(t, r.tick().Summary, "3 decided, 2 alerted, 1 capped")
}

func TestARetoldAlertIsOnePush(t *testing.T) {
	r := newWatchRig(t)
	r.noon()
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: []Change{r.urgentMail("m1")}, NextCursor: "c1"})
	if _, _, err := r.run(); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	r.tick()

	alerts, err := r.store.AlertsToday(context.Background(), r.identity, rigAccount, dayStart(r.clock.at, time.UTC))
	if err != nil || alerts != 1 {
		t.Fatalf("alerts = %d, %v", alerts, err)
	}
}

func TestTheCockpitCountsThePushesTheCapCounts(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	r.noon()
	if err := r.service.Enable(ctx, r.identity, rigAccount); err != nil {
		t.Fatalf("enable: %v", err)
	}
	r.commitEvents("seen", "ev1")
	cursor := "start-calendar"
	for i := range 2 {
		next := fmt.Sprintf("cal-%d", i)
		r.pim.page(ResourceCalendar, cursor, ChangesPage{Changes: []Change{r.soonEvent("ev1", next)}, NextCursor: next})
		cursor = next
		r.tick()
	}

	status, err := r.service.Status(ctx, r.identity)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := status.Accounts[0].AlertsToday; got != 2 {
		t.Fatalf("alerts_today = %d, want the 2 pushes of one event", got)
	}
}

// An enable starts a cursor with nothing of the past watch on it, the count included, whichever
// way that watch stopped.
func TestStartingACursorAgainStartsTheCountAgain(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.noon()
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: changesOf(r.urgentMail, "m", 5), NextCursor: "c1"})
	r.tick()

	if err := r.store.StartCursor(ctx, r.identity, rigAccount, ResourceMail, "again", CursorOK); err != nil {
		t.Fatalf("start: %v", err)
	}
	alerts, err := r.store.AlertsToday(ctx, r.identity, rigAccount, dayStart(r.clock.at, time.UTC))
	if err != nil || alerts != 0 {
		t.Fatalf("alerts = %d, %v", alerts, err)
	}
}

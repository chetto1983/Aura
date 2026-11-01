//go:build db_integration

package pimwatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const lineTime = "Jan 2 15:04"

func (r *watchRig) calendarPage(cursor, next string, changes ...Change) {
	r.pim.page(ResourceCalendar, cursor, ChangesPage{Changes: changes, NextCursor: next})
}

func addedEvent(id string, start EventTime) Change {
	return Change{
		Type: ChangeAdded, ID: id, CalendarID: "cal", Subject: "Budget", From: "anna@acme.it",
		Event: &Event{Start: start, Fingerprint: "f1"},
	}
}

// deletedEvent is all Google guarantees of a deleted event: its id.
func deletedEvent(id string) Change {
	return Change{Type: ChangeDeleted, ID: id, CalendarID: "cal"}
}

func (r *watchRig) verdict(id string) Verdict {
	r.t.Helper()
	verdict, err := r.store.Verdict(context.Background(), r.identity, rigAccount, ResourceCalendar, id)
	if err != nil {
		r.t.Fatalf("verdict %s: %v", id, err)
	}
	return verdict
}

// A meeting deleted three hours before it starts is the alert the watch exists for, and the
// delete names only an id, so the row's start is what makes it `now`.
func TestADeletedEventInsideADayIsToldFromTheStartItsRowKept(t *testing.T) {
	r := newWatchRig(t)
	soon := r.clock.at.Add(3 * time.Hour)
	r.calendarPage("start-calendar", "c1", addedEvent("ev1", EventTime{Time: soon}))
	r.tick()

	delete(r.pim.items, "ev1")
	r.calendarPage("c1", "c2", deletedEvent("ev1"))
	out := r.tick()

	mustContain(t, out.Notice, "- Cancelled ("+soon.Format(lineTime)+")")
	if strings.Contains(out.Notice, "(no subject)") {
		t.Fatalf("a deleted event read as a mail with no subject:\n%s", out.Notice)
	}
	got := r.verdict("ev1")
	if got.Tier != TierNow || got.Change != WordCancelled || !got.Start.Time.Equal(soon.Truncate(time.Microsecond)) {
		t.Fatalf("verdict = %+v", got)
	}
}

func TestADeletedEventFarOffWaitsForTheDigestAndStillSaysSo(t *testing.T) {
	r := newWatchRig(t)
	far := r.clock.at.Add(48 * time.Hour)
	r.calendarPage("start-calendar", "c1", addedEvent("ev1", EventTime{Time: far}))
	r.tick()
	r.calendarPage("c1", "c2", deletedEvent("ev1"))
	if out := r.tick(); out.Notice != "" {
		t.Fatalf("far off, it pushed: %q", out.Notice)
	}
	r.clock.advance(time.Minute)

	out, err := r.digester(LangEN).Run(context.Background(), r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "[work] Cancelled ("+far.Format(lineTime)+")")
	if strings.Contains(out.Notice, "no longer there") {
		t.Fatalf("a cancelled meeting read as a mail that went:\n%s", out.Notice)
	}
}

func TestAnAllDayEventKeepsItsDateWhenItIsDeleted(t *testing.T) {
	r := newWatchRig(t)
	r.clock.at = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC).AddDate(1, 0, 0)
	day := r.clock.at.Add(24 * time.Hour).Truncate(24 * time.Hour)
	r.calendarPage("start-calendar", "c1", addedEvent("fair", EventTime{Time: day, AllDay: true}))
	r.tick()
	r.calendarPage("c1", "c2", deletedEvent("fair"))

	out := r.tick()

	mustContain(t, out.Notice, "- Cancelled ("+day.Format("Jan 2")+")")
	if got := r.verdict("fair"); !got.Start.AllDay {
		t.Fatalf("verdict = %+v", got)
	}
}

func TestADigestSaysWhatHappenedToAnEvent(t *testing.T) {
	r := newWatchRig(t)
	far := r.clock.at.Add(48 * time.Hour)
	moved := far.Add(time.Hour)
	r.pim.items["ev1"] = Item{
		ID: "ev1", CalendarID: "cal", From: "anna@acme.it", Subject: "Budget",
		Event: &Event{Start: EventTime{Time: moved}},
	}
	r.pim.items["ev2"] = Item{ID: "ev2", CalendarID: "cal", From: "anna@acme.it", Subject: "Audit"}
	second := addedEvent("ev2", EventTime{Time: far})
	r.calendarPage("start-calendar", "c1", addedEvent("ev1", EventTime{Time: far}), second)
	r.tick()
	again := eventChange("ev1", "f2", moved)
	r.calendarPage("c1", "c2", again)
	r.tick()
	r.clock.advance(time.Minute)

	out, err := r.digester(LangIT).Run(context.Background(), r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "anna@acme.it: Spostato: Budget", "anna@acme.it: Nuovo: Audit")
}

func TestARetoldCancellationIsNotDroppedAsGone(t *testing.T) {
	r := newWatchRig(t)
	soon := r.clock.at.Add(3 * time.Hour)
	r.calendarPage("start-calendar", "c1", addedEvent("ev1", EventTime{Time: soon}))
	r.tick()
	r.calendarPage("c1", "c2", deletedEvent("ev1"))
	if _, _, err := r.run(); err != nil {
		t.Fatalf("the run that dies: %v", err)
	}

	mustContain(t, r.tick().Notice, "- Cancelled ("+soon.Format(lineTime)+")")
}

func TestEveryEventVerdictKeepsItsStartAndAMailKeepsNone(t *testing.T) {
	r := newWatchRig(t)
	soon := r.clock.at.Add(3 * time.Hour)
	moved := soon.Add(time.Hour)
	r.calendarPage("start-calendar", "c1", addedEvent("ev1", EventTime{Time: soon}))
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: []Change{r.todayMail("m1")}, NextCursor: "m1"})
	r.tick()
	if got := r.verdict("ev1"); !got.Start.Time.Equal(soon.Truncate(time.Microsecond)) || got.Change != WordNew {
		t.Fatalf("added = %+v", got)
	}

	r.calendarPage("c1", "c2", eventChange("ev1", "f2", moved))
	r.tick()
	if got := r.verdict("ev1"); !got.Start.Time.Equal(moved.Truncate(time.Microsecond)) || got.Change != WordMoved {
		t.Fatalf("moved = %+v", got)
	}

	mail, err := r.store.Verdict(context.Background(), r.identity, rigAccount, ResourceMail, "m1")
	if err != nil || !mail.Start.IsZero() || mail.Change != "" {
		t.Fatalf("mail = %+v, %v", mail, err)
	}
}

// Nothing names it, so nothing can be said: the limit the plan records.
func TestADeleteWithNoRowAndNoStartStaysSilent(t *testing.T) {
	r := newWatchRig(t)
	r.calendarPage("start-calendar", "c1", deletedEvent("unknown"))

	if out := r.tick(); out.Notice != "" {
		t.Fatalf("notice = %q", out.Notice)
	}
	if _, err := r.store.Verdict(context.Background(), r.identity, rigAccount, ResourceCalendar, "unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("it left a row: %v", err)
	}
}

func TestAMeetingMovedAwayIsToldByTheTimeTheWatchKept(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	soon := r.clock.at.Add(3 * time.Hour)
	nextWeek := r.clock.at.Add(7 * 24 * time.Hour)
	r.pim.page(ResourceCalendar, "start-calendar", ChangesPage{Changes: []Change{
		{Type: ChangeAdded, ID: "ev1", CalendarID: "cal", Subject: "Audit", From: "organizer@acme.it", Event: &Event{Start: EventTime{Time: soon}, Fingerprint: "f1"}},
	}, NextCursor: "c1"})
	if err := r.store.StartCursor(ctx, r.identity, rigAccount, ResourceCalendar, "start-calendar", CursorOK); err != nil {
		t.Fatalf("start: %v", err)
	}
	r.tick()

	r.pim.page(ResourceCalendar, "c1", ChangesPage{Changes: []Change{eventChange("ev1", "f2", nextWeek)}, NextCursor: "c2"})
	mustContain(t, r.tick().Notice, "Moved: Audit")

	r.pim.page(ResourceCalendar, "c2", ChangesPage{Changes: []Change{eventChange("ev1", "f3", nextWeek.Add(24*time.Hour))}, NextCursor: "c3"})
	if out := r.tick(); out.Notice != "" {
		t.Fatalf("a move from a far time to another far time alerted: %q", out.Notice)
	}
	verdict, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceCalendar, "ev1")
	if err != nil {
		t.Fatalf("verdict: %v", err)
	}
	if verdict.Tier != TierToday || !verdict.Start.Time.Equal(nextWeek.Add(24*time.Hour).Truncate(time.Microsecond)) {
		t.Fatalf("verdict = tier %s, start %s: the second move must replace the kept start", verdict.Tier, verdict.Start.Time)
	}
}

// Microsoft writes a delete with no event, so the row must know the meeting was the person's own.
func TestThePersonsOwnMeetingDeletedInsideADayAlertsNobody(t *testing.T) {
	r := newWatchRig(t)
	soon := r.clock.at.Add(3 * time.Hour)
	own := addedEvent("ev1", EventTime{Time: soon})
	own.Event.OrganizerIsSelf = true
	r.calendarPage("start-calendar", "c1", own)
	r.tick()

	r.calendarPage("c1", "c2", deletedEvent("ev1"))
	if out := r.tick(); out.Notice != "" {
		t.Fatalf("the person's own delete pushed: %q", out.Notice)
	}
	if got := r.verdict("ev1"); got.Tier != TierLater || got.Change != WordCancelled {
		t.Fatalf("verdict = %+v", got)
	}
}

// The organizer cancels, the person then removes it from the calendar: one alert, not two.
func TestARemovalAfterAToldCancellationSaysNothingAndLeavesTheRow(t *testing.T) {
	r := newWatchRig(t)
	soon := r.clock.at.Add(3 * time.Hour)
	r.calendarPage("start-calendar", "c1", addedEvent("ev1", EventTime{Time: soon}))
	r.tick()
	cancelled := eventChange("ev1", "f2", soon)
	cancelled.Event.Status = EventCancelled
	r.calendarPage("c1", "c2", cancelled)
	mustContain(t, r.tick().Notice, "Cancelled")
	before := r.verdict("ev1")

	r.calendarPage("c2", "c3", deletedEvent("ev1"))
	if out := r.tick(); out.Notice != "" {
		t.Fatalf("the removal pushed again: %q", out.Notice)
	}
	if after := r.verdict("ev1"); after.Fingerprint != before.Fingerprint || after.Change != WordCancelled || !after.DecidedAt.Equal(before.DecidedAt) {
		t.Fatalf("the row moved: before %+v, after %+v", before, after)
	}
}

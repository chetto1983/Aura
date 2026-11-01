//go:build db_integration

package pimwatch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// answeringPIM is a sidecar answering each resource with its own body, for the runs that have to
// go through the real client.
func answeringPIM(t *testing.T, r *watchRig, answer func(resource string, w http.ResponseWriter)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Resource string `json:"resource"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		answer(body.Resource, w)
	}))
	t.Cleanup(srv.Close)
	client := NewClient(srv.URL, &staticToken{token: "t"})
	r.watcher.PIM = client
	return client
}

// A page that cannot finish inside the timeout is asked again unchanged. It must not read as a
// quiet account: after the hour, one alarm says so, and the cursor waits for it.
func TestAPageThatKeepsTimingOutReachesTheHourAlarmOnce(t *testing.T) {
	r := newWatchRig(t)
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		select {
		case <-released:
		case <-req.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(released)
	client := NewClient(srv.URL, &staticToken{token: "t"})
	client.http.Timeout = 20 * time.Millisecond
	r.watcher.PIM = client
	before := r.cursor(ResourceMail).Value

	var alarms []int
	const ticksInAnHour = 12
	for i := range ticksInAnHour + 3 {
		out := r.tick()
		if strings.Contains(out.Notice, "the mail") {
			alarms = append(alarms, i)
		}
		mustContain(t, out.Summary, "failing: mail pim_unreachable")
		r.clock.advance(5 * time.Minute)
	}

	if len(alarms) != 1 || alarms[0] != ticksInAnHour {
		t.Fatalf("alarms on ticks %v, want only %d", alarms, ticksInAnHour)
	}
	got := r.cursor(ResourceMail)
	if got.Value != before || got.Status != CursorFailing || got.LastError != "pim_unreachable" {
		t.Fatalf("cursor = %+v", got)
	}
}

// A run always gets its first page, at the worst the PIM is allowed. What it does not get after
// that is a second resource: at 60 seconds the reserve no longer fits a two-minute bound, so the
// calendar waits for the next tick for as long as the mail stays that slow.
func TestAMailPageAtItsWorstLeavesTheCalendarForTheNextTick(t *testing.T) {
	r := newWatchRig(t)
	r.pim.changeErr[ResourceMail] = transient()
	ctx, cancel := context.WithDeadline(context.Background(), r.clock.at.Add(2*time.Minute))
	defer cancel()
	r.pim.onChanges = func(string, string) { r.clock.advance(pimRequestTimeout) }
	mailBefore, calendarBefore := r.pim.calls[ResourceMail], r.pim.calls[ResourceCalendar]

	out, err := r.watcher.Tick(ctx, TickInput{IdentityID: r.identity, AccountID: rigAccount, RunID: uuid.NewString()})
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if r.pim.calls[ResourceMail] != mailBefore+1 || r.pim.calls[ResourceCalendar] != calendarBefore {
		t.Fatalf("mail read %d times, calendar %d", r.pim.calls[ResourceMail]-mailBefore, r.pim.calls[ResourceCalendar]-calendarBefore)
	}
	mustContain(t, out.Summary, "failing: mail provider_transient", "backlog continues on the next tick")
}

func TestAnAccountThePIMDoesNotHaveIsNamedInTheOneReport(t *testing.T) {
	r := newWatchRig(t)
	var calendarAsked atomic.Bool
	answeringPIM(t, r, func(resource string, w http.ResponseWriter) {
		if resource == ResourceCalendar {
			calendarAsked.Store(true)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"account_not_found"}`))
	})

	_, out, err := r.run()
	hardErr, ok := errors.AsType[*HardFailure](err)
	if !ok || !hardErr.SuppressSchedulerNotification() {
		t.Fatalf("err = %v", err)
	}
	mustContain(t, hardErr.Error(), rigAccount, "account_not_found")
	mustContain(t, out.Summary, "hard failure: mail account_not_found")
	if got := r.cursor(ResourceMail); got.LastError != "account_not_found" {
		t.Fatalf("cursor = %+v", got)
	}
	if calendarAsked.Load() {
		t.Fatal("a second report could follow: the calendar was read after a hard failure")
	}
}

// Everything the fork writes, through the real client: one event of any shape must not stop the
// page. The clock stands where the fixture's dates mean what the test says.
func TestEveryShapeTheForkWritesPassesThroughAPage(t *testing.T) {
	r := newWatchRig(t)
	r.clock.at = time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	shapes := loadContract(t).OtherShapes
	answeringPIM(t, r, func(resource string, w http.ResponseWriter) {
		if resource == ResourceCalendar {
			_, _ = w.Write(shapes)
			return
		}
		_, _ = w.Write([]byte(`{"changes":[],"nextCursor":"start-mail","hasMore":false,"resetRequired":false}`))
	})

	out := r.tick()

	if got := r.cursor(ResourceCalendar).Value; got != "cur-3" {
		t.Fatalf("cursor = %q", got)
	}
	ctx := context.Background()
	for id, want := range map[string]Tier{"e-2": TierToday, "e-4": TierNow, "e-5": TierToday} {
		verdict, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceCalendar, id)
		if err != nil || verdict.Tier != want {
			t.Fatalf("%s = %+v, %v, want %s", id, verdict, err, want)
		}
	}
	if _, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceCalendar, "e-3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a delete with no row and no start left one: %v", err)
	}
	mustContain(t, out.Notice, "anna@acme.it: Cancelled: Riunione annullata")
}

// All day on the third has begun for a person in Tokyo while it is still the second in UTC.
func TestAnAllDayEventIsReadInThePersonsZone(t *testing.T) {
	r := newWatchRig(t)
	r.watcher.Locale = fakeLocale{LangEN, time.FixedZone("tokyo", 9*60*60)}
	r.clock.at = time.Date(2026, 11, 2, 20, 0, 0, 0, time.UTC)
	start := EventTime{Time: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC), AllDay: true}
	r.pim.page(ResourceCalendar, "start-calendar", ChangesPage{Changes: []Change{
		{Type: ChangeAdded, ID: "fair", CalendarID: "cal", Subject: "Fiera", Event: &Event{Start: start, Fingerprint: "f1"}},
	}, NextCursor: "c1"})

	r.tick()

	verdict, err := r.store.Verdict(context.Background(), r.identity, rigAccount, ResourceCalendar, "fair")
	if err != nil || verdict.Tier != TierToday {
		t.Fatalf("verdict = %+v, %v", verdict, err)
	}
}

func TestACancelledMeetingNobodySawIsTold(t *testing.T) {
	r := newWatchRig(t)
	soon := r.clock.at.Add(3 * time.Hour)
	cancelled := func(id string, self bool) Change {
		return Change{
			Type: ChangeUpdated, ID: id, CalendarID: "cal", Subject: "Budget", From: "anna@acme.it",
			Event: &Event{Start: EventTime{Time: soon}, Status: EventCancelled, OrganizerIsSelf: self, Fingerprint: "f-" + id},
		}
	}
	r.pim.page(ResourceCalendar, "start-calendar", ChangesPage{
		Changes: []Change{cancelled("theirs", false), cancelled("mine", true)}, NextCursor: "c1",
	})

	mustContain(t, r.tick().Notice, "anna@acme.it: Cancelled: Budget")

	ctx := context.Background()
	theirs, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceCalendar, "theirs")
	if err != nil || theirs.Tier != TierNow {
		t.Fatalf("theirs = %+v, %v", theirs, err)
	}
	mine, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceCalendar, "mine")
	if err != nil || mine.Tier != TierLater {
		t.Fatalf("mine = %+v, %v", mine, err)
	}
}

func TestADeleteIsToldOnlyWhenTheWatchKnewTheEvent(t *testing.T) {
	r := newWatchRig(t)
	soon := r.clock.at.Add(3 * time.Hour)
	r.pim.page(ResourceCalendar, "start-calendar", ChangesPage{Changes: []Change{
		{Type: ChangeAdded, ID: "known", CalendarID: "cal", Subject: "Budget", Event: &Event{Start: EventTime{Time: soon}, Fingerprint: "f1"}},
	}, NextCursor: "c1"})
	r.tick()

	r.pim.page(ResourceCalendar, "c1", ChangesPage{Changes: []Change{
		{Type: ChangeDeleted, ID: "unknown", CalendarID: "cal"},
	}, NextCursor: "c2"})
	if out := r.tick(); out.Notice != "" {
		t.Fatalf("a delete nobody can describe spoke: %q", out.Notice)
	}

	ctx := context.Background()
	if _, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceCalendar, "unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("it left a row: %v", err)
	}

	r.pim.page(ResourceCalendar, "c2", ChangesPage{Changes: []Change{
		{Type: ChangeDeleted, ID: "known", CalendarID: "cal"},
	}, NextCursor: "c3"})
	r.tick()
	verdict, err := r.store.Verdict(ctx, r.identity, rigAccount, ResourceCalendar, "known")
	if err != nil || verdict.Tier != TierNow || verdict.Fingerprint != deletedFingerprint {
		t.Fatalf("known = %+v, %v", verdict, err)
	}
}

func TestANoticeShowsTheNameBesideTheAddress(t *testing.T) {
	r := newWatchRig(t)
	change := r.urgentMail("m1")
	change.From, change.FromName = "mario@acme.it", "Mario Rossi"
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: []Change{change}, NextCursor: "c1"})

	out := r.tick()

	mustContain(t, out.Quoted, "Mario Rossi <mario@acme.it>: Subject of m1")
	mustContain(t, out.Notice, "Mario Rossi <mario@acme.it>: Subject of m1")
	if strings.Contains(out.Head, "Mario") || strings.Contains(out.Refs, "Mario") {
		t.Fatalf("the name left the frame: head %q refs %q", out.Head, out.Refs)
	}
	if strings.Contains(out.Summary, "Mario") {
		t.Fatalf("the name reached the ledger: %q", out.Summary)
	}
}

//go:build db_integration

package pimwatch

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func (r *watchRig) digester(lang Lang) *Digester {
	return &Digester{Store: r.store, PIM: r.pim, Locale: fakeLocale{lang, time.UTC}, Now: r.clock.now}
}

func TestTheDigestListsNowFirstThenLabelsAndCountsAsAWholeDay(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	label, err := r.store.AddLabel(ctx, r.identity, "Cliente", "", 0)
	if err != nil {
		t.Fatalf("label: %v", err)
	}
	r.clock.advance(time.Minute)
	r.commit(rigAccount, r.draft("t1", TierToday, ""), r.draft("t2", TierToday, label.ID), r.draft("n1", TierNow, ""), r.draft("skip", TierLater, ""))
	r.clock.advance(time.Minute)

	out, err := r.digester(LangIT).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	want := strings.Join([]string{
		"Riepilogo di posta e calendario", "Urgenti", "1. [work] n1@acme.it: Subject of n1",
		"Cliente", "2. [work] t2@acme.it: Subject of t2", "Da smistare", "3. [work] t1@acme.it: Subject of t1",
	}, "\n")
	if out.Notice != want {
		t.Fatalf("digest:\n%s\nwant:\n%s", out.Notice, want)
	}
	mustContain(t, out.Summary, "3 in the window, 3 shown")
}

func TestAnUnreadableAccountIsNamedAndTheWindowStays(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.clock.advance(time.Minute)
	r.commit("work", r.draft("w1", TierToday, ""))
	r.commit("home", r.draft("h1", TierToday, ""))
	r.clock.advance(time.Minute)
	r.pim.itemsErr["home"] = transient()

	out, err := r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "w1@acme.it", "I could not read: home.")
	mustContain(t, out.Summary, "unreadable: home")

	delete(r.pim.itemsErr, "home")
	r.clock.advance(time.Minute)
	out, err = r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("second digest: %v", err)
	}
	mustContain(t, out.Notice, "h1@acme.it", "w1@acme.it")
}

// An account that will not come back must not hold everybody's digest: held, its old rows filled
// the first twenty lines for good and nothing newer was ever read.
func TestAHardFailingAccountDoesNotFreezeEveryonesDigest(t *testing.T) {
	for _, code := range []string{"account_not_found", "permission_denied", "provider_auth", "pim_not_authorized"} {
		t.Run(code, func(t *testing.T) {
			r := newWatchRig(t)
			ctx := context.Background()
			r.clock.advance(time.Minute)
			old := make([]VerdictDraft, 0, 25)
			for i := range 25 {
				old = append(old, r.draft(fmt.Sprintf("x%02d", i), TierToday, ""))
			}
			r.commit("gone", old...)
			r.clock.advance(time.Minute)
			r.pim.itemsErr["gone"] = &PIMError{Class: FailureHard, Code: code}

			first, err := r.digester(LangEN).Run(ctx, r.identity)
			if err != nil {
				t.Fatalf("first digest: %v", err)
			}
			mustContain(t, first.Notice, "I could not read: gone.", "stay in the account's day")
			if strings.Contains(first.Notice, "next digest") {
				t.Fatalf("promised a line that will not come back:\n%s", first.Notice)
			}
			mustContain(t, first.Summary, "unreadable: gone")

			r.clock.advance(24 * time.Hour)
			r.commit(rigAccount, r.draft("fresh", TierToday, ""))
			r.clock.advance(time.Minute)
			second, err := r.digester(LangEN).Run(ctx, r.identity)
			if err != nil {
				t.Fatalf("second digest: %v", err)
			}
			mustContain(t, second.Notice, "1. [work] fresh@acme.it")
			if strings.Contains(second.Notice, "gone") {
				t.Fatalf("the next day named the account again:\n%s", second.Notice)
			}
		})
	}
}

func TestAnAccountWithNothingToShowIsStillNamed(t *testing.T) {
	r := newWatchRig(t)
	r.clock.advance(time.Minute)
	r.commit("gone", r.draft("x1", TierToday, ""))
	r.clock.advance(time.Minute)
	r.pim.itemsErr["gone"] = hard()

	out, err := r.digester(LangIT).Run(context.Background(), r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "Riepilogo di posta e calendario", "Non ho potuto leggere: gone.", "restano nella giornata")
}

// A transient failure among them keeps the promise that its lines come back, so the window holds
// for everyone that day, the hard-failing account's lines with it.
func TestATransientFailureHoldsTheWindowWhateverElseFailed(t *testing.T) {
	for _, code := range []string{"pim_unreachable", "provider_transient"} {
		t.Run(code, func(t *testing.T) {
			r := newWatchRig(t)
			ctx := context.Background()
			r.clock.advance(time.Minute)
			r.commit("down", r.draft("d1", TierToday, ""))
			r.commit("gone", r.draft("g1", TierToday, ""))
			r.clock.advance(time.Minute)
			r.pim.itemsErr["down"] = &PIMError{Class: FailureTransient, Code: code}
			r.pim.itemsErr["gone"] = hard()

			out, err := r.digester(LangEN).Run(ctx, r.identity)
			if err != nil {
				t.Fatalf("digest: %v", err)
			}
			mustContain(t, out.Notice, "I could not read: down. Its items come back", "I could not read: gone.")
			settings, err := r.store.Settings(ctx, r.identity)
			if err != nil {
				t.Fatalf("settings: %v", err)
			}
			if !settings.LastDigestAt.IsZero() {
				t.Fatalf("the window moved to %v past a failure that will pass", settings.LastDigestAt)
			}
		})
	}
}

func TestTheWindowMovesAfterAGoodDigestAndHoldsWhatCameAfter(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	r.clock.advance(time.Minute)
	r.commit(rigAccount, r.draft("before", TierToday, ""))
	r.clock.advance(time.Minute)
	if _, err := r.digester(LangEN).Run(ctx, r.identity); err != nil {
		t.Fatalf("digest: %v", err)
	}

	r.clock.advance(time.Minute)
	r.commit(rigAccount, r.draft("after", TierToday, ""))
	r.clock.advance(time.Minute)
	out, err := r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "after@acme.it")
	if strings.Contains(out.Notice, "before@acme.it") {
		t.Fatalf("the window did not move:\n%s", out.Notice)
	}
}

func TestAnEmptyWindowSendsNothingAndStillMoves(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	out, err := r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if out.Notice != "" || out.Head != "" {
		t.Fatalf("an empty digest spoke: %+v", out)
	}
	settings, err := r.store.Settings(ctx, r.identity)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if !settings.LastDigestAt.Equal(r.clock.at.Truncate(time.Microsecond)) {
		t.Fatalf("window at %v, want %v", settings.LastDigestAt, r.clock.at)
	}
}

func TestWhatThePhoneAlreadySaidLeavesTheDigest(t *testing.T) {
	r := newWatchRig(t)
	r.clock.advance(time.Minute)
	r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: []Change{r.urgentMail("told"), r.todayMail("fresh")}, NextCursor: "c1"})
	r.tick()
	r.tick()
	r.clock.advance(time.Minute)

	out, err := r.digester(LangEN).Run(context.Background(), r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "fresh@acme.it")
	if strings.Contains(out.Notice, "told@acme.it") {
		t.Fatalf("told again:\n%s", out.Notice)
	}
	mustContain(t, out.Summary, "2 in the window, 1 shown")
}

// A run completes before it notifies, so `notified` says the run ended. Only the queue knows the
// push never arrived, and the digest is that alert's second chance.
func TestAnAlertThatNeverArrivedStandsInTheDigest(t *testing.T) {
	for status, shown := range map[string]bool{"failed": true, "undeliverable": true, "pending": true, "delivered": false} {
		r := newWatchRig(t)
		ctx := context.Background()
		r.clock.advance(time.Minute)
		r.pim.page(ResourceMail, "start-mail", ChangesPage{Changes: []Change{r.urgentMail("lost"), r.todayMail("fresh")}, NextCursor: "c1"})
		run, _, err := r.run()
		if err != nil {
			t.Fatalf("tick: %v", err)
		}
		r.ledger.completed[run] = true
		r.tick()
		task := uuid.NewString()
		for _, step := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO aura.scheduler_tasks (id, kind, schedule_kind, every_minutes, tz, payload, status, next_run_at, notify_route, identity_id)
				VALUES ($1, 'pim_watch', 'every', 5, 'UTC', '{}'::jsonb, 'active', now(), 'none', $2)`, []any{task, r.identity}},
			{`INSERT INTO aura.agent_job_runs (id, task_id, status) VALUES ($1, $2, 'completed')`, []any{run, task}},
			{`INSERT INTO aura.pending_notifications (id, run_id, notify_route, body, notify_after, status, identity_id)
				VALUES ($1, $2, 'telegram', 'alert', now(), $3, $4)`, []any{uuid.NewString(), run, status, r.identity}},
		} {
			if _, err := r.store.pool.Exec(ctx, step.sql, step.args...); err != nil {
				t.Fatalf("%s: seed: %v", status, err)
			}
		}
		t.Cleanup(func() { _, _ = r.store.pool.Exec(ctx, `DELETE FROM aura.scheduler_tasks WHERE id = $1`, task) })
		r.clock.advance(time.Minute)

		out, err := r.digester(LangEN).Run(ctx, r.identity)
		if err != nil {
			t.Fatalf("%s: digest: %v", status, err)
		}
		if got := strings.Contains(out.Notice, "lost@acme.it"); got != shown {
			t.Fatalf("%s: shown %v:\n%s", status, got, out.Notice)
		}
	}
}

func TestADigestShowsTwentyLinesAndReadsTheWindow(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	drafts := make([]VerdictDraft, 0, 30)
	for _, change := range changesOf(r.todayMail, "d", 30) {
		drafts = append(drafts, VerdictDraft{ItemID: change.ID, Decision: Decision{Tier: TierToday, By: ByDefault}})
	}
	r.clock.advance(time.Minute)
	r.commit(rigAccount, drafts...)
	r.clock.advance(time.Minute)

	out, err := r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "20. [work]", "And 10 more.")
	if strings.Contains(out.Notice, "21. [work]") {
		t.Fatalf("past the cap:\n%s", out.Notice)
	}
	read := 0
	for _, refs := range r.pim.reads {
		read += len(refs)
	}
	if read != 30 {
		t.Fatalf("read %d items of a window of 30", read)
	}
}

func TestAGoneMailLeavesTheDigest(t *testing.T) {
	r := newWatchRig(t)
	r.clock.advance(time.Minute)
	r.commit(rigAccount, r.draft("kept", TierToday, ""), r.draft("gone", TierToday, ""))
	delete(r.pim.items, "gone")
	r.clock.advance(time.Minute)

	out, err := r.digester(LangEN).Run(context.Background(), r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "kept@acme.it")
	if strings.Contains(out.Notice, "(no longer there)") {
		t.Fatalf("a line with nothing in it:\n%s", out.Notice)
	}
}

func TestARemovedLabelsMailsReadAsUnsorted(t *testing.T) {
	r := newWatchRig(t)
	ctx := context.Background()
	label, err := r.store.AddLabel(ctx, r.identity, "Ex", "", 0)
	if err != nil {
		t.Fatalf("label: %v", err)
	}
	r.clock.advance(time.Minute)
	r.commit(rigAccount, r.draft("m1", TierToday, label.ID))
	if err := r.store.RemoveLabel(ctx, r.identity, label.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	r.clock.advance(time.Minute)

	out, err := r.digester(LangEN).Run(ctx, r.identity)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	mustContain(t, out.Notice, "To sort\n1. [work] m1@acme.it")
}

// A provider's id is text: on IMAP "10" sorts before "9", and a burst arrives in one page, so ties
// on the page's clock used to read as a shuffle. changesOf pads its ids, which hid this.
func TestADigestKeepsTheOrderAPageArrivedIn(t *testing.T) {
	r := newWatchRig(t)
	drafts := make([]VerdictDraft, 0, 12)
	for i := 1; i <= 12; i++ {
		drafts = append(drafts, VerdictDraft{ItemID: fmt.Sprintf("INBOX/1/%d", i), Decision: Decision{Tier: TierToday, By: ByDefault}})
	}
	r.commit(rigAccount, drafts...)
	r.clock.advance(time.Minute)

	verdicts, err := r.store.DigestVerdicts(context.Background(), r.identity, r.clock.at.Add(-time.Hour), r.clock.at)
	if err != nil {
		t.Fatalf("digest verdicts: %v", err)
	}
	if len(verdicts) != len(drafts) {
		t.Fatalf("%d verdicts", len(verdicts))
	}
	for i, verdict := range verdicts {
		if verdict.ItemID != drafts[i].ItemID {
			t.Fatalf("line %d is %s, want %s", i, verdict.ItemID, drafts[i].ItemID)
		}
	}
}

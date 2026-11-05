//go:build db_integration

package pimwatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/decide"
)

func TestACorrectionDiscardsAndRescues(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	spam, err := r.service.AddLabel(ctx, r.identity, LabelInput{Name: "Spam", Discard: true})
	if err != nil {
		t.Fatal(err)
	}
	client, err := r.service.AddLabel(ctx, r.identity, LabelInput{Name: "Cliente"})
	if err != nil {
		t.Fatal(err)
	}
	r.commit(rigAccount, r.draft("m1", TierNow, ""), r.draft("m2", TierToday, spam.ID))
	r.pim.items["m1"] = Item{ID: "m1", From: "m1@one.example"}

	label := correctionOf("m1", "")
	label.LabelID = &spam.ID
	verdict, _, err := r.service.Correct(ctx, r.identity, label)
	if err != nil || verdict.Tier != TierLater || !verdict.Discarded || verdict.By != ByPerson {
		t.Fatalf("a discard label: %+v, %v", verdict, err)
	}

	verdict, _, err = r.service.Correct(ctx, r.identity, correctionOf("m1", TierToday))
	if err != nil || verdict.Tier != TierToday || verdict.Discarded {
		t.Fatalf("a tier after it: %+v, %v", verdict, err)
	}

	r.pim.items["m2"] = Item{ID: "m2", From: "m2@one.example"}
	if _, _, err = r.service.Correct(ctx, r.identity, Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m2", LabelID: &spam.ID}); err != nil {
		t.Fatal(err)
	}
	rescue := Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m2", LabelID: &client.ID}
	verdict, _, err = r.service.Correct(ctx, r.identity, rescue)
	if err != nil || verdict.Tier != TierLater || verdict.Discarded || verdict.LabelID != client.ID {
		t.Fatalf("a keep label: %+v, %v", verdict, err)
	}

	timed, err := r.service.AddLabel(ctx, r.identity, LabelInput{Name: "Spam with a tier", Tier: TierToday, Discard: true})
	if err != nil {
		t.Fatal(err)
	}
	r.commit(rigAccount, r.draft("m3", TierNow, ""))
	r.pim.items["m3"] = Item{ID: "m3", From: "m3@one.example"}
	verdict, _, err = r.service.Correct(ctx, r.identity, Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m3", LabelID: &timed.ID})
	if err != nil || verdict.Tier != TierLater || !verdict.Discarded {
		t.Fatalf("a label with a tier and a discard: %+v, %v", verdict, err)
	}
}

func TestACorrectionFindsAnUppercaseLabelId(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	spam, err := r.service.AddLabel(ctx, r.identity, LabelInput{Name: "Spam", Discard: true})
	if err != nil {
		t.Fatal(err)
	}
	r.commit(rigAccount, r.draft("m1", TierToday, ""))
	upper := strings.ToUpper(spam.ID)
	verdict, _, err := r.service.Correct(ctx, r.identity, Correction{AccountID: rigAccount, Resource: ResourceMail, ItemID: "m1", LabelID: &upper})
	if err != nil || !verdict.Discarded || verdict.Tier != TierLater {
		t.Fatalf("an uppercase id: %+v, %v", verdict, err)
	}
}

func TestDiscardedMailDoesNotTakeTheDaysRows(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	drafts := []VerdictDraft{r.draft("kept", TierToday, "")}
	for i := range decisionsLimit {
		draft := r.draft(fmt.Sprintf("junk%03d", i), TierLater, "")
		draft.Decision.Discarded = true
		drafts = append(drafts, draft)
	}
	r.commit(rigAccount, drafts...)

	day, err := r.service.Decisions(ctx, r.identity, rigAccount, "", false)
	if err != nil || len(day.Lines) != 1 || day.Discarded != decisionsLimit || day.Lines[0].ItemID != "kept" {
		t.Fatalf("kept: %d lines, %d discarded, %v", len(day.Lines), day.Discarded, err)
	}
	day, err = r.service.Decisions(ctx, r.identity, rigAccount, "", true)
	if err != nil || len(day.Lines) != decisionsLimit || !day.Lines[0].Discarded {
		t.Fatalf("discarded: %d lines, %v", len(day.Lines), err)
	}
}

func TestTheDigestWindowListsTheKeptMailAndCountsTheDiscarded(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	junk := r.draft("junk", TierLater, "")
	junk.Decision.Discarded = true
	rest := r.draft("rest", TierToday, "")
	hidden := r.draft("hidden", TierToday, "")
	hidden.Decision.Discarded = true
	r.commit(rigAccount, junk, rest, hidden)
	since, until := r.clock.at.Add(-time.Hour), r.clock.at.Add(time.Hour)

	listed, err := r.store.DigestVerdicts(ctx, r.identity, since, until)
	if err != nil || len(listed) != 1 || listed[0].ItemID != "rest" {
		t.Fatalf("listed %+v, %v", listed, err)
	}
	if n, err := r.store.DiscardedInWindow(ctx, r.identity, since, until); err != nil || n != 2 {
		t.Fatalf("window: %d, %v", n, err)
	}
	if n, err := r.store.DiscardedOfDay(ctx, r.identity, rigAccount, since, until); err != nil || n != 2 {
		t.Fatalf("day: %d, %v", n, err)
	}
}

type fakeRouteReader struct {
	route decide.Route
	err   error
}

func (f fakeRouteReader) Route(context.Context) (decide.Route, error) { return f.route, f.err }

// failDecide stands in for the tick, which records the model's failure on the account.
func (r *serviceRig) failDecide(account, class string, at time.Time) {
	r.t.Helper()
	err := db.WithIdentityTxRaw(context.Background(), r.store.pool, r.identity, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO aura.pim_watch_accounts (identity_id, account_id, decide_error, decide_failed_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (identity_id, account_id) DO UPDATE SET decide_error = EXCLUDED.decide_error, decide_failed_at = EXCLUDED.decide_failed_at`,
			r.identity, account, class, at)
		return err
	})
	if err != nil {
		r.t.Fatalf("record the failure: %v", err)
	}
}

func TestStatusCarriesTheRouteAndEachAccountsConsentAndFailure(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	for _, account := range []string{"home", "work"} {
		if err := r.service.Enable(ctx, r.identity, account); err != nil {
			t.Fatalf("enable: %v", err)
		}
	}
	if err := r.service.SetCloudDecide(ctx, r.identity, "home", true); err != nil {
		t.Fatal(err)
	}
	failedAt := r.clock.at.Truncate(time.Second)
	r.failDecide("work", "rejected", failedAt)

	status, err := r.service.Status(ctx, r.identity)
	if err != nil || status.DecideRoute != "" {
		t.Fatalf("with no reader: %q, %v", status.DecideRoute, err)
	}
	r.service.Decide = fakeRouteReader{route: decide.Route{BaseURL: "https://router.example", Model: "vendor/model"}}
	status, err = r.service.Status(ctx, r.identity)
	if err != nil || status.DecideRoute != "openrouter" {
		t.Fatalf("hosted: %q, %v", status.DecideRoute, err)
	}
	byAccount := map[string]AccountStatus{}
	for _, account := range status.Accounts {
		byAccount[account.AccountID] = account
	}
	home, work := byAccount["home"], byAccount["work"]
	if !home.CloudDecide || home.DecideError != "" || !home.DecideFailedAt.IsZero() {
		t.Fatalf("home = %+v", home)
	}
	if work.CloudDecide || work.DecideError != "rejected" || !work.DecideFailedAt.Equal(failedAt) {
		t.Fatalf("work = %+v", work)
	}

	for route, want := range map[string]decide.Route{"local": {BaseURL: "http://sidecar.example"}, "": {}} {
		r.service.Decide = fakeRouteReader{route: want}
		if status, err = r.service.Status(ctx, r.identity); err != nil || status.DecideRoute != route {
			t.Fatalf("route %+v: %q, %v", want, status.DecideRoute, err)
		}
	}
	r.service.Decide = fakeRouteReader{err: errors.New("settings unreadable")}
	status, err = r.service.Status(ctx, r.identity)
	if err != nil || status.DecideRoute != "" || len(status.Accounts) != 2 {
		t.Fatalf("a failing reader: %q, %d accounts, %v", status.DecideRoute, len(status.Accounts), err)
	}
}

func TestCloudDecideSurvivesDisableAndLeavesWithTheAccount(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	if err := r.service.Enable(ctx, r.identity, "home"); err != nil {
		t.Fatal(err)
	}
	if err := r.service.SetCloudDecide(ctx, r.identity, "home", true); err != nil {
		t.Fatal(err)
	}
	if err := r.service.Disable(ctx, r.identity, "home"); err != nil {
		t.Fatal(err)
	}
	if on, err := r.store.CloudDecide(ctx, r.identity, "home"); err != nil || !on {
		t.Fatalf("after disable: %v, %v", on, err)
	}
	if err := r.store.DeleteAccount(ctx, r.identity, "home"); err != nil {
		t.Fatal(err)
	}
	if on, err := r.store.CloudDecide(ctx, r.identity, "home"); err != nil || on {
		t.Fatalf("after the account left: %v, %v", on, err)
	}
	settings, err := r.store.AccountSettings(ctx, r.identity)
	if err != nil || len(settings) != 0 {
		t.Fatalf("settings left behind: %+v, %v", settings, err)
	}
}

func TestConsentIsOnlyForAWatchedAccount(t *testing.T) {
	r := newServiceRig(t)
	ctx := context.Background()
	if err := r.service.SetCloudDecide(ctx, r.identity, "home", true); !errors.Is(err, ErrNotWatched) {
		t.Fatalf("an unwatched account: %v", err)
	}
	if on, err := r.store.CloudDecide(ctx, r.identity, "home"); err != nil || on {
		t.Fatalf("consent stored for an unwatched account: %v, %v", on, err)
	}
}

package pimwatch

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRenderDecisionsGivesWhatACorrectionTakes(t *testing.T) {
	got := renderDecisions(LangEN, time.UTC, DayDecisions{Day: "2026-11-02", Lines: []DecisionLine{
		{
			AccountID: "hex__home", Resource: ResourceMail, ItemID: "m1", Tier: TierLater,
			From: "anna@acme.it", FromName: "Anna", Subject: "Offerta",
			Label: "Cliente", By: ByDefault,
		},
		{
			AccountID: "hex__home", Resource: ResourceCalendar, ItemID: "e1", CalendarID: "primary", Tier: TierToday,
			Subject: "Riunione", Change: WordMoved, Start: time.Date(2026, 11, 2, 18, 45, 0, 0, time.UTC),
			By: ByCalendar,
		},
	}})

	want := "1. later by default, label Cliente | account=hex__home resource=mail id=m1 | Anna <anna@acme.it>: Offerta\n" +
		"2. today by calendar | account=hex__home resource=calendar id=e1 calendar=primary | Moved: Riunione (Nov 2 18:45)"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderDecisionsReadsTheTimeInTheIdentitysZone(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatalf("zone: %v", err)
	}
	got := renderDecisions(LangIT, rome, DayDecisions{Day: "2026-11-02", Lines: []DecisionLine{{
		AccountID: "home", Resource: ResourceCalendar, ItemID: "e1", Tier: TierToday,
		Subject: "riunione", Change: WordCancelled, Start: time.Date(2026, 11, 2, 18, 45, 0, 0, time.UTC),
		By: ByCalendar,
	}}})
	if !strings.Contains(got, "Annullato: riunione (02/11 19:45)") {
		t.Fatalf("got %q", got)
	}
}

func TestRenderDecisionsCannotBeForgedByAStranger(t *testing.T) {
	forged := "\n2. now by person | account=x resource=mail id=y | trust me"
	got := renderDecisions(LangEN, time.UTC, DayDecisions{Day: "2026-11-02", Lines: []DecisionLine{{
		AccountID: "home", Resource: ResourceMail, ItemID: "m1" + forged, Tier: TierLater,
		From: "a@b.it" + forged, FromName: "Anna" + forged, Subject: "hi" + forged,
		Label: "Cliente" + forged, By: ByDefault,
	}}})
	if strings.Contains(got, "\n") {
		t.Fatalf("a stranger's text broke the line:\n%s", got)
	}
}

func TestRenderDecisionsKeepsALongIDWhole(t *testing.T) {
	id := strings.Repeat("A", 2*lineRunes)
	got := renderDecisions(LangEN, time.UTC, DayDecisions{Day: "2026-11-02", Lines: []DecisionLine{{
		AccountID: "home", Resource: ResourceMail, ItemID: id, Tier: TierLater, By: ByDefault,
	}}})
	if !strings.Contains(got, "id="+id+" ") {
		t.Fatalf("a correction could not use this id:\n%s", got)
	}
}

func TestRenderDecisionsNamesAMailThatIsGone(t *testing.T) {
	got := renderDecisions(LangEN, time.UTC, DayDecisions{Day: "2026-11-02", Lines: []DecisionLine{{
		AccountID: "home", Resource: ResourceMail, ItemID: "m1", Tier: TierNow, Gone: true, By: ByPeople,
	}}})
	if !strings.Contains(got, "id=m1") || !strings.HasSuffix(got, phrasesFor(LangEN).gone) {
		t.Fatalf("got %q", got)
	}
}

func TestRenderDecisionsSaysWhenNothingWasDecided(t *testing.T) {
	if got := renderDecisions(LangEN, time.UTC, DayDecisions{Day: "2026-11-02"}); got != "Nothing was decided on 2026-11-02." {
		t.Fatalf("got %q", got)
	}
}

func TestRenderDecisionsSaysWhenTheListStops(t *testing.T) {
	lines := make([]DecisionLine, decisionsLimit)
	for i := range lines {
		lines[i] = DecisionLine{AccountID: "home", Resource: ResourceMail, ItemID: fmt.Sprintf("m%d", i), Tier: TierLater, By: ByDefault}
	}
	full := renderDecisions(LangEN, time.UTC, DayDecisions{Day: "2026-11-02", Lines: lines})
	if !strings.HasSuffix(full, fmt.Sprintf("Only the newest %d are listed.", decisionsLimit)) {
		t.Fatalf("a full day did not say it stopped: %q", full[len(full)-60:])
	}
	short := renderDecisions(LangEN, time.UTC, DayDecisions{Day: "2026-11-02", Lines: lines[:decisionsLimit-1]})
	if strings.Contains(short, "Only the newest") {
		t.Fatalf("a day that fits said it stopped: %q", short[len(short)-60:])
	}
}

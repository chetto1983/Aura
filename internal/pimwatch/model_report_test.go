package pimwatch

import (
	"bytes"
	"strings"
	"testing"
)

func TestModelReportCountsEachModelApart(t *testing.T) {
	const a, b = "liquid/d1.1-20261027", "d1-3b"
	ask := func(row ModelRow) ModelRow { row.ModelAsked = true; return row }
	rows := []ModelRow{
		// a: left standing, 4 options.
		ask(ModelRow{ModelName: a, Tier: TierLater, ModelTier: TierLater, By: ByModel, LabelID: "L1", ModelLabelID: "L1", ModelOptions: 4}),
		// a: rescued from the discard by the person, who gave it another label.
		ask(ModelRow{ModelName: a, Tier: TierLater, ModelTier: TierLater, By: ByPerson, LabelID: "L3", ModelLabelID: "L2", ModelDiscarded: true, ModelOptions: 7}),
		// a: its label was removed, 12 options.
		ask(ModelRow{ModelName: a, Tier: TierToday, ModelTier: TierToday, By: ByModel, ModelOptions: 12}),
		// a: an alert it started, then the person corrected.
		ask(ModelRow{ModelName: a, Tier: TierToday, LadderTier: TierToday, ModelTier: TierNow, By: ByPerson, LabelID: "L2", ModelLabelID: "L1", Notified: true, ModelOptions: 2}),
		// a: an alert it started and nobody corrected.
		ask(ModelRow{ModelName: a, Tier: TierNow, LadderTier: TierToday, ModelTier: TierNow, By: ByModel, LabelID: "L1", ModelLabelID: "L1", Notified: true, ModelOptions: 2}),
		// a: the ladder's own alert, which the model's label only kept.
		ask(ModelRow{ModelName: a, Tier: TierNow, LadderTier: TierNow, ModelTier: TierNow, By: ByModel, LabelID: "L1", ModelLabelID: "L1", Notified: true, ModelOptions: 2}),
		// a: not applied, and the person added the label by hand.
		ask(ModelRow{ModelName: a, Tier: TierToday, By: ByPerson, LabelID: "L1", ModelOptions: 3}),
		// a: a guess under the threshold, then the same label added by hand: the person's, not the model's.
		ask(ModelRow{ModelName: a, Tier: TierToday, By: ByPerson, LabelID: "L1", ModelLabelID: "L1", ModelOptions: 3}),
		// b: one label left standing, one discard kept.
		ask(ModelRow{ModelName: b, Tier: TierLater, ModelTier: TierLater, By: ByModel, LabelID: "L1", ModelLabelID: "L1", ModelDiscarded: true, Discarded: true, ModelOptions: 6}),
		// Not asked: the budget was spent, and the person labelled it by hand.
		{ModelSkipped: true, Tier: TierToday, By: ByPerson, LabelID: "L1"},
		// Rules only.
		{Tier: TierNow, LadderTier: TierNow, By: ByRule, Notified: true},
		{Tier: TierNow, LadderTier: TierNow, By: ByPerson, Notified: true},
		// An alert the person demoted: the row's tier is now `later`, the ladder's answer is not.
		{Tier: TierLater, LadderTier: TierNow, By: ByPerson, Notified: true},
		{Tier: TierLater, LadderTier: TierLater, By: ByPerson},
		{Tier: TierLater, LadderTier: TierLater, By: ByDefault},
	}
	report := BuildModelReport(rows)

	want := map[string]ModelCounts{
		a: {
			Model: a, Asked: 8, Labelled: 6, Standing: 3, Gone: 1, HandLabelled: 2,
			AlertsStarted: 2, AlertsCorrected: 1, Discarded: 1, Rescued: 1,
			ByOptions: map[string][2]int{"2": {3, 2}, "3_5": {1, 1}, "6_10": {1, 0}, "11": {1, 0}},
		},
		b: {
			Model: b, Asked: 1, Labelled: 1, Standing: 1, Discarded: 1,
			ByOptions: map[string][2]int{"6_10": {1, 1}},
		},
		"": {Model: "", Skipped: 1, SkippedHandLabelled: 1},
	}
	if len(report.Models) != len(want) {
		t.Fatalf("models = %+v", report.Models)
	}
	for _, got := range report.Models {
		w, ok := want[got.Model]
		if !ok || got.Asked != w.Asked || got.Labelled != w.Labelled || got.Standing != w.Standing || got.Gone != w.Gone ||
			got.HandLabelled != w.HandLabelled || got.AlertsStarted != w.AlertsStarted || got.AlertsCorrected != w.AlertsCorrected ||
			got.Discarded != w.Discarded || got.Rescued != w.Rescued || got.Skipped != w.Skipped ||
			got.SkippedHandLabelled != w.SkippedHandLabelled || len(got.ByOptions) != len(w.ByOptions) {
			t.Fatalf("%q = %+v, want %+v", got.Model, got, w)
		}
		for bucket, pair := range w.ByOptions {
			if got.ByOptions[bucket] != pair {
				t.Fatalf("%q bucket %s = %v, want %v", got.Model, bucket, got.ByOptions[bucket], pair)
			}
		}
	}
	if r := report.RulesOnly; r.Rows != 5 || r.Alerts != 3 || r.AlertsCorrected != 2 || r.Corrected != 3 {
		t.Fatalf("rules only = %+v", r)
	}

	var out bytes.Buffer
	report.WriteText(&out)
	for _, want := range []string{
		"standing/labelled: 0.500 (3/6, below)",
		"standing/(standing+by hand+skipped by hand): 0.500 (3/6, below)",
		"the same on the mail it was asked about: 0.600 (3/5, met)",
		"standing/labelled: 1.000 (1/1, met)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("report lacks %q:\n%s", want, out.String())
		}
	}
}

func TestModelReportBarsMeetTheirFloorExactly(t *testing.T) {
	const exact, short = "exact", "short"
	var rows []ModelRow
	standing := func(name string, n int) {
		for range n {
			rows = append(rows, ModelRow{ModelName: name, ModelAsked: true, ModelTier: TierLater, By: ByModel, LabelID: "L1", ModelLabelID: "L1", ModelOptions: 4})
		}
	}
	gone := func(name string) {
		rows = append(rows, ModelRow{ModelName: name, ModelAsked: true, ModelTier: TierLater, By: ByModel, ModelOptions: 4})
	}
	standing(exact, 9)
	gone(exact)
	standing(short, 8)
	gone(short)
	var out bytes.Buffer
	BuildModelReport(rows).WriteText(&out)
	for _, want := range []string{"standing/labelled: 0.900 (9/10, met)", "standing/labelled: 0.889 (8/9, below)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("report lacks %q:\n%s", want, out.String())
		}
	}
}

func TestModelReportPrintsTheBarsAndTheBaseline(t *testing.T) {
	var out bytes.Buffer
	BuildModelReport(nil).WriteText(&out)
	for _, want := range []string{"standing/labelled", "0.90", "0.60", "rules only", "mail decided: 0"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("report lacks %q:\n%s", want, out.String())
		}
	}
}

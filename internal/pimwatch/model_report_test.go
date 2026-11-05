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
		// b: one label left standing, one discard kept.
		ask(ModelRow{ModelName: b, Tier: TierLater, ModelTier: TierLater, By: ByModel, LabelID: "L1", ModelLabelID: "L1", ModelDiscarded: true, Discarded: true, ModelOptions: 6}),
		// Not asked: the budget was spent, and the person labelled it by hand.
		{ModelSkipped: true, Tier: TierToday, By: ByPerson, LabelID: "L1"},
		// Rules only.
		{Tier: TierNow, By: ByRule, Notified: true},
		{Tier: TierNow, By: ByPerson, Notified: true},
		{Tier: TierLater, By: ByPerson},
		{Tier: TierLater, By: ByDefault},
	}
	report := BuildModelReport(rows)

	want := map[string]ModelCounts{
		a: {
			Model: a, Asked: 7, Labelled: 6, Standing: 3, Gone: 1, HandLabelled: 1,
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
	if r := report.RulesOnly; r.Rows != 4 || r.Alerts != 2 || r.AlertsCorrected != 1 || r.Corrected != 2 {
		t.Fatalf("rules only = %+v", r)
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

package pimwatch

import (
	"math"
	"strings"
	"testing"
	"time"
)

var (
	reportNow    = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	reportDigest = time.Date(2026, 11, 9, 18, 0, 0, 0, time.UTC)
	reportOld    = reportNow.Add(-72 * time.Hour)
)

func shadowRow(tier Tier, by DecidedBy, ladder Tier, ladderBy DecidedBy, memory, centroid Tier, margin float64) ShadowRow {
	return ShadowRow{
		Tier: tier, By: by, LadderTier: ladder, LadderBy: ladderBy, SenderMemory: memory, Centroid: centroid,
		Margin: margin, DecidedAt: reportOld,
	}
}

func handTable() []ShadowRow {
	notified := func(r ShadowRow) ShadowRow { r.Notified = true; return r }
	fresh := func(r ShadowRow) ShadowRow { r.DecidedAt = reportNow.Add(-time.Hour); return r }
	return []ShadowRow{
		shadowRow(TierNow, ByPerson, TierToday, ByPeople, TierNow, TierNow, 0.05),
		shadowRow(TierLater, ByPerson, TierToday, ByDefault, TierToday, TierLater, 0.02),
		shadowRow(TierNow, ByPerson, TierToday, ByPeople, TierToday, TierToday, 0.01),
		shadowRow(TierToday, ByPeople, TierToday, ByPeople, TierToday, TierToday, 0.03),
		notified(shadowRow(TierNow, ByRule, TierNow, ByRule, TierNow, TierLater, 0.04)),
		shadowRow(TierLater, ByCategory, TierLater, ByCategory, TierLater, TierNow, 0.06),
		fresh(shadowRow(TierToday, ByDefault, TierToday, ByDefault, TierToday, "", 0)),
		shadowRow(TierToday, ByDefault, TierToday, ByDefault, TierLater, "", 0),
		shadowRow(TierToday, ByPerson, TierLater, ByCategory, TierLater, TierToday, 0.07),
		notified(shadowRow(TierNow, ByPeople, TierNow, ByPeople, TierNow, TierNow, 0.08)),
	}
}

func sameArm(t *testing.T, name string, got, want ArmScore) {
	t.Helper()
	if got.Right != want.Right || got.NowFound != want.NowFound || got.NowAll != want.NowAll || math.Abs(got.MacroF1-want.MacroF1) > 1e-9 {
		t.Fatalf("%s = %+v, want %+v", name, got, want)
	}
}

func TestBuildShadowReportOnAHandTable(t *testing.T) {
	r := BuildShadowReport(handTable(), reportDigest, reportNow)
	if r.Rows != 10 || r.Answered != 8 || r.Strong.Count != 4 || r.Weak.Count != 4 || r.All.Count != 8 {
		t.Fatalf("counts = rows %d answered %d strong %d weak %d all %d", r.Rows, r.Answered, r.Strong.Count, r.Weak.Count, r.All.Count)
	}
	sameArm(t, "strong ladder", r.Strong.Ladder, ArmScore{Right: 0, NowFound: 0, NowAll: 2, MacroF1: 0})
	sameArm(t, "strong memory", r.Strong.Memory, ArmScore{Right: 1, NowFound: 1, NowAll: 2, MacroF1: 2.0 / 9})
	sameArm(t, "strong centroid", r.Strong.Centroid, ArmScore{Right: 3, NowFound: 1, NowAll: 2, MacroF1: 7.0 / 9})
	sameArm(t, "weak ladder", r.Weak.Ladder, ArmScore{Right: 4, NowFound: 2, NowAll: 2, MacroF1: 1})
	sameArm(t, "weak memory", r.Weak.Memory, ArmScore{Right: 3, NowFound: 2, NowAll: 2, MacroF1: 5.0 / 9})
	sameArm(t, "weak centroid", r.Weak.Centroid, ArmScore{Right: 2, NowFound: 1, NowAll: 2, MacroF1: 4.0 / 9})
	sameArm(t, "all ladder", r.All.Ladder, ArmScore{Right: 4, NowFound: 2, NowAll: 4, MacroF1: 7.0 / 18})
	sameArm(t, "all memory", r.All.Memory, ArmScore{Right: 4, NowFound: 3, NowAll: 4, MacroF1: 25.0 / 63})
	sameArm(t, "all centroid", r.All.Centroid, ArmScore{Right: 5, NowFound: 2, NowAll: 4, MacroF1: 2.0 / 3})
	want := map[DecidedBy]int{ByPeople: 1, ByDefault: 1, ByRule: 1, ByCategory: 2}
	if len(r.Overrules) != len(want) {
		t.Fatalf("overrules = %v, want %v", r.Overrules, want)
	}
	for rung, n := range want {
		if r.Overrules[rung] != n {
			t.Fatalf("overrules = %v, want %v", r.Overrules, want)
		}
	}
	if r.MarginP10 != 0.01 || r.MarginP50 != 0.04 || r.MarginP90 != 0.08 {
		t.Fatalf("margins = %v %v %v", r.MarginP10, r.MarginP50, r.MarginP90)
	}
}

func TestWeakTruthNeedsTheMailToHaveReachedThePerson(t *testing.T) {
	digest := reportNow.Add(-96 * time.Hour)
	beforeDigest := func(r ShadowRow) ShadowRow { r.DecidedAt = digest.Add(-time.Hour); return r }
	rows := []ShadowRow{
		beforeDigest(shadowRow(TierLater, ByCategory, TierLater, ByCategory, TierLater, TierLater, 0.02)),
		beforeDigest(shadowRow(TierToday, ByDefault, TierToday, ByDefault, TierToday, TierToday, 0.02)),
		shadowRow(TierNow, ByPeople, TierNow, ByPeople, TierNow, TierNow, 0.02),
	}
	if r := BuildShadowReport(rows, digest, reportNow); r.Weak.Count != 1 || r.Weak.Ladder.Right != 1 || r.Weak.Ladder.NowAll != 0 {
		t.Fatalf("weak = %+v, want the today mail the digest covered alone", r.Weak)
	}
	if r := BuildShadowReport(rows, time.Time{}, reportNow); r.Weak.Count != 0 {
		t.Fatalf("weak with no digest sent = %+v", r.Weak)
	}
}

func TestALabelOnlyCorrectionIsWeakTruth(t *testing.T) {
	labelOnly := shadowRow(TierToday, ByPerson, TierToday, ByPeople, TierToday, TierNow, 0.03)
	labelOnly.DecidedAt = reportNow.Add(-time.Hour)
	rows := []ShadowRow{labelOnly, shadowRow(TierNow, ByPerson, TierToday, ByPeople, TierNow, TierNow, 0.05)}
	r := BuildShadowReport(rows, time.Time{}, reportNow)
	if r.Strong.Count != 1 || r.Strong.Ladder.Right != 0 || r.Strong.Centroid.Right != 1 {
		t.Fatalf("strong = %+v, want the tier change alone", r.Strong)
	}
	if r.Weak.Count != 1 || r.Weak.Ladder.Right != 1 || r.Weak.Centroid.Right != 0 {
		t.Fatalf("weak = %+v, want the label-only correction, kept tier as the truth", r.Weak)
	}
}

func TestAnEmptyReportHasNoMargins(t *testing.T) {
	r := BuildShadowReport(nil, time.Time{}, reportNow)
	if r.Rows != 0 || r.MarginP50 != 0 || r.All.Ladder.MacroF1 != 0 {
		t.Fatalf("report = %+v", r)
	}
}

func TestTheReportPrintsNoSenderAndNoId(t *testing.T) {
	var out strings.Builder
	BuildShadowReport(handTable(), reportDigest, reportNow).WriteText(&out)
	text := out.String()
	if strings.Contains(text, "@") {
		t.Fatalf("the report carries an address:\n%s", text)
	}
	for _, want := range []string{"strong", "weak", "all", "ladder", "memory", "centroid", "by construction", "0.778", "category 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the report lacks %q:\n%s", want, text)
		}
	}
}

// The person saw the model's tier where its label applied, so that is the tier a correction changed
// or kept.
func TestACorrectionOfAModelTierIsStrongTruth(t *testing.T) {
	modelled := func(r ShadowRow) ShadowRow { r.ModelTier = TierLater; return r }
	changed := modelled(shadowRow(TierToday, ByPerson, TierToday, ByDefault, TierToday, TierToday, 0.03))
	kept := modelled(shadowRow(TierLater, ByPerson, TierToday, ByDefault, TierToday, TierToday, 0.03))
	r := BuildShadowReport([]ShadowRow{changed, kept}, time.Time{}, reportNow)
	if r.Strong.Count != 1 || r.Strong.Ladder.Right != 1 {
		t.Fatalf("strong = %+v, want the correction away from the model's later", r.Strong)
	}
	if r.Weak.Count != 1 || r.Weak.Ladder.Right != 0 {
		t.Fatalf("weak = %+v, want the label-only correction that kept the model's later", r.Weak)
	}
	var out strings.Builder
	r.WriteText(&out)
	if !strings.Contains(out.String(), "the tier the person saw") {
		t.Fatalf("the weak truth's sentence does not say whose tier it is:\n%s", out.String())
	}
}

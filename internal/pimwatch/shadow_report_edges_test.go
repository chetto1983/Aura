package pimwatch

import (
	"math"
	"strings"
	"testing"
	"time"
)

// A mail counts as settled only once it has stood weakTruthAge, and then only if it reached the person.
func TestWeakTruthStartsAtFortyEightHours(t *testing.T) {
	aged := func(age time.Duration, tier Tier) ShadowRow {
		r := shadowRow(tier, ByRule, tier, ByRule, tier, tier, 0.02)
		r.DecidedAt, r.Notified = reportNow.Add(-age), true
		return r
	}
	for _, c := range []struct {
		name string
		age  time.Duration
		want int
	}{
		{"just past the cutoff", 48*time.Hour + time.Minute, 1},
		{"just short of it", 47*time.Hour + 30*time.Minute, 0},
	} {
		if got := BuildShadowReport([]ShadowRow{aged(c.age, TierLater)}, time.Time{}, reportNow).Weak.Count; got != c.want {
			t.Errorf("%s: weak count %d, want %d", c.name, got, c.want)
		}
	}
}

func TestAnAlertCountsOnlyOnceTheMailIsOldEnough(t *testing.T) {
	notified := shadowRow(TierLater, ByRule, TierLater, ByRule, TierLater, TierLater, 0.02)
	notified.Notified = true
	fresh := notified
	fresh.DecidedAt = reportNow.Add(-time.Hour)
	if r := BuildShadowReport([]ShadowRow{fresh}, time.Time{}, reportNow); r.Weak.Count != 0 {
		t.Fatalf("a fresh alerted mail is weak truth: %+v", r.Weak)
	}
	// later is never in a digest, so only the alert can make it count.
	if r := BuildShadowReport([]ShadowRow{notified}, time.Time{}, reportNow); r.Weak.Count != 1 {
		t.Fatalf("an old alerted later mail is not weak truth: %+v", r.Weak)
	}
}

func TestADigestHoldsTheNowMailToo(t *testing.T) {
	digest := reportNow.Add(-60 * time.Hour)
	row := shadowRow(TierNow, ByPeople, TierNow, ByPeople, TierNow, TierNow, 0.02)
	row.DecidedAt = digest.Add(-time.Hour)
	if r := BuildShadowReport([]ShadowRow{row}, digest, reportNow); r.Weak.Count != 1 || r.Weak.Ladder.NowAll != 1 {
		t.Fatalf("weak = %+v, want the now mail the digest held", r.Weak)
	}
}

// A tier nobody had or answered is left out of the macro F1; the others still count.
func TestMacroF1SkipsOnlyTheTiersNobodyHad(t *testing.T) {
	right := func(tier Tier) settledMail {
		return settledMail{truth: tier, ladder: tier, memory: tier, centroid: tier}
	}
	if got := scoreTruth([]settledMail{right(TierLater)}).Ladder.MacroF1; got != 1 {
		t.Fatalf("one tier all right: macro F1 %v, want 1", got)
	}
	if got := scoreTruth([]settledMail{right(TierToday), right(TierLater)}).Ladder.MacroF1; got != 1 {
		t.Fatalf("no now mail: macro F1 %v, want 1 over today and later", got)
	}
}

func TestTheMarginsAreThePercentilesOfTheAnsweredMail(t *testing.T) {
	rows := make([]ShadowRow, 100)
	for i := range rows {
		rows[i] = shadowRow(TierLater, ByDefault, TierLater, ByDefault, TierLater, TierLater, float64(100-i)/100)
	}
	r := BuildShadowReport(rows, time.Time{}, reportNow)
	for _, c := range []struct {
		name      string
		got, want float64
	}{{"p10", r.MarginP10, 0.10}, {"p50", r.MarginP50, 0.50}, {"p90", r.MarginP90, 0.90}} {
		if math.Abs(c.got-c.want) > 1e-9 {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestNearestRankIsTheFirstValueThatCoversP(t *testing.T) {
	values := make([]float64, 101)
	for i := range values {
		values[i] = float64(i + 1)
	}
	for _, c := range []struct{ p, want float64 }{{0, 1}, {10, 11}, {50, 51}, {100, 101}} {
		if got := nearestRank(values, c.p); got != c.want {
			t.Errorf("nearestRank(1..101, %v) = %v, want %v", c.p, got, c.want)
		}
	}
}

// The report is read by a person deciding a promotion, so its layout is part of what it says.
func TestTheReportTextIsTheTableAndTheThreeTruths(t *testing.T) {
	score := func(right, found, all int, f1 float64) ArmScore {
		return ArmScore{Right: right, NowFound: found, NowAll: all, MacroF1: f1}
	}
	truth := func(n int) Truth {
		return Truth{Count: n, Ladder: score(1, 0, 2, 0.25), Memory: score(2, 1, 2, 0.5), Centroid: score(3, 2, 2, 0.75)}
	}
	r := ShadowReport{
		Rows: 12, Answered: 9, Strong: truth(4), Weak: truth(5), All: truth(9),
		Overrules: map[DecidedBy]int{ByRule: 1}, MarginP10: 0.012, MarginP50: 0.034, MarginP90: 0.056,
	}
	var out strings.Builder
	r.WriteText(&out)
	want := `mail decided: 12, answered by the centroid: 9

TRUTH   ROWS  ARM       RIGHT  MACRO F1  NOW FOUND
strong  4     ladder    1      0.250     0/2
              memory    2      0.500     1/2
              centroid  3      0.750     2/2
weak    5     ladder    1      0.250     0/2
              memory    2      0.500     1/2
              centroid  3      0.750     2/2
all     9     ladder    1      0.250     0/2
              memory    2      0.500     1/2
              centroid  3      0.750     2/2

the centroid would overrule the ladder, by rung: rule 1
margin p10/p50/p90 over the answered mail: 0.012 0.034 0.056

strong: mail whose tier the person changed. The tier changed was the one they saw, so the ladder scores zero here by construction wherever the decision model's label had not moved it; the memory is the arm to beat.
weak: mail whose label alone the person changed, and uncorrected mail older than 48 h that an alert or a digest brought to the person. Its tier is the tier the person saw: the decision model's where its label applied, else the ladder's, which then scores 100% by construction.
all: both together, the fair comparison.
`
	if got := out.String(); got != want {
		t.Fatalf("report text:\n%s\nwant:\n%s", got, want)
	}

	r.Overrules = map[DecidedBy]int{}
	out.Reset()
	r.WriteText(&out)
	if !strings.Contains(out.String(), "by rung: none\n") {
		t.Fatalf("a report with no overrule does not say none:\n%s", out.String())
	}
}

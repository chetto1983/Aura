package pimwatch

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
)

// weakTruthAge is how long an uncorrected mail must have stood before its tier counts as settled.
const weakTruthAge = 48 * time.Hour

// ArmScore is how one arm did against one truth: answers right, `now` found of the `now` there
// were, and the macro F1 over the tiers.
type ArmScore struct {
	Right, NowFound, NowAll int
	MacroF1                 float64
}

// Truth scores the three arms against one kind of settled tier.
type Truth struct {
	Count                    int
	Ladder, Memory, Centroid ArmScore
}

// ShadowReport is the promotion gate's counts, read from the verdict rows alone
// (docs/superpowers/specs/2026-11-03-urgency-centroid-design.md, "Baseline"). It holds no sender,
// no subject and no id.
type ShadowReport struct {
	Rows, Answered int
	// Strong is the mail whose tier the person changed; Weak the mail whose tier they kept while
	// changing its label, and the uncorrected mail older than weakTruthAge that reached them through
	// an alert or a digest; All both.
	Strong, Weak, All Truth
	// Overrules counts, by the ladder's rung, the mail the centroid would have put elsewhere.
	Overrules                       map[DecidedBy]int
	MarginP10, MarginP50, MarginP90 float64
}

// settledMail is one settled mail: its true tier and what each arm answered. An empty answer is wrong.
type settledMail struct{ truth, ladder, memory, centroid Tier }

// BuildShadowReport scores the rows. lastDigest is when the last digest window closed, zero if
// none was ever sent.
func BuildShadowReport(rows []ShadowRow, lastDigest, now time.Time) ShadowReport {
	report := ShadowReport{Rows: len(rows), Overrules: map[DecidedBy]int{}}
	var strong, weak []settledMail
	var margins []float64
	for _, row := range rows {
		if row.Centroid != "" {
			report.Answered++
			margins = append(margins, row.Margin)
			if row.Centroid != row.LadderTier {
				report.Overrules[row.LadderBy]++
			}
		}
		settled := settledMail{truth: row.Tier, ladder: row.LadderTier, memory: row.SenderMemory, centroid: row.Centroid}
		// A correction that changed only the label is decided_by person too, but its tier is the one
		// the person saw, the model's where its label applied: they saw the mail and kept it.
		switch {
		case row.By == ByPerson && row.Tier != cmp.Or(row.ModelTier, row.LadderTier):
			strong = append(strong, settled)
		case row.By == ByPerson || reachedThePerson(row, lastDigest, now):
			weak = append(weak, settled)
		}
	}
	report.Strong, report.Weak, report.All = scoreTruth(strong), scoreTruth(weak), scoreTruth(slices.Concat(strong, weak))
	slices.Sort(margins)
	report.MarginP10, report.MarginP50, report.MarginP90 = nearestRank(margins, 10), nearestRank(margins, 50), nearestRank(margins, 90)
	return report
}

// reachedThePerson is an uncorrected mail old enough to count as settled that a completed alert
// told, or that a sent digest's window held. The digest lists `now` and `today` only.
func reachedThePerson(row ShadowRow, lastDigest, now time.Time) bool {
	if !row.DecidedAt.Before(now.Add(-weakTruthAge)) {
		return false
	}
	if row.Notified {
		return true
	}
	inDigest := row.Tier == TierNow || row.Tier == TierToday
	return inDigest && !lastDigest.IsZero() && !row.DecidedAt.After(lastDigest)
}

func scoreTruth(rows []settledMail) Truth {
	return Truth{
		Count:    len(rows),
		Ladder:   scoreArm(rows, func(j settledMail) Tier { return j.ladder }),
		Memory:   scoreArm(rows, func(j settledMail) Tier { return j.memory }),
		Centroid: scoreArm(rows, func(j settledMail) Tier { return j.centroid }),
	}
}

// scoreArm counts one arm. A tier absent from both the truth and the arm's answers is left out of
// the macro F1, so a quiet week without `now` does not score zero for everyone.
func scoreArm(rows []settledMail, answer func(settledMail) Tier) ArmScore {
	var score ArmScore
	tp, fp, fn := map[Tier]int{}, map[Tier]int{}, map[Tier]int{}
	for _, j := range rows {
		got := answer(j)
		if got == j.truth {
			score.Right++
			tp[got]++
		} else {
			fn[j.truth]++
			if got != "" {
				fp[got]++
			}
		}
		if j.truth == TierNow {
			score.NowAll++
			if got == TierNow {
				score.NowFound++
			}
		}
	}
	var sum float64
	var tiers int
	for _, tier := range []Tier{TierNow, TierToday, TierLater} {
		if tp[tier]+fp[tier]+fn[tier] == 0 {
			continue
		}
		sum += float64(2*tp[tier]) / float64(2*tp[tier]+fp[tier]+fn[tier])
		tiers++
	}
	if tiers > 0 {
		score.MacroF1 = sum / float64(tiers)
	}
	return score
}

// nearestRank is the p-th percentile of sorted values by nearest rank, 0 when there are none.
func nearestRank(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	return sorted[max(rank, 1)-1]
}

// WriteText prints the counts, and what each truth favours by construction.
func (r ShadowReport) WriteText(w io.Writer) {
	_, _ = fmt.Fprintf(w, "mail decided: %d, answered by the centroid: %d\n\n", r.Rows, r.Answered)
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "TRUTH\tROWS\tARM\tRIGHT\tMACRO F1\tNOW FOUND")
	for _, truth := range []struct {
		name  string
		truth Truth
	}{{"strong", r.Strong}, {"weak", r.Weak}, {"all", r.All}} {
		for i, arm := range []struct {
			name  string
			score ArmScore
		}{{"ladder", truth.truth.Ladder}, {"memory", truth.truth.Memory}, {"centroid", truth.truth.Centroid}} {
			name, rows := "", ""
			if i == 0 {
				name, rows = truth.name, fmt.Sprint(truth.truth.Count)
			}
			_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%d\t%.3f\t%d/%d\n", name, rows, arm.name, arm.score.Right, arm.score.MacroF1, arm.score.NowFound, arm.score.NowAll)
		}
	}
	_ = table.Flush()
	var overrules []string
	for _, rung := range []DecidedBy{ByRule, ByAuthFailed, ByPeople, ByCategory, ByDefault} {
		if n := r.Overrules[rung]; n > 0 {
			overrules = append(overrules, fmt.Sprintf("%s %d", rung, n))
		}
	}
	rungs := "none"
	if len(overrules) > 0 {
		rungs = strings.Join(overrules, ", ")
	}
	_, _ = fmt.Fprintf(w, "\nthe centroid would overrule the ladder, by rung: %s\n", rungs)
	_, _ = fmt.Fprintf(w, "margin p10/p50/p90 over the answered mail: %.3f %.3f %.3f\n\n", r.MarginP10, r.MarginP50, r.MarginP90)
	_, _ = fmt.Fprintln(w, "strong: mail whose tier the person changed. The tier changed was the one they saw, so the ladder scores zero here by construction wherever the decision model's label had not moved it; the memory is the arm to beat.")
	_, _ = fmt.Fprintln(w, "weak: mail whose label alone the person changed, and uncorrected mail older than 48 h that an alert or a digest brought to the person. Its tier is the tier the person saw: the decision model's where its label applied, else the ladder's, which then scores 100% by construction.")
	_, _ = fmt.Fprintln(w, "all: both together, the fair comparison.")
}

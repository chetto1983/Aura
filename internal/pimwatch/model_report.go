package pimwatch

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
)

const (
	// standingBar and handBar are the spec's two bars (docs/superpowers/specs/2026-11-03-decision-model-labels-design.md,
	// "Baseline"), written before G4 measured anything.
	standingBar = 0.90
	handBar     = 0.60
)

// optionBuckets are the option counts llama.cpp scales probabilities by, in print order.
var optionBuckets = []string{"2", "3_5", "6_10", "11"}

// ModelCounts is what one decision model did. Model is "" for the mail it was not asked about.
type ModelCounts struct {
	Model                                         string
	Asked, Labelled, Standing, Gone, HandLabelled int
	AlertsStarted, AlertsCorrected                int
	Discarded, Rescued                            int
	Skipped, SkippedHandLabelled                  int
	// ByOptions is, by the number of options offered, the labels applied and those left standing.
	ByOptions map[string][2]int
}

// ModelReport is the decision model's counts, read from the verdict rows alone, against the same
// window decided by the rules alone. It holds no sender, no subject and no id.
type ModelReport struct {
	Models    []ModelCounts
	RulesOnly struct{ Alerts, AlertsCorrected, Corrected, Rows int }
}

func optionBucket(options int) string {
	switch {
	case options <= 2:
		return "2"
	case options <= 5:
		return "3_5"
	case options <= 10:
		return "6_10"
	}
	return "11"
}

// BuildModelReport counts each model apart: the hosted model's probabilities move between identical
// calls, so two models' numbers never add up.
func BuildModelReport(rows []ModelRow) ModelReport {
	var report ModelReport
	byModel := map[string]*ModelCounts{}
	for _, row := range rows {
		if !row.ModelAsked && !row.ModelSkipped {
			report.countRulesOnly(row)
			continue
		}
		counts := byModel[row.ModelName]
		if counts == nil {
			counts = &ModelCounts{Model: row.ModelName, ByOptions: map[string][2]int{}}
			byModel[row.ModelName] = counts
		}
		counts.count(row)
	}
	for _, counts := range byModel {
		report.Models = append(report.Models, *counts)
	}
	slices.SortFunc(report.Models, func(a, b ModelCounts) int { return strings.Compare(a.Model, b.Model) })
	return report
}

func (r *ModelReport) countRulesOnly(row ModelRow) {
	r.RulesOnly.Rows++
	if row.Tier == TierNow && row.Notified {
		r.RulesOnly.Alerts++
		if row.By == ByPerson {
			r.RulesOnly.AlertsCorrected++
		}
	}
	if row.By == ByPerson {
		r.RulesOnly.Corrected++
	}
}

func (c *ModelCounts) count(row ModelRow) {
	handLabelled := row.ModelTier == "" && row.By == ByPerson && row.LabelID != ""
	if row.ModelSkipped {
		c.Skipped++
		if handLabelled {
			c.SkippedHandLabelled++
		}
		return
	}
	c.Asked++
	standing := row.ModelLabelID != "" && row.LabelID == row.ModelLabelID
	if standing {
		c.Standing++
	}
	if handLabelled {
		c.HandLabelled++
	}
	if row.ModelDiscarded {
		c.Discarded++
		if !row.Discarded {
			c.Rescued++
		}
	}
	if row.ModelTier == "" {
		return
	}
	c.Labelled++
	if row.ModelLabelID == "" {
		c.Gone++
	}
	if row.ModelTier == TierNow && row.LadderTier != TierNow && row.Notified {
		c.AlertsStarted++
		if row.By == ByPerson {
			c.AlertsCorrected++
		}
	}
	bucket := optionBucket(row.ModelOptions)
	pair := c.ByOptions[bucket]
	pair[0]++
	if standing {
		pair[1]++
	}
	c.ByOptions[bucket] = pair
}

// bar prints part over whole against its bar: "n/a" when there is no whole.
func bar(part, whole int, floor float64) string {
	if whole == 0 {
		return "n/a"
	}
	got := float64(part) / float64(whole)
	verdict := "below"
	if got >= floor {
		verdict = "met"
	}
	return fmt.Sprintf("%.3f (%d/%d, %s)", got, part, whole, verdict)
}

// WriteText prints each model's counts against the spec's two bars, then the rules-only window.
func (r ModelReport) WriteText(w io.Writer) {
	asked := 0
	for _, c := range r.Models {
		asked += c.Asked + c.Skipped
	}
	_, _ = fmt.Fprintf(w, "mail decided: %d, of it decided by the rules alone: %d\n\n", asked+r.RulesOnly.Rows, r.RulesOnly.Rows)
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "MODEL\tASKED\tLABELLED\tSTANDING\tGONE\tBY HAND\tSKIPPED\tSKIPPED BY HAND\tALERTS STARTED\tCORRECTED\tDISCARDED\tRESCUED")
	for _, c := range r.Models {
		name := c.Model
		if name == "" {
			name = "(not asked)"
		}
		_, _ = fmt.Fprintf(table, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n", name, c.Asked, c.Labelled, c.Standing,
			c.Gone, c.HandLabelled, c.Skipped, c.SkippedHandLabelled, c.AlertsStarted, c.AlertsCorrected, c.Discarded, c.Rescued)
	}
	_ = table.Flush()

	_, _ = fmt.Fprintf(w, "\nthe bars: standing/labelled >= %.2f, and standing/(standing+by hand+skipped by hand) >= %.2f\n", standingBar, handBar)
	// A mail the model was not asked about names no model, so its hand labels count against each.
	var skippedByHand int
	for _, c := range r.Models {
		skippedByHand += c.SkippedHandLabelled
	}
	for _, c := range r.Models {
		if c.Model == "" {
			continue
		}
		byHand := c.Standing + c.HandLabelled
		_, _ = fmt.Fprintf(w, "\n%s\n", c.Model)
		_, _ = fmt.Fprintf(w, "  standing/labelled: %s\n", bar(c.Standing, c.Labelled, standingBar))
		_, _ = fmt.Fprintf(w, "  standing/(standing+by hand+skipped by hand): %s\n", bar(c.Standing, byHand+skippedByHand, handBar))
		_, _ = fmt.Fprintf(w, "  the same on the mail it was asked about: %s\n", bar(c.Standing, byHand, handBar))
		for _, bucket := range optionBuckets {
			if pair, ok := c.ByOptions[bucket]; ok {
				_, _ = fmt.Fprintf(w, "  options %s: %d labelled, %d standing\n", bucket, pair[0], pair[1])
			}
		}
	}

	_, _ = fmt.Fprintf(w, "\nrules only, the mail the model was neither asked about nor skipped: %d mails, %d alerts of which %d corrected, %d tiers or labels corrected\n",
		r.RulesOnly.Rows, r.RulesOnly.Alerts, r.RulesOnly.AlertsCorrected, r.RulesOnly.Corrected)
	_, _ = fmt.Fprintln(w, "baseline: no label, which is «Da smistare». Standing counts a label equal to the model's even where it was not applied, so a person's agreement with a guess under the threshold counts in standing and in by hand.")
}

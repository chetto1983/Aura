package agent

import (
	"fmt"
	"testing"
)

// evalGateRow is what the run's gate reads from one reading.
type evalGateRow struct {
	trial          int
	split          string
	turnID         string
	recallMiss     string
	recallSource   string
	baselineSource string
}

// evalGateFailures lists why a run proves nothing about memory: a recall that errored, a
// decision that fell back because a sidecar was down, or a final split in which memory never
// answered. The hard→none criterion is checked separately by memoryHardToNone.
func evalGateFailures(rows []evalGateRow, finalReplayed bool) []string {
	var failures []string
	describe := func(reason string, hits []evalGateRow) {
		if len(hits) == 0 {
			return
		}
		ids := make([]string, 0, 3)
		for _, row := range hits[:min(len(hits), 3)] {
			ids = append(ids, fmt.Sprintf("trial %d %s", row.trial, row.turnID))
		}
		failures = append(failures, fmt.Sprintf("%s: %d reading(s), e.g. %v", reason, len(hits), ids))
	}
	var recallErrors, fallbacks []evalGateRow
	memory := 0
	for _, row := range rows {
		if row.recallMiss == "recall_error" {
			recallErrors = append(recallErrors, row)
		}
		if row.recallSource == EffortSourceFallback || row.baselineSource == EffortSourceFallback {
			fallbacks = append(fallbacks, row)
		}
		if row.split == "final" && row.recallSource == EffortSourceMemory {
			memory++
		}
	}
	describe("recall errored", recallErrors)
	describe("a decision fell back", fallbacks)
	if finalReplayed && memory == 0 {
		failures = append(failures, "the final split produced no memory decision in the recall arm")
	}
	return failures
}

func TestEvalGateFailures(t *testing.T) {
	healthy := []evalGateRow{
		{trial: 1, split: "final", turnID: "a", recallSource: EffortSourceMemory, baselineSource: EffortSourceTeacher},
		{trial: 1, split: "final", turnID: "b", recallSource: EffortSourceSeeds, baselineSource: EffortSourceSeeds, recallMiss: "no_memory"},
	}
	with := func(edit func(*evalGateRow)) []evalGateRow {
		rows := append([]evalGateRow(nil), healthy...)
		edit(&rows[1])
		return rows
	}
	cases := []struct {
		name  string
		rows  []evalGateRow
		final bool
		want  int
	}{
		{"healthy", healthy, true, 0},
		{"recall error", with(func(r *evalGateRow) { r.recallMiss = "recall_error" }), true, 1},
		{"recall arm fell back", with(func(r *evalGateRow) { r.recallSource = EffortSourceFallback }), true, 1},
		{"baseline fell back", with(func(r *evalGateRow) { r.baselineSource = EffortSourceFallback }), true, 1},
		{"no memory in final", healthy[1:], true, 1},
		{"no memory, final not replayed", healthy[1:], false, 0},
		{"memory only in calibration", []evalGateRow{{split: "calibration", recallSource: EffortSourceMemory}}, true, 1},
	}
	for _, tc := range cases {
		if got := evalGateFailures(tc.rows, tc.final); len(got) != tc.want {
			t.Errorf("%s: %d failure(s) %v, want %d", tc.name, len(got), got, tc.want)
		}
	}
}

package main

import (
	"testing"
)

func TestHashIsStableAndDistinct(t *testing.T) {
	t.Parallel()
	if hash("a") != hash("a") {
		t.Fatal("same input produced different hashes")
	}
	if hash("a") == hash("b") {
		t.Fatal("distinct inputs produced the same hash")
	}
	if got := len(hash("a")); got != 64 {
		t.Fatalf("hash length = %d, want 64", got)
	}
}

func TestDecisionRowsCarryThreeRankedAlternatives(t *testing.T) {
	root := findRepoRoot(t)
	rows, err := loadDecisionRows(root, "test-run", "test-policy")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 132 {
		t.Fatalf("rows = %d, want 132", len(rows))
	}
	for _, row := range rows {
		if len(row.Considered) != 3 {
			t.Fatalf("%s considered = %d, want 3", row.DecisionID, len(row.Considered))
		}
		if row.Considered[0].Score < row.Considered[1].Score {
			t.Fatalf("%s alternatives are not reward-ranked", row.DecisionID)
		}
		if row.Propensity <= 0 || row.Propensity > 1 {
			t.Fatalf("%s propensity = %f", row.DecisionID, row.Propensity)
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir := "."
	for i := 0; i < 8; i++ {
		if rows, err := loadDecisionRows(dir, "probe", "probe"); err == nil && len(rows) > 0 {
			return dir
		}
		dir += "/.."
	}
	t.Fatal("repository root not found")
	return ""
}

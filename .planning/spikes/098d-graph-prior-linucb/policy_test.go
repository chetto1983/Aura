package main

import (
	"math/rand"
	"testing"
)

func TestUpdateInverseMatchesKnownOneDimensionalCase(t *testing.T) {
	t.Parallel()
	inv := [][]float64{{1}}
	updateInverse(inv, []float64{2})
	if diff := inv[0][0] - 0.2; diff < -1e-12 || diff > 1e-12 {
		t.Fatalf("inverse = %.12f, want 0.2", inv[0][0])
	}
}

func TestEpsilonDecisionReportsLoggedPropensity(t *testing.T) {
	t.Parallel()
	actions := []string{"a", "b", "c"}
	got := epsilonDecision(actions, map[string]float64{"a": 1, "b": 0, "c": 0}, 0.06, rand.New(rand.NewSource(1)))
	if got.Action != "a" {
		t.Fatalf("action = %q, want a", got.Action)
	}
	want := 0.96
	if diff := got.Propensity - want; diff < -1e-12 || diff > 1e-12 {
		t.Fatalf("propensity = %.12f, want %.12f", got.Propensity, want)
	}
}

func TestHybridDownWeightsMisspecifiedGraphPrediction(t *testing.T) {
	t.Parallel()
	p := newHybridPolicy(2)
	action := "tool/qwen_top3"
	p.count[action] = 10
	p.graphMSE[action] = 0.9
	p.linMSE[action] = 0.1
	if got := p.weight(action); got > 0.2 {
		t.Fatalf("graph weight = %.3f, want <= 0.2", got)
	}
}

func TestRewardModelHasTwoSamplesPerCell(t *testing.T) {
	model, err := loadRewardModel(findRepoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range model.Scenarios {
		for _, action := range sc.Actions {
			if got := len(sc.Samples[action]); got != 2 {
				t.Fatalf("%s/%s samples = %d, want 2", sc.ID, action, got)
			}
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir := "."
	for i := 0; i < 8; i++ {
		if _, err := loadRewardModel(dir); err == nil {
			return dir
		}
		dir += "/.."
	}
	t.Fatal("repository root not found")
	return ""
}

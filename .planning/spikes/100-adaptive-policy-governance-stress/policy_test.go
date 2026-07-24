package main

import (
	"math/rand"
	"testing"
)

func TestGovernedPolicyFallsBackWhenGraphUnavailable(t *testing.T) {
	t.Parallel()
	sc := scenario{
		domain: "tool", prompt: "weather", actions: []string{"qwen_full", "qwen_top3", "semantic_top1"},
		embedding: []float64{1},
	}
	p := newGovernedKNN()
	got := p.choose(sc, features(sc), false, rand.New(rand.NewSource(1)))
	if !got.fallback || got.action != "semantic_top1" {
		t.Fatalf("choice = %+v, want static fallback", got)
	}
}

func TestGovernedPolicyUsesCachedStateWhenGraphBecomesUnavailable(t *testing.T) {
	t.Parallel()
	sc := scenario{
		domain: "tool", prompt: "weather", actions: []string{"qwen_full", "qwen_top3", "semantic_top1"},
		embedding: []float64{1},
	}
	p := newGovernedKNN()
	x := features(sc)
	p.update("qwen_top3", x, 0.9)
	got := p.choose(sc, x, false, rand.New(rand.NewSource(1)))
	if got.fallback || got.action != "qwen_top3" {
		t.Fatalf("choice = %+v, want cached qwen_top3", got)
	}
}

func TestGovernedPolicyBoundsHistoryAndReward(t *testing.T) {
	t.Parallel()
	p := newGovernedKNN()
	for i := 0; i < 300; i++ {
		p.update("a", []float64{1}, 10)
	}
	if got := len(p.history["a"]); got != 256 {
		t.Fatalf("history = %d, want 256", got)
	}
	for _, item := range p.history["a"] {
		if item.reward != 1 {
			t.Fatalf("unclipped reward = %f", item.reward)
		}
	}
}

func TestRobustMedianIgnoresSingleCorruptedNeighbor(t *testing.T) {
	t.Parallel()
	history := []observation{
		{x: []float64{1}, reward: 0.9},
		{x: []float64{1}, reward: 0.9},
		{x: []float64{1}, reward: -0.25},
	}
	got, ok := neighborMedian(history, []float64{1}, 3)
	if !ok || got != 0.9 {
		t.Fatalf("median = %f, %t; want 0.9, true", got, ok)
	}
}

func TestGovernedPolicyQuarantinesAbruptlyDegradedAction(t *testing.T) {
	t.Parallel()
	p := newGovernedKNN()
	for i := 0; i < 16; i++ {
		p.update("winner", []float64{1}, 0.9)
	}
	for i := 0; i < 8; i++ {
		p.update("winner", []float64{1}, -0.15)
	}
	if p.quarantineUntil["winner"] <= p.step {
		t.Fatalf("quarantine until = %d, step = %d", p.quarantineUntil["winner"], p.step)
	}
	if p.rollbacks() != 1 {
		t.Fatalf("rollbacks = %d, want 1", p.rollbacks())
	}
}

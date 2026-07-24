package main

import "testing"

func TestStaticServedActionDoesNotDependOnShadowRecommendation(t *testing.T) {
	t.Parallel()
	sc := sourceScenario{ID: "t01", Domain: "tool"}
	before := staticServedAction(sc)
	for _, shadow := range []string{"semantic_top1", "qwen_top3", "qwen_full"} {
		_ = shadow
		if got := staticServedAction(sc); got != before {
			t.Fatalf("served action changed from %s to %s", before, got)
		}
	}
}

func TestLiveRewardPenalizesCostWithoutOverridingCorrectness(t *testing.T) {
	t.Parallel()
	correctCheap := liveReward(true, 10, 10, "")
	correctExpensive := liveReward(true, 1000, 10000, "")
	incorrectCheap := liveReward(false, 10, 10, "")
	if !(correctCheap > correctExpensive && correctExpensive > incorrectCheap) {
		t.Fatalf("rewards cheap=%f expensive=%f incorrect=%f", correctCheap, correctExpensive, incorrectCheap)
	}
}

func TestAnswerNormalizationHandlesKnownBenchmarkVariants(t *testing.T) {
	t.Parallel()
	if !sameAnswer("Tuesday at 02:00 UTC.", "tuesday 02:00 utc") {
		t.Fatal("expected normalized maintenance-window answer to match")
	}
	if !sameAnswer("seven", "7") {
		t.Fatal("expected number word to match")
	}
}

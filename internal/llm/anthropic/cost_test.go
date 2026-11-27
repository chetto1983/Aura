package anthropic

import (
	"math"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

var catalogue = map[string]llm.Price{
	"claude-sonnet-5-5": {InputPer1M: 2, OutputPer1M: 10, CacheReadPer1M: 0.10, CacheWritePer1M: 2.5},
	"claude-haiku-5-5": {InputPer1M: 1, OutputPer1M: 5, CacheReadPer1M: 0.1, CacheWritePer1M: 1.25,
		LongPrompt: &llm.LongPromptPrice{MinPromptTokens: 100_000, InputPer1M: 2, OutputPer1M: 10, CacheReadPer1M: 0.2, CacheWritePer1M: 2.5}},
	"no-write-rate": {InputPer1M: 3, OutputPer1M: 15, CacheReadPer1M: 0.3},
	"no-read-rate":  {InputPer1M: 3, OutputPer1M: 15, CacheWritePer1M: 3.75},
}

func near(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-12 {
		t.Fatalf("cost %v, want %v", got, want)
	}
}

func TestCostIsSetWheneverAPriceExists(t *testing.T) {
	near(t, callCost(catalogue, "claude-sonnet-5-5", tokens{input: 1000, read: 16002, output: 50}), (1000*2+16002*0.10+50*10)/1e6)
	near(t, callCost(catalogue, "claude-sonnet-5-5", tokens{input: 10, write: 400, output: 1}), (10*2+400*2.5+1*10)/1e6)
	near(t, callCost(catalogue, "claude-haiku-5-5", tokens{input: 90_000, read: 9_000, write: 1_000, output: 10}), (90_000*2+9_000*0.2+1_000*2.5+10*10)/1e6)
	near(t, callCost(catalogue, "claude-haiku-5-5", tokens{input: 90_000, read: 9_999, output: 10}), (90_000*1+9_999*0.1+10*5)/1e6)
	near(t, callCost(catalogue, "no-write-rate", tokens{input: 10, read: 20, output: 1}), (10*3+20*0.3+15)/1e6)
	near(t, callCost(catalogue, "no-read-rate", tokens{input: 10, read: 20, output: 1}), (10*3+20*3+15)/1e6)
	if got := callCost(catalogue, "no-write-rate", tokens{input: 10, write: 1, output: 1}); got != nil {
		t.Fatalf("a cache write with no write rate was priced: %v", *got)
	}
	if got := callCost(catalogue, "claude-unknown", tokens{input: 10, output: 1}); got != nil {
		t.Fatalf("an unpriced model was priced: %v", *got)
	}
	if got := callCost(nil, "claude-sonnet-5-5", tokens{input: 10}); got != nil {
		t.Fatalf("an empty catalogue priced a call: %v", *got)
	}
}

func TestUsageChunkCarriesTheCallCost(t *testing.T) {
	cfg := testConfig()
	cfg.Prices = catalogue
	events := append([]sseEvent{messageStart(1000, 16002, 0, "[]")}, textBlock(0, "x")...)
	chunks := streamOf(t, cfg, append(events, messageDelta("end_turn", 50), messageStop()))
	near(t, chunks[len(chunks)-1].Usage.Cost, (1000*2+16002*0.10+50*10)/1e6)
}

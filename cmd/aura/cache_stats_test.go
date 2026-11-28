package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/cachemetrics"
)

// A turn with no known cost reads n/a in its row, and the summary says its total is the
// priced turns' only, rather than print the unknown as $0.0000.
func TestCacheStatsShowsAnUnknownCostAsUnknown(t *testing.T) {
	var out bytes.Buffer
	rows := []cachemetrics.Metric{
		{ConversationID: "c-1", Seq: 1, PromptTokens: 100},
		{ConversationID: "c-1", Seq: 2, PromptTokens: 100, CostUSD: new(0.25)},
	}
	writeCacheStats(&out, time.Now(), rows, cachemetrics.Aggregate{Turns: 2, TotalPromptTokens: 200, TotalCostUSD: 0.25, UnpricedTurns: 1})
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 6 {
		t.Fatalf("cache-stats printed %d lines, want 6:\n%s", len(lines), out.String())
	}
	if !strings.HasSuffix(strings.TrimSpace(lines[2]), "n/a") || !strings.HasSuffix(strings.TrimSpace(lines[3]), "0.2500") {
		t.Fatalf("per-turn costs:\n%s", out.String())
	}
	if lines[5] != "1 turn(s) have no known cost: TOTAL COST_USD sums the priced ones only" {
		t.Fatalf("summary note = %q", lines[5])
	}
}

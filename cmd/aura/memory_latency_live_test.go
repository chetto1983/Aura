//go:build arcadedb_integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"
)

const memoryLatencySamples = 25

type memoryLatencyEvidence struct {
	Samples      int     `json:"samples"`
	P50MS        float64 `json:"p50_ms"`
	P95MS        float64 `json:"p95_ms"`
	MaxMS        float64 `json:"max_ms"`
	ColdRetained bool    `json:"cold_retained"`
	Path         string  `json:"path"`
}

func TestAgentMemoryCLILiveSearchP95(t *testing.T) {
	if os.Getenv("AURA_DB_URL") == "" {
		memoryLiveGap(t, "AURA_DB_URL is required")
	}
	durations := make([]time.Duration, 0, memoryLatencySamples)
	for range memoryLatencySamples {
		started := time.Now()
		ctx, err := withOperatorIdentity(context.Background())
		if err == nil {
			_, err = callMemoryToolText(ctx, "memory_search", map[string]any{
				"query": "Dove vive Davide?",
				"limit": 5,
			})
		}
		if err != nil {
			t.Fatalf("sample %d: %v", len(durations)+1, err)
		}
		durations = append(durations, time.Since(started))
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	evidence := memoryLatencyEvidence{
		Samples:      len(durations),
		P50MS:        durationMS(nearestRank(durations, 0.50)),
		P95MS:        durationMS(nearestRank(durations, 0.95)),
		MaxMS:        durationMS(durations[len(durations)-1]),
		ColdRetained: true,
		Path:         "cli_identity_mcp_search",
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("marshal latency evidence: %v", err)
	}
	t.Logf("AURA_AGENT_MEMORY_LATENCY_JSON=%s", payload)
	if evidence.P95MS > 1000 {
		t.Fatalf("memory_search p95 %.2fms exceeds 1000ms", evidence.P95MS)
	}
}

func nearestRank(samples []time.Duration, percentile float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	rank := int(float64(len(samples))*percentile + 0.999999999)
	return samples[max(1, rank)-1]
}

func durationMS(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}

func memoryLiveGap(t *testing.T, format string, args ...any) {
	t.Helper()
	message := fmt.Sprintf(format, args...)
	if os.Getenv("CI") != "" {
		t.Fatal(message)
	}
	t.Skip(message)
}

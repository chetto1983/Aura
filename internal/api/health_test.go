package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/aura/aura/internal/storage/reindex"
	"github.com/aura/aura/internal/tokenjuice"
)

func TestHealth_ReindexFieldPresent(t *testing.T) {
	// Build a Deps with a ReindexHealth callback returning known values.
	e := newTestEnv(t)
	e.router = NewRouter(Deps{
		Wiki:      e.wiki,
		Sources:   e.sources,
		Scheduler: e.sched,
		ReindexHealth: func() reindex.Health {
			return reindex.Health{QueueDepth: 7, Dropped: 3, DroppedAfterStop: 1}
		},
	})

	rr := e.do("GET", "/health")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	rx, ok := body["reindex"].(map[string]any)
	if !ok {
		t.Fatalf("response missing reindex field: %v", body)
	}
	if int(rx["queue_depth"].(float64)) != 7 {
		t.Fatalf("queue_depth = %v, want 7", rx["queue_depth"])
	}
	// WARNING 7 of 2026-05-11 plan revision 2: int64(...(float64)) is
	// valid Go — type assertion on `any` to extract float64, then
	// explicit conversion to int64. Compiles cleanly.
	if int64(rx["dropped"].(float64)) != 3 {
		t.Fatalf("dropped = %v, want 3", rx["dropped"])
	}
	if int64(rx["dropped_after_stop"].(float64)) != 1 {
		t.Fatalf("dropped_after_stop = %v, want 1", rx["dropped_after_stop"])
	}
}

func TestHealth_ReindexFieldZero_WhenCallbackNil(t *testing.T) {
	e := newTestEnv(t)
	// ReindexHealth is nil by default in newTestEnv — verify zero value is still present.

	rr := e.do("GET", "/health")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	rx, ok := body["reindex"].(map[string]any)
	if !ok {
		t.Fatalf("reindex field missing even when callback nil: %v", body)
	}
	if int(rx["queue_depth"].(float64)) != 0 {
		t.Fatalf("queue_depth = %v, want 0 (zero value)", rx["queue_depth"])
	}
}

// TestHealth_TokenJuiceFieldPresent verifies that /health always includes a
// tokenjuice block, and that after a compaction is recorded the aggregate
// values populate correctly — satisfying the Phase-TJ US-TJ06 integration AC:
// "end-to-end run with tokenjuice enabled → /api/health values > 0".
func TestHealth_TokenJuiceFieldPresent(t *testing.T) {
	// Isolate global stats so parallel test runs don't interfere.
	tokenjuice.ResetStats()
	t.Cleanup(tokenjuice.ResetStats)

	// Simulate two tool compactions (rule "aura/file-read").
	tokenjuice.RecordCompaction("aura/file-read", 1000, 100) // 900 bytes saved
	tokenjuice.RecordCompaction("aura/file-read", 500, 50)   // 450 bytes saved

	e := newTestEnv(t)
	rr := e.do("GET", "/health")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	tj, ok := body["tokenjuice"].(map[string]any)
	if !ok {
		t.Fatalf("response missing tokenjuice field: %v", body)
	}
	if int64(tj["total_calls"].(float64)) != 2 {
		t.Fatalf("total_calls = %v, want 2", tj["total_calls"])
	}
	if int64(tj["total_bytes_saved"].(float64)) != 1350 {
		t.Fatalf("total_bytes_saved = %v, want 1350", tj["total_bytes_saved"])
	}
	if tj["avg_ratio"].(float64) <= 0 {
		t.Fatalf("avg_ratio = %v, want > 0", tj["avg_ratio"])
	}
	rules, ok := tj["top_rules_by_savings"].([]any)
	if !ok || len(rules) == 0 {
		t.Fatalf("top_rules_by_savings empty or missing: %v", tj["top_rules_by_savings"])
	}
}

// TestHealth_TokenJuiceFieldZero_WhenNoCompactions verifies the field is
// present with zero values before any compactions have occurred.
func TestHealth_TokenJuiceFieldZero_WhenNoCompactions(t *testing.T) {
	tokenjuice.ResetStats()
	t.Cleanup(tokenjuice.ResetStats)

	e := newTestEnv(t)
	rr := e.do("GET", "/health")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	tj, ok := body["tokenjuice"].(map[string]any)
	if !ok {
		t.Fatalf("tokenjuice field missing: %v", body)
	}
	if int64(tj["total_calls"].(float64)) != 0 {
		t.Fatalf("total_calls = %v, want 0", tj["total_calls"])
	}
}

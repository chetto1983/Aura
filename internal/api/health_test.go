package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/aura/aura/internal/storage/reindex"
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

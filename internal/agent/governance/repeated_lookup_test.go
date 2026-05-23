package governance

import (
	"sync"
	"testing"
)

func TestRepeatedLookupCounterBlocksOnThirdWebSearch(t *testing.T) {
	c := NewRepeatedLookupCounter()
	args := map[string]any{"query": "foo"}
	if err := c.Check("web_search", args); err != nil {
		t.Fatalf("1st call should pass: %v", err)
	}
	if err := c.Check("web_search", args); err != nil {
		t.Fatalf("2nd call should pass: %v", err)
	}
	if err := c.Check("web_search", args); err == nil {
		t.Fatal("3rd call should return blocked error")
	}
}

func TestRepeatedLookupCounterWebFetchURLNormalization(t *testing.T) {
	c := NewRepeatedLookupCounter()
	// Uppercase host + trailing slash must normalize to the same key.
	if err := c.Check("web_fetch", map[string]any{"url": "https://EXAMPLE.COM/path/"}); err != nil {
		t.Fatalf("1st call: %v", err)
	}
	if err := c.Check("web_fetch", map[string]any{"url": "https://example.com/path"}); err != nil {
		t.Fatalf("2nd call same normalized URL: %v", err)
	}
	if err := c.Check("web_fetch", map[string]any{"url": "https://example.com/path"}); err == nil {
		t.Fatal("3rd call should be blocked")
	}
}

func TestRepeatedLookupCounterWebFetchPreservesQueryParams(t *testing.T) {
	c := NewRepeatedLookupCounter()
	// Distinct query params → different signature → both succeed.
	if err := c.Check("web_fetch", map[string]any{"url": "https://example.com/path?q=1"}); err != nil {
		t.Fatalf("1st call: %v", err)
	}
	if err := c.Check("web_fetch", map[string]any{"url": "https://example.com/path?q=2"}); err != nil {
		t.Fatalf("2nd call different query params: %v", err)
	}
}

func TestRepeatedLookupCounterSearchMemory(t *testing.T) {
	c := NewRepeatedLookupCounter()
	for i := range MaxRepeats {
		if err := c.Check("search_memory", map[string]any{"query": "test query"}); err != nil {
			t.Fatalf("call %d should pass: %v", i+1, err)
		}
	}
	if err := c.Check("search_memory", map[string]any{"query": "test query"}); err == nil {
		t.Fatal("call beyond MaxRepeats should be blocked")
	}
}

func TestRepeatedLookupCounterNormalizesCaseAndWhitespace(t *testing.T) {
	c := NewRepeatedLookupCounter()
	if err := c.Check("web_search", map[string]any{"query": "  FOO  "}); err != nil {
		t.Fatalf("1st call: %v", err)
	}
	if err := c.Check("web_search", map[string]any{"query": "foo"}); err != nil {
		t.Fatalf("2nd call normalized: %v", err)
	}
	if err := c.Check("web_search", map[string]any{"query": "FOO"}); err == nil {
		t.Fatal("3rd call (same normalized) should be blocked")
	}
}

func TestRepeatedLookupCounterIgnoresUntrackedTools(t *testing.T) {
	c := NewRepeatedLookupCounter()
	for range 100 {
		if err := c.Check("read_memory", map[string]any{"query": "foo"}); err != nil {
			t.Fatalf("untracked tool should never be blocked: %v", err)
		}
		if err := c.Check("schedule_task", map[string]any{"query": "foo"}); err != nil {
			t.Fatalf("untracked tool should never be blocked: %v", err)
		}
	}
}

func TestRepeatedLookupCounterDifferentTargetsDontInterfere(t *testing.T) {
	c := NewRepeatedLookupCounter()
	// Exhaust the budget for "foo".
	for range MaxRepeats + 1 {
		_ = c.Check("web_search", map[string]any{"query": "foo"})
	}
	// "bar" should still succeed — its counter is independent.
	if err := c.Check("web_search", map[string]any{"query": "bar"}); err != nil {
		t.Fatalf("different target should not be blocked: %v", err)
	}
}

func TestRepeatedLookupCounterResetsPerTurn(t *testing.T) {
	// Each turn gets a fresh counter so the budget restarts.
	// 2 sequential turns × 2 calls each = 4 total succeeding.
	for turn := range 2 {
		c := NewRepeatedLookupCounter()
		if err := c.Check("web_search", map[string]any{"query": "foo"}); err != nil {
			t.Fatalf("turn %d 1st call: %v", turn+1, err)
		}
		if err := c.Check("web_search", map[string]any{"query": "foo"}); err != nil {
			t.Fatalf("turn %d 2nd call: %v", turn+1, err)
		}
	}
	// Turn 3: 3rd call in the same turn is blocked.
	c := NewRepeatedLookupCounter()
	_ = c.Check("web_search", map[string]any{"query": "foo"})
	_ = c.Check("web_search", map[string]any{"query": "foo"})
	if err := c.Check("web_search", map[string]any{"query": "foo"}); err == nil {
		t.Fatal("3rd call in turn 3 should be blocked")
	}
}

func TestRepeatedLookupCounterConcurrentSafe(t *testing.T) {
	c := NewRepeatedLookupCounter()
	var wg sync.WaitGroup
	var mu sync.Mutex
	blocked := 0
	const goroutines = 10
	for range goroutines {
		wg.Go(func() {
			if err := c.Check("web_search", map[string]any{"query": "concurrent"}); err != nil {
				mu.Lock()
				blocked++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	want := goroutines - MaxRepeats
	if blocked != want {
		t.Errorf("blocked = %d, want %d", blocked, want)
	}
}

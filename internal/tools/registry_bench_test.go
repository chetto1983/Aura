package tools

import (
	"context"
	"sync"
	"testing"
	"time"
)

// sleepTool simulates a remote tool call (web fetch / wiki search) with a
// fixed wall-clock latency. Holds no state — concurrent Execute calls are
// safe.
type sleepTool struct {
	name  string
	delay time.Duration
}

func (s *sleepTool) Name() string                  { return s.name }
func (s *sleepTool) Description() string           { return "sleep" }
func (s *sleepTool) Parameters() map[string]any    { return map[string]any{} }
func (s *sleepTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(s.delay):
		return "ok", nil
	}
}

// BenchmarkRegistryExecuteSequential measures wall-clock time for 4
// independent tool calls run one-after-another (the pre-slice-11l path).
// With 10ms each, expect ~40ms.
func BenchmarkRegistryExecuteSequential(b *testing.B) {
	r := NewRegistry(nil)
	for i := 0; i < 4; i++ {
		r.Register(&sleepTool{name: "t" + string(rune('0'+i)), delay: 10 * time.Millisecond})
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 4; j++ {
			if _, err := r.Execute(ctx, "t"+string(rune('0'+j)), nil); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkRegistryExecuteParallel measures wall-clock time for the same
// 4 calls run concurrently — the slice 11l path. With 10ms each, expect
// ~10ms (bounded by slowest call, not the sum).
func BenchmarkRegistryExecuteParallel(b *testing.B) {
	r := NewRegistry(nil)
	for i := 0; i < 4; i++ {
		r.Register(&sleepTool{name: "t" + string(rune('0'+i)), delay: 10 * time.Millisecond})
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				if _, err := r.Execute(ctx, "t"+string(rune('0'+j)), nil); err != nil {
					b.Error(err)
				}
			}(j)
		}
		wg.Wait()
	}
}

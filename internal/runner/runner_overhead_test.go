package runner

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// The number has to be real, not a placeholder: it is subtracted from the compaction
// budget, so a zero here silently restores the blind spot it exists to close.
func TestManifestOverheadCountsTheToolsOnTheWire(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	r := &Runner{registry: reg}

	got := r.manifestOverheadTokens()
	if got <= 0 {
		t.Fatalf("manifestOverheadTokens() = %d, want the rendered defs to cost something", got)
	}
	// Cached: the manifest's size only moves when tools are registered, which is boot.
	if second := r.manifestOverheadTokens(); second != got {
		t.Fatalf("second call = %d, want the cached %d", second, got)
	}
}

// A Runner with no registry is a unit test's Runner. It must not panic and must not invent
// an overhead that would shift a budget nobody configured.
func TestManifestOverheadIsZeroWithoutARegistry(t *testing.T) {
	if got := (&Runner{}).manifestOverheadTokens(); got != 0 {
		t.Fatalf("manifestOverheadTokens() = %d, want 0", got)
	}
}

func TestValidateCompactionConfigRejectsUnreachableMeasuredTrigger(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	r := &Runner{
		registry:          reg,
		compactionEnabled: true,
		cfg: llm.Config{
			ContextWindow:            1_000,
			CompactionTriggerPercent: 1,
			TotalTimeoutSec:          120,
		},
	}
	overhead := r.manifestOverheadTokens()
	if overhead <= 10 {
		t.Fatalf("fixture overhead = %d, want more than the 10-token trigger budget", overhead)
	}
	err := r.ValidateCompactionConfig()
	if err == nil {
		t.Fatal("ValidateCompactionConfig accepted an unreachable enabled trigger")
	}
	for _, want := range []string{"unreachable", "1000", "10", "fixed prompt overhead"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error %q missing actionable evidence %q", err, want)
		}
	}
}

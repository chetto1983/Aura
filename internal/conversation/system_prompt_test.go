package conversation

import (
	"strings"
	"testing"
	"time"
)

func TestRenderRuntimeContextUsesExactOffset(t *testing.T) {
	loc := time.FixedZone("TEST", 90*60)
	now := time.Date(2026, 5, 4, 12, 30, 0, 0, time.UTC)

	got := RenderRuntimeContext(now, loc)
	for _, want := range []string{
		"Current local time: 2026-05-04 14:00:00 (TEST, UTC+01:30)",
		"Current UTC time: 2026-05-04T12:30:00Z",
		"User timezone: TEST",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("runtime context missing %q:\n%s", want, got)
		}
	}
}

func TestRenderSystemPromptIncludesRuntimeContext(t *testing.T) {
	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	got := RenderSystemPrompt(now, time.UTC)
	for _, want := range []string{"You are Aura", "## Runtime Context", "Current UTC time: 2026-05-04T10:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Fatalf("system prompt missing %q", want)
		}
	}
}

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

func TestDefaultSystemPromptDoesNotAdvertiseSpecializedTools(t *testing.T) {
	got := DefaultSystemPrompt()
	for _, forbidden := range []string{
		"create_xlsx",
		"create_docx",
		"create_pdf",
		"Use execute_code for:",
		"/tmp/aura_out",
		"Use list_files for directory inventory",
		"For project or file audits",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("system prompt advertises specialized tool guidance %q", forbidden)
		}
	}
}

func TestDefaultSystemPromptRequiresStaleReferenceVerification(t *testing.T) {
	got := DefaultSystemPrompt()
	for _, want := range []string{"stale references", "verify it with the narrowest relevant tool"} {
		if !strings.Contains(got, want) {
			t.Fatalf("system prompt missing stale-reference guidance %q", want)
		}
	}
}

func TestDefaultSystemPromptKeepsDefaultToolGuidanceSmall(t *testing.T) {
	got := DefaultSystemPrompt()
	for _, want := range []string{
		"Use search_memory",
		"Use schedule_task only for explicit reminder or task-creation requests",
		"workspace file tools only when they are explicitly exposed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("system prompt missing default hot-path guidance %q", want)
		}
	}
}

package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

// operationalLesson builds a compact_memory_documents Document that
// WriteApprovedLesson would produce for a given tool + error class.
func operationalLesson(sig, toolName, errorClass string, updatedAt time.Time) memoryindex.Document {
	return memoryindex.Document{
		ID:        "operational:" + sig,
		Kind:      memoryindex.KindOperational,
		Title:     "Lesson: " + toolName + " " + errorClass,
		Body:      "Tool `" + toolName + "` repeated `" + errorClass + "` failures. Reason pattern: timeout. Sample attempts: [abc123].",
		Handle:    "operational:" + sig,
		Status:    "active",
		UpdatedAt: updatedAt,
	}
}

// TestRecallOperationalTool_EmptyStore returns the "no operational lessons yet"
// sentinel when no approved lessons exist.
func TestRecallOperationalTool_EmptyStore(t *testing.T) {
	tool := NewRecallOperationalTool(fakeCompactMemorySearch{docs: nil})
	out, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "no operational lessons yet" {
		t.Errorf("got %q, want %q", out, "no operational lessons yet")
	}
}

// TestRecallOperationalTool_Populated returns N hits ordered by recency when
// approved lessons exist and no filter is applied.
func TestRecallOperationalTool_Populated(t *testing.T) {
	now := time.Now().UTC()
	older := now.Add(-24 * time.Hour)
	docs := []memoryindex.Document{
		operationalLesson("aabbcc", "web_search", "io", older),
		operationalLesson("ddeeff", "create_xlsx", "auth", now),
	}
	tool := NewRecallOperationalTool(fakeCompactMemorySearch{docs: docs})
	// fakeCompactMemorySearch returns docs in insertion order; Execute sorts by recency.
	out, err := tool.Execute(context.Background(), map[string]any{"query": "lesson"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "2 operational lesson(s)") {
		t.Errorf("expected 2 lessons in output:\n%s", out)
	}
	// create_xlsx (newer) must appear before web_search (older).
	idxXlsx := strings.Index(out, "create_xlsx")
	idxWeb := strings.Index(out, "web_search")
	if idxXlsx < 0 || idxWeb < 0 {
		t.Fatalf("expected both tool names in output:\n%s", out)
	}
	if idxXlsx > idxWeb {
		t.Errorf("create_xlsx (newer) should appear before web_search (older):\n%s", out)
	}
	// Handles must be present in "operational:<sig>" form.
	if !strings.Contains(out, "operational:aabbcc") || !strings.Contains(out, "operational:ddeeff") {
		t.Errorf("expected operational: handles in output:\n%s", out)
	}
}

// TestRecallOperationalTool_FilterByToolName returns only lessons for the
// requested tool_name, excluding unrelated lessons.
func TestRecallOperationalTool_FilterByToolName(t *testing.T) {
	now := time.Now().UTC()
	docs := []memoryindex.Document{
		operationalLesson("aabbcc", "web_search", "io", now.Add(-1*time.Hour)),
		operationalLesson("ddeeff", "create_xlsx", "auth", now),
	}
	tool := NewRecallOperationalTool(fakeCompactMemorySearch{docs: docs})
	out, err := tool.Execute(context.Background(), map[string]any{"tool_name": "web_search"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "web_search") {
		t.Errorf("expected web_search lesson in output:\n%s", out)
	}
	if strings.Contains(out, "create_xlsx") {
		t.Errorf("create_xlsx should be excluded when filtering by web_search:\n%s", out)
	}
	if !strings.Contains(out, "1 operational lesson(s)") {
		t.Errorf("expected exactly 1 lesson:\n%s", out)
	}
}

// TestRecallOperationalTool_PendingProposalsExcluded verifies that documents
// with Status != "active" are never surfaced.
func TestRecallOperationalTool_PendingProposalsExcluded(t *testing.T) {
	now := time.Now().UTC()
	pending := memoryindex.Document{
		ID:        "operational:pending1",
		Kind:      memoryindex.KindOperational,
		Title:     "Lesson: web_search io",
		Body:      "pending proposal body",
		Handle:    "operational:pending1",
		Status:    "pending", // NOT active
		UpdatedAt: now,
	}
	tool := NewRecallOperationalTool(fakeCompactMemorySearch{docs: []memoryindex.Document{pending}})
	out, err := tool.Execute(context.Background(), map[string]any{"query": "lesson"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "no operational lessons yet" {
		t.Errorf("pending proposal leaked into output: %q", out)
	}
}

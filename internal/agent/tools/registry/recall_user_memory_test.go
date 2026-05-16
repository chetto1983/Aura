package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

// userFact builds a compact_memory_documents Document that
// WriteApprovedUserFact would produce for a given category + fact.
func userFact(handle, category, fact string, updatedAt time.Time) memoryindex.Document {
	return memoryindex.Document{
		ID:        handle,
		Kind:      memoryindex.KindUserMemory,
		Title:     fact,
		Body:      fact,
		Handle:    handle,
		Status:    "active",
		Tags:      []string{category},
		UpdatedAt: updatedAt,
	}
}

// TestRecallUserMemoryTool_EmptyStore returns the "no user facts yet"
// sentinel when no approved user facts exist.
func TestRecallUserMemoryTool_EmptyStore(t *testing.T) {
	tool := NewRecallUserMemoryTool(fakeCompactMemorySearch{docs: nil})
	out, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "no user facts yet" {
		t.Errorf("got %q, want %q", out, "no user facts yet")
	}
}

// TestRecallUserMemoryTool_Populated returns N hits ordered by recency when
// approved user facts exist and no filter is applied.
func TestRecallUserMemoryTool_Populated(t *testing.T) {
	now := time.Now().UTC()
	older := now.Add(-24 * time.Hour)
	docs := []memoryindex.Document{
		userFact("user_memory:aabbcc", "preference", "Prefers dark mode in IDEs", older),
		userFact("user_memory:ddeeff", "person", "Lives in Milan, Italy", now),
	}
	tool := NewRecallUserMemoryTool(fakeCompactMemorySearch{docs: docs})
	// fakeCompactMemorySearch returns docs in insertion order; Execute sorts by recency.
	out, err := tool.Execute(context.Background(), map[string]any{"query": "user"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "2 user fact(s)") {
		t.Errorf("expected 2 facts in output:\n%s", out)
	}
	// Milan (newer) must appear before dark mode (older).
	idxMilan := strings.Index(out, "Milan")
	idxDark := strings.Index(out, "dark mode")
	if idxMilan < 0 || idxDark < 0 {
		t.Fatalf("expected both facts in output:\n%s", out)
	}
	if idxMilan > idxDark {
		t.Errorf("Milan (newer) should appear before dark mode (older):\n%s", out)
	}
	// Handles must be present.
	if !strings.Contains(out, "user_memory:aabbcc") || !strings.Contains(out, "user_memory:ddeeff") {
		t.Errorf("expected user_memory: handles in output:\n%s", out)
	}
}

// TestRecallUserMemoryTool_FilterByCategory returns only facts for the
// requested category, excluding unrelated facts.
func TestRecallUserMemoryTool_FilterByCategory(t *testing.T) {
	now := time.Now().UTC()
	docs := []memoryindex.Document{
		userFact("user_memory:aabbcc", "preference", "Prefers dark mode in IDEs", now.Add(-1*time.Hour)),
		userFact("user_memory:ddeeff", "person", "Lives in Milan, Italy", now),
	}
	tool := NewRecallUserMemoryTool(fakeCompactMemorySearch{docs: docs})
	out, err := tool.Execute(context.Background(), map[string]any{"category": "preference"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "dark mode") {
		t.Errorf("expected preference fact in output:\n%s", out)
	}
	if strings.Contains(out, "Milan") {
		t.Errorf("person fact should be excluded when filtering by preference:\n%s", out)
	}
	if !strings.Contains(out, "1 user fact(s)") {
		t.Errorf("expected exactly 1 fact:\n%s", out)
	}
}

// TestRecallUserMemoryTool_PendingProposalsExcluded verifies that documents
// with Status != "active" are never surfaced.
func TestRecallUserMemoryTool_PendingProposalsExcluded(t *testing.T) {
	now := time.Now().UTC()
	pending := memoryindex.Document{
		ID:        "user_memory:pending1",
		Kind:      memoryindex.KindUserMemory,
		Title:     "Pending user fact",
		Body:      "pending proposal body",
		Handle:    "user_memory:pending1",
		Status:    "pending", // NOT active
		Tags:      []string{"preference"},
		UpdatedAt: now,
	}
	tool := NewRecallUserMemoryTool(fakeCompactMemorySearch{docs: []memoryindex.Document{pending}})
	out, err := tool.Execute(context.Background(), map[string]any{"query": "user"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "no user facts yet" {
		t.Errorf("pending proposal leaked into output: %q", out)
	}
}

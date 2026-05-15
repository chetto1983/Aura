package attempts_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
)

// fakeRepo is a test double for attempts.Repo. Data is keyed by toolName;
// rows must be provided newest-first to match the repo.Recent contract.
type fakeRepo struct {
	data map[string][]attempts.ToolAttempt
}

func (f *fakeRepo) Record(_ context.Context, _ tools.ToolObservation) error { return nil }

func (f *fakeRepo) Recent(_ context.Context, _, toolName string, n int) ([]attempts.ToolAttempt, error) {
	rows := f.data[toolName]
	if len(rows) > n {
		rows = rows[:n]
	}
	// return a copy so callers cannot mutate the test fixture
	return append([]attempts.ToolAttempt(nil), rows...), nil
}

// failAttempt builds a ToolAttempt with outcome derived from class, for test setup.
func failAttempt(toolName, class string, endedAt time.Time) attempts.ToolAttempt {
	return attempts.ToolAttempt{
		ToolName: toolName,
		ToolKind: tools.ToolKindOf(toolName),
		Outcome:  tools.BucketOf(class).String(),
		Class:    class,
		EndedAt:  endedAt,
	}
}

func TestBriefer_EmptyRepo_NoCapsule(t *testing.T) {
	repo := &fakeRepo{data: map[string][]attempts.ToolAttempt{}}
	b := attempts.NewBriefing(repo, nil)
	result := b.Brief(context.Background(), "run1",
		[]string{"web_search", "read_memory"},
		map[string]struct{}{"web_search": {}, "read_memory": {}})
	if result != "" {
		t.Fatalf("expected empty capsule, got: %q", result)
	}
}

func TestBriefer_AllOKOutcomes_NoCapsule(t *testing.T) {
	now := time.Now()
	repo := &fakeRepo{data: map[string][]attempts.ToolAttempt{
		"web_search": {
			{ToolName: "web_search", Outcome: "ok", Class: "", EndedAt: now},
		},
	}}
	b := attempts.NewBriefing(repo, nil)
	result := b.Brief(context.Background(), "run1", []string{"web_search"}, map[string]struct{}{"web_search": {}})
	if result != "" {
		t.Fatalf("expected empty capsule for ok outcomes, got: %q", result)
	}
}

func TestBriefer_ThreeToolsWithFailures_ThreeLineCapsule(t *testing.T) {
	now := time.Now()
	repo := &fakeRepo{data: map[string][]attempts.ToolAttempt{
		"web_fetch": {
			failAttempt("web_fetch", "validation", now.Add(-1*time.Minute)),
			failAttempt("web_fetch", "validation", now.Add(-2*time.Minute)),
		},
		"web_search": {
			failAttempt("web_search", "rate_limited", now.Add(-30*time.Second)),
		},
		"read_memory": {
			failAttempt("read_memory", "io", now.Add(-5*time.Minute)),
		},
	}}
	b := attempts.NewBriefing(repo, nil)
	result := b.Brief(context.Background(), "run1",
		[]string{"web_fetch", "web_search", "read_memory"},
		map[string]struct{}{"web_fetch": {}, "web_search": {}, "read_memory": {}})

	if result == "" {
		t.Fatal("expected non-empty capsule")
	}
	if !strings.Contains(result, "Recent tool experience") {
		t.Fatalf("capsule missing header: %q", result)
	}
	for _, tool := range []string{"web_fetch", "web_search", "read_memory"} {
		if !strings.Contains(result, tool) {
			t.Fatalf("capsule missing tool %s in: %q", tool, result)
		}
	}
	// Count lines starting with "- "
	toolLines := 0
	for _, line := range strings.Split(result, "\n") {
		if strings.HasPrefix(line, "- ") {
			toolLines++
		}
	}
	if toolLines != 3 {
		t.Fatalf("expected 3 tool lines, got %d in:\n%s", toolLines, result)
	}
}

func TestBriefer_TwelveTools_OnlyTopEight(t *testing.T) {
	now := time.Now()
	data := map[string][]attempts.ToolAttempt{}
	toolNames := make([]string, 12)
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("tool_%02d", i)
		toolNames[i] = name
		data[name] = []attempts.ToolAttempt{
			failAttempt(name, "validation", now.Add(-time.Duration(i+1)*time.Minute)),
		}
	}
	repo := &fakeRepo{data: data}
	b := attempts.NewBriefing(repo, nil)

	availableSet := make(map[string]struct{}, 12)
	for _, n := range toolNames {
		availableSet[n] = struct{}{}
	}

	result := b.Brief(context.Background(), "run1", toolNames, availableSet)

	toolLines := 0
	for _, line := range strings.Split(result, "\n") {
		if strings.HasPrefix(line, "- ") {
			toolLines++
		}
	}
	if toolLines != 8 {
		t.Fatalf("expected 8 tool lines (cap), got %d in:\n%s", toolLines, result)
	}
}

func TestBriefer_TwelveTools_TopEightAreNewest(t *testing.T) {
	now := time.Now()
	data := map[string][]attempts.ToolAttempt{}
	toolNames := make([]string, 12)
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("tool_%02d", i)
		toolNames[i] = name
		// tool_00 is newest (-1min), tool_11 is oldest (-12min)
		data[name] = []attempts.ToolAttempt{
			failAttempt(name, "validation", now.Add(-time.Duration(i+1)*time.Minute)),
		}
	}
	repo := &fakeRepo{data: data}
	b := attempts.NewBriefing(repo, nil)

	availableSet := make(map[string]struct{}, 12)
	for _, n := range toolNames {
		availableSet[n] = struct{}{}
	}

	result := b.Brief(context.Background(), "run1", toolNames, availableSet)

	// top 8 newest = tool_00..tool_07 (offsets 1..8 min)
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("tool_%02d", i)
		if !strings.Contains(result, name) {
			t.Errorf("expected %s in top-8 capsule, missing in:\n%s", name, result)
		}
	}
	// oldest 4 should be absent
	for i := 8; i < 12; i++ {
		name := fmt.Sprintf("tool_%02d", i)
		if strings.Contains(result, name) {
			t.Errorf("expected %s absent (too old), but found in:\n%s", name, result)
		}
	}
}

func TestBriefer_MCPToolOffline_AnnotatesUnavailable(t *testing.T) {
	now := time.Now()
	repo := &fakeRepo{data: map[string][]attempts.ToolAttempt{
		"mcp_github_search": {
			failAttempt("mcp_github_search", "rate_limited", now),
		},
	}}
	b := attempts.NewBriefing(repo, nil)

	// mcp_github_search NOT in availableSet (MCP server offline)
	result := b.Brief(context.Background(), "run1",
		[]string{"mcp_github_search"},
		map[string]struct{}{})

	if !strings.Contains(result, "(currently unavailable)") {
		t.Fatalf("expected '(currently unavailable)' annotation for offline MCP tool, got: %q", result)
	}
	if !strings.Contains(result, "mcp_github_search") {
		t.Fatalf("expected tool name in capsule, got: %q", result)
	}
}

func TestBriefer_MCPToolOnline_NoUnavailableAnnotation(t *testing.T) {
	now := time.Now()
	repo := &fakeRepo{data: map[string][]attempts.ToolAttempt{
		"mcp_github_search": {
			failAttempt("mcp_github_search", "rate_limited", now),
		},
	}}
	b := attempts.NewBriefing(repo, nil)

	// mcp_github_search IS in availableSet (server is online)
	result := b.Brief(context.Background(), "run1",
		[]string{"mcp_github_search"},
		map[string]struct{}{"mcp_github_search": {}})

	if strings.Contains(result, "(currently unavailable)") {
		t.Fatalf("unexpected '(currently unavailable)' for online MCP tool, got: %q", result)
	}
}

func TestBriefer_NativeTool_NoUnavailableAnnotation(t *testing.T) {
	now := time.Now()
	repo := &fakeRepo{data: map[string][]attempts.ToolAttempt{
		"web_fetch": {
			failAttempt("web_fetch", "validation", now),
		},
	}}
	b := attempts.NewBriefing(repo, nil)

	// web_fetch is native — even if absent from availableSet, no "(currently unavailable)"
	result := b.Brief(context.Background(), "run1",
		[]string{"web_fetch"},
		map[string]struct{}{})

	if strings.Contains(result, "(currently unavailable)") {
		t.Fatalf("native tool should not get unavailable annotation, got: %q", result)
	}
}

func TestBriefer_TotalCap_RespectedWithExtremeInputs(t *testing.T) {
	now := time.Now()
	data := map[string][]attempts.ToolAttempt{}
	toolNames := make([]string, 8)
	longErr := strings.Repeat("x", 300) // exceeds per-tool cap after formatting
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("tool_%02d", i)
		toolNames[i] = name
		data[name] = []attempts.ToolAttempt{
			{
				ToolName:      name,
				ToolKind:      "native",
				Outcome:       "fatal",
				Class:         "error",
				ErrorRedacted: longErr,
				EndedAt:       now.Add(-time.Duration(i) * time.Minute),
			},
		}
	}
	repo := &fakeRepo{data: data}
	b := attempts.NewBriefing(repo, nil)

	availableSet := make(map[string]struct{}, 8)
	for _, n := range toolNames {
		availableSet[n] = struct{}{}
	}

	result := b.Brief(context.Background(), "run1", toolNames, availableSet)
	if len(result) > 1500 {
		t.Fatalf("capsule exceeds 1500-char cap: got %d chars", len(result))
	}
}

func TestBriefer_PerToolCap_RespectedOnLongLine(t *testing.T) {
	now := time.Now()
	longErr := strings.Repeat("z", 300)
	repo := &fakeRepo{data: map[string][]attempts.ToolAttempt{
		"web_fetch": {
			{
				ToolName:      "web_fetch",
				ToolKind:      "native",
				Outcome:       "fatal",
				Class:         "error",
				ErrorRedacted: longErr,
				EndedAt:       now,
			},
		},
	}}
	b := attempts.NewBriefing(repo, nil)
	result := b.Brief(context.Background(), "run1",
		[]string{"web_fetch"},
		map[string]struct{}{"web_fetch": {}})

	for _, line := range strings.Split(result, "\n") {
		if strings.HasPrefix(line, "- ") && len(line) > 200 {
			t.Fatalf("tool line exceeds 200-char cap (%d chars): %q", len(line), line)
		}
	}
}

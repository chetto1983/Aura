// Package toolindex — eval_topk_test.go runs a fixture-driven top-K
// accuracy check against the lexical tool discovery path (Registry.Search).
//
// Threshold: 70% (≥10/14 queries must rank the expected tool in top-3).
// The registry has no vector index in this environment; scores are purely
// lexical (BM25-style name/description matching). The threshold is a
// regression guard, not a quality bar — if description improvements raise
// accuracy, update the threshold upward to lock in the gain.
//
// Skip: AURA_SKIP_TOOL_EVALS=1 go test ./...
package toolindex

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	tools "github.com/aura/aura/internal/agent/tools/registry"
)

type evalEntry struct {
	Query        string `json:"query"`
	ExpectedTool string `json:"expected_tool"`
	Rationale    string `json:"rationale"`
}

// TestDiscoveryTopKEval asserts ≥70% of fixture queries rank the expected
// tool in the top-3 results from Registry.Search (lexical path only).
// When below threshold, per-query results are printed so regressions are
// immediately actionable.
func TestDiscoveryTopKEval(t *testing.T) {
	if os.Getenv("AURA_SKIP_TOOL_EVALS") == "1" {
		t.Skip("AURA_SKIP_TOOL_EVALS=1: skipping tool discovery eval")
	}

	data, err := os.ReadFile("eval_topk_fixture.json")
	if err != nil {
		t.Fatalf("read eval_topk_fixture.json: %v", err)
	}
	var entries []evalEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("fixture is empty")
	}

	reg := buildEvalRegistry()

	const topK = 3
	// 70% threshold locks in the current lexical-search baseline.
	const threshold = 0.70

	hits := 0
	var missLines []string
	for _, e := range entries {
		results := reg.Search(e.Query, topK)
		found := false
		for _, r := range results {
			if r.Name == e.ExpectedTool {
				found = true
				break
			}
		}
		if found {
			hits++
		} else {
			names := make([]string, 0, len(results))
			for _, r := range results {
				names = append(names, fmt.Sprintf("%s(%d)", r.Name, r.Score))
			}
			missLines = append(missLines, fmt.Sprintf(
				"  MISS  query=%q  expected=%q  top%d=[%s]",
				e.Query, e.ExpectedTool, topK, strings.Join(names, ", ")))
		}
	}

	total := len(entries)
	pct := float64(hits) / float64(total)

	if len(missLines) > 0 {
		t.Logf("Per-query misses (%d/%d hit):\n%s", hits, total, strings.Join(missLines, "\n"))
	}
	if pct < threshold {
		t.Errorf("top-%d accuracy %.0f%% (%d/%d) is below threshold %.0f%%; misses logged above",
			topK, pct*100, hits, total, threshold*100)
	}
}

// buildEvalRegistry creates a registry populated with all native tool types
// using zero-value structs. Name(), Description(), and Parameters() on every
// tool are hardcoded and do not access internal state, so zero-value
// instantiation is safe for the lexical search path.
func buildEvalRegistry() *tools.Registry {
	reg := tools.NewRegistry(nil)
	for _, tool := range nativeEvalTools() {
		reg.Register(tool)
	}
	return reg
}

func nativeEvalTools() []tools.Tool {
	return []tools.Tool{
		&tools.AskUserTool{},
		&tools.RequestDashboardTokenTool{},
		&tools.ToolSearchTool{},
		&tools.SearchMemoryTool{},
		&tools.WebTool{},
		&tools.WikiPageTool{},
		&tools.TaskTool{},
		&tools.SourceTool{},
		&tools.FileTool{},
		&tools.DocTool{},
		&tools.ExecuteCodeTool{},
		&tools.ExecuteShellTool{},
		&tools.DevToolTool{},
		&tools.DailyBriefingTool{},
	}
}

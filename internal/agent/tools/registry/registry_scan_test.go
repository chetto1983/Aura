package tools

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// isDefaultState returns true when a ToolDefinition is in the fail-closed
// all-defaults state: all four MCP hints false AND VisibilityActiveTurn. A
// tool in this state was not explicitly catalogued — normalizeToolDefinition
// sets ActiveTurn as the default tier, and all hints default to false.
func isDefaultState(d ToolDefinition) bool {
	return !d.ReadOnlyHint && !d.DestructiveHint && !d.IdempotentHint && !d.OpenWorldHint &&
		d.VisibilityTier == VisibilityActiveTurn
}

// TestCatalogueScanNativeTools verifies that every native registered tool type
// has been explicitly catalogued with MCP hints + VisibilityTier.
//
// Swarm tools (swarmtools package) cannot be instantiated here because
// swarmtools imports this package (cycle). They are verified in
// internal/agent/tools/swarm/tools_test.go::TestCatalogueSwarmToolMetadata.
func TestCatalogueScanNativeTools(t *testing.T) {
	// All concrete native tool types from this package. Using zero-value
	// structs is safe because Definition() methods are hardcoded and never
	// read the struct fields. Tools requiring *time.Location get time.Local
	// to satisfy any nil-guard in normalizeToolDefinition.
	toolsToCheck := []Tool{
		&AskUserTool{},
		&RequestDashboardTokenTool{},
		&ToolSearchTool{},
		&SearchMemoryTool{},
		&RecallOperationalTool{},
		&WebTool{},
		&WikiPageTool{},
		&TaskTool{loc: time.Local},
		&SourceTool{},
		&FileTool{},
		&DocTool{},
		&ExecuteCodeTool{},
		&ExecuteShellTool{},
		&DevToolTool{},
		&DailyBriefingTool{loc: time.Local, now: time.Now},
	}

	var failures []string
	for _, tool := range toolsToCheck {
		def := definitionForTool(tool)
		if isDefaultState(def) {
			failures = append(failures, fmt.Sprintf(
				"  %s: all hints=false AND tier=active_turn (not explicitly catalogued)",
				def.Name,
			))
		}
	}

	if len(failures) > 0 {
		t.Errorf("catalogue scan: %d native tool(s) have default (uncatalogued) metadata:\n%s\n"+
			"Fix: add a Definition() method on each tool with explicit hints + VisibilityTier.",
			len(failures), strings.Join(failures, "\n"))
	}
}

// TestCatalogueAlwaysOnTools verifies the three control-surface tools are
// marked VisibilityAlwaysOn.
func TestCatalogueAlwaysOnTools(t *testing.T) {
	alwaysOn := []Tool{
		&AskUserTool{},
		&RequestDashboardTokenTool{},
		&ToolSearchTool{},
	}
	for _, tool := range alwaysOn {
		def := definitionForTool(tool)
		if def.VisibilityTier != VisibilityAlwaysOn {
			t.Errorf("%s: VisibilityTier = %q, want %q", def.Name, def.VisibilityTier, VisibilityAlwaysOn)
		}
	}
}

// TestCatalogueDestructiveTools verifies tools that can delete or overwrite
// data carry DestructiveHint=true.
func TestCatalogueDestructiveTools(t *testing.T) {
	destructive := []Tool{
		&SourceTool{},    // delete action
		&FileTool{},      // write/patch actions
		&TaskTool{loc: time.Local}, // cancel/run_now mutate tasks
		&ExecuteCodeTool{},
		&ExecuteShellTool{},
		&WikiPageTool{},  // replace/edit can overwrite page content
	}
	for _, tool := range destructive {
		def := definitionForTool(tool)
		if !def.DestructiveHint {
			t.Errorf("%s: DestructiveHint = false, want true", def.Name)
		}
	}
}

// TestCatalogueOpenWorldTools verifies tools whose results depend on external
// / mutable state carry OpenWorldHint=true.
func TestCatalogueOpenWorldTools(t *testing.T) {
	openWorld := []Tool{
		&WebTool{},
		&SearchMemoryTool{}, // wiki content drifts as pages are updated
	}
	for _, tool := range openWorld {
		def := definitionForTool(tool)
		if !def.OpenWorldHint {
			t.Errorf("%s: OpenWorldHint = false, want true", def.Name)
		}
	}
}

// TestCatalogueReadOnlyHintConsistency verifies that tools claiming ReadOnly
// do not also claim Destructive (would be contradictory).
func TestCatalogueReadOnlyHintConsistency(t *testing.T) {
	toolsToCheck := []Tool{
		&AskUserTool{},
		&RequestDashboardTokenTool{},
		&ToolSearchTool{},
		&SearchMemoryTool{},
		&RecallOperationalTool{},
		&WebTool{},
		&WikiPageTool{},
		&TaskTool{loc: time.Local},
		&SourceTool{},
		&FileTool{},
		&DocTool{},
		&ExecuteCodeTool{},
		&ExecuteShellTool{},
		&DevToolTool{},
		&DailyBriefingTool{loc: time.Local, now: time.Now},
	}
	for _, tool := range toolsToCheck {
		def := definitionForTool(tool)
		if def.ReadOnlyHint && def.DestructiveHint {
			t.Errorf("%s: ReadOnlyHint=true and DestructiveHint=true are contradictory", def.Name)
		}
	}
}

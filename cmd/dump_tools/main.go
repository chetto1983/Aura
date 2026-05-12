// Command dump_tools dumps Name+Description+Parameters of all registered
// built-in tools to stdout as JSON. Tool structs are zero-value initialized;
// fields used only by Execute() (DB handles, schedulers, etc.) are nil but
// Name/Description/Parameters do not touch them.
//
// Used for offline benchmarking of tool_search strategies — see
// /tmp/benchmark_tools.py.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/aura/aura/internal/tools"
)

type toolDump struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

func main() {
	all := []tools.Tool{
		&tools.DirectWebFetchTool{},
		&tools.ExecuteCodeTool{},
		&tools.ExecuteShellTool{},
		&tools.RequestDashboardTokenTool{},
		&tools.DailyBriefingTool{},
		&tools.CreateXLSXTool{},
		&tools.CreatePDFTool{},
		&tools.IngestSourceTool{},
		&tools.SearchMemoryTool{},
		&tools.CreateDOCXTool{},
		&tools.TaskTool{},
		&tools.SearXNGSearchTool{},
		&tools.DeleteSourceTool{},
		&tools.OCRSourceTool{},
		&tools.ReadSourceTool{},
		&tools.ListSourcesTool{},
		&tools.LintSourcesTool{},
		&tools.StoreSourceTool{},
		&tools.DevToolTool{},
		&tools.WikiPageTool{},
		&tools.ListFilesTool{},
		&tools.ReadFileTool{},
		&tools.SearchFilesTool{},
		&tools.WriteFileTool{},
		&tools.ApplyPatchTool{},
	}

	out := make([]toolDump, 0, len(all))
	for _, t := range all {
		out = append(out, toolDump{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
}

// Command export-tools dumps Aura's real, in-tree tool specifications to JSON so
// the FunctionGemma fine-tuning pipeline trains and evaluates against the exact
// tool surface the agent dispatches against — not a hand-maintained copy that
// silently drifts from the code.
//
// It constructs the same built-in registry that cmd/aura/main.go wires, using
// zero-value tools: a tool's Spec() is static metadata and reads no live
// dependency (the few that touch a field — skill's loader, tool_search's
// registry — are nil-guarded), so the manifest is reproducible without a
// database, sidecars, or secrets.
//
// Run from the repo root:
//
//	go run ./finetune/exporter -out finetune/data/aura_tools.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// toolRecord is the full per-tool view the dataset builder needs: both the
// default-manifest fields (Name + Summary) and the post-tool_search fields
// (Description + Parameters), so synthetic traces can teach BOTH the deferred
// stub turn and the loaded-schema turn.
type toolRecord struct {
	Name        string          `json:"name"`
	Summary     string          `json:"summary"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Deferred    bool            `json:"deferred"`
	Mutating    bool            `json:"mutating"`
}

// export is the file the Python pipeline reads. DefaultToolDefs mirrors exactly
// what the agent puts in req.Tools on a fresh turn (deferred tools contribute
// only Name + Summary), so the dataset's system view matches production.
type export struct {
	Tools           []toolRecord    `json:"tools"`
	DefaultToolDefs json.RawMessage `json:"default_tooldefs"`
}

// builtins mirrors cmd/aura/main.go's registration set (21 tools). task and
// skill are constructed via package-internal helpers in production; their zero
// values expose the same static Spec(), which is all the exporter reads.
func builtins(reg *tools.Registry) []tools.Tool {
	return []tools.Tool{
		tools.TextResponse{},
		&tools.ToolSearch{Registry: reg},
		&tools.ReadToolOutput{},
		tools.CurrentTime{},
		tools.AskUser{},
		&tools.TaskTool{},
		&tools.TodoTool{},
		&tools.SkillTool{},
		&tools.SkillManageTool{Skills: &tools.SkillTool{}},
		&tools.WebSearch{},
		&tools.WebFetch{},
		&tools.DocumentSearch{},
		&tools.ShellExec{},
		&tools.ShellPoll{},
		&tools.ShellKill{},
		&tools.ReadFile{},
		&tools.WriteFile{},
		&tools.Patch{},
		&tools.SearchFiles{},
		&tools.SendFile{},
		&tools.SwarmSpawn{},
	}
}

func main() {
	out := flag.String("out", "", "output JSON path (default: stdout)")
	flag.Parse()

	reg := tools.NewRegistry()
	for _, t := range builtins(reg) {
		reg.Register(t)
	}
	if err := reg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "export-tools: registry invalid: %v\n", err)
		os.Exit(1)
	}

	records := make([]toolRecord, 0, len(reg.All()))
	for _, t := range reg.All() {
		s := t.Spec()
		records = append(records, toolRecord{
			Name:        s.Name,
			Summary:     s.Summary,
			Description: s.Description,
			Parameters:  append(json.RawMessage(nil), s.Parameters...),
			Deferred:    s.Deferred,
			Mutating:    s.Mutating,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })

	defaultDefs, err := json.Marshal(reg.RenderToolDefs(nil))
	if err != nil {
		fmt.Fprintf(os.Stderr, "export-tools: marshal tooldefs: %v\n", err)
		os.Exit(1)
	}

	blob, err := json.MarshalIndent(export{Tools: records, DefaultToolDefs: defaultDefs}, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "export-tools: marshal export: %v\n", err)
		os.Exit(1)
	}
	blob = append(blob, '\n')

	if *out == "" {
		_, _ = os.Stdout.Write(blob)
		return
	}
	if err := os.WriteFile(*out, blob, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "export-tools: write %s: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "export-tools: wrote %d tools to %s\n", len(records), *out)
}

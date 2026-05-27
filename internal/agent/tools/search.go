package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolSearch is the built-in hook tool that lets the LLM fetch full specs of
// deferred tools. The pattern mirrors Claude Code's ToolSearch behavior: the
// model sees only Name+Summary by default, then calls `tool_search` with a
// `select:<name>,<name>` argument or a free-text query to load the full
// Description+Parameters into context.
//
// This is a NON-DEFERRED tool — always visible — so the model can find it
// without recursion.
type ToolSearch struct {
	Registry *Registry
}

type toolSearchArgs struct {
	Query string `json:"query"`
}

func (ts *ToolSearch) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Either 'select:Name1,Name2' to load specific tools by name, or a keyword phrase to match deferred tool names and summaries."
    }
  },
  "required": ["query"]
}`)
	return Spec{
		Name:        "tool_search",
		Summary:     "Load full specifications for deferred tools by name or keyword.",
		Description: "Fetches deferred-tool schemas so they become callable. Use 'select:Name1,Name2' for direct selection, or keyword phrase to discover relevant tools by matching their summaries.",
		Parameters:  params,
		Deferred:    false,
	}
}

func (ts *ToolSearch) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var args toolSearchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("tool_search args: %w", err)
	}
	q := strings.TrimSpace(args.Query)
	if q == "" {
		return "", fmt.Errorf("tool_search: query is required")
	}

	matches := ts.match(q)
	if len(matches) == 0 {
		return "no matching tools", nil
	}

	var b strings.Builder
	for _, t := range matches {
		s := t.Spec()
		fmt.Fprintf(&b, "## %s\n%s\n\nParameters:\n%s\n\n", s.Name, s.Description, string(s.Parameters))
	}
	return b.String(), nil
}

func (ts *ToolSearch) match(q string) []Tool {
	if sel, ok := strings.CutPrefix(q, "select:"); ok {
		names := strings.Split(sel, ",")
		out := make([]Tool, 0, len(names))
		for _, n := range names {
			n = strings.TrimSpace(n)
			if t, ok := ts.Registry.Get(n); ok {
				out = append(out, t)
			}
		}
		return out
	}
	// keyword match: case-insensitive substring against Name and Summary, only deferred tools
	ql := strings.ToLower(q)
	out := make([]Tool, 0, 4)
	for _, t := range ts.Registry.All() {
		s := t.Spec()
		if !s.Deferred {
			continue
		}
		if strings.Contains(strings.ToLower(s.Name), ql) || strings.Contains(strings.ToLower(s.Summary), ql) {
			out = append(out, t)
		}
	}
	return out
}

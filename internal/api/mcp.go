package api

import (
	"net/http"
	"sort"
)

func handleMCPServers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := make([]MCPServerSummary, 0, len(deps.MCP))
		for _, c := range deps.MCP {
			if c == nil {
				continue
			}
			tools := c.Tools()
			summary := MCPServerSummary{
				Name:      c.Name(),
				Transport: c.Transport(),
				ToolCount: len(tools),
				Tools:     make([]MCPToolInfo, 0, len(tools)),
			}
			for _, tool := range tools {
				summary.Tools = append(summary.Tools, MCPToolInfo{
					Name:        tool.Name,
					Description: tool.Description,
					InputSchema: tool.InputSchema,
				})
			}
			sort.Slice(summary.Tools, func(i, j int) bool {
				return summary.Tools[i].Name < summary.Tools[j].Name
			})
			out = append(out, summary)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

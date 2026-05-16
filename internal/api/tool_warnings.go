package api

import (
	"net/http"
	"time"
)

// handleToolWarnings serves GET /api/tool-warnings. Returns aggregated failure
// counts from tool_attempts for the last 7 days, grouped by (tool, class).
// Admin-gated via deps.SkillsAdmin — consistent with the skills admin
// convention. Returns an empty warnings array (not 404) when no data exists.
func handleToolWarnings(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !deps.SkillsAdmin {
			writeError(w, deps.Logger, http.StatusForbidden,
				"tool warnings require admin access (set SKILLS_ADMIN=true)")
			return
		}
		if deps.ToolWarnings == nil {
			writeJSON(w, deps.Logger, http.StatusOK,
				ToolWarningsResponse{Warnings: []ToolWarningItem{}})
			return
		}
		rows, err := deps.ToolWarnings.Warnings(r.Context(), 7)
		if err != nil {
			deps.Logger.Warn("api: tool-warnings query", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError,
				"failed to query tool warnings")
			return
		}
		out := make([]ToolWarningItem, 0, len(rows))
		for _, row := range rows {
			out = append(out, ToolWarningItem{
				Kind:     row.ToolKind,
				Tool:     row.ToolName,
				Class:    row.Class,
				N:        row.N,
				LastSeen: row.LastSeen.UTC().Format(time.RFC3339),
			})
		}
		writeJSON(w, deps.Logger, http.StatusOK, ToolWarningsResponse{Warnings: out})
	}
}

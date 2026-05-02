package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aura/aura/internal/scheduler"
)

func handleMaintenanceList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Issues == nil {
			writeJSON(w, deps.Logger, http.StatusOK, []WikiIssue{})
			return
		}
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "open"
		}
		severity := r.URL.Query().Get("severity")

		rows, err := deps.Issues.List(r.Context(), status)
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list issues")
			return
		}

		out := make([]WikiIssue, 0, len(rows))
		for _, issue := range rows {
			if severity != "" && issue.Severity != severity {
				continue
			}
			out = append(out, issueToDTO(issue))
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func handleMaintenanceResolve(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, fmt.Sprintf("invalid id %q", idStr))
			return
		}
		if deps.Issues == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "issue not found")
			return
		}

		// Fetch before resolving to return the updated row.
		issue, err := deps.Issues.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, scheduler.ErrIssueNotFound) {
				writeError(w, deps.Logger, http.StatusNotFound, "issue not found")
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to fetch issue")
			return
		}
		if issue.Status != "open" {
			writeError(w, deps.Logger, http.StatusConflict, "issue already resolved")
			return
		}

		if err := deps.Issues.Resolve(r.Context(), id); err != nil {
			if errors.Is(err, scheduler.ErrIssueNotFound) {
				writeError(w, deps.Logger, http.StatusNotFound, "issue not found")
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to resolve issue")
			return
		}
		issue.Status = "resolved"
		now := time.Now().UTC()
		issue.ResolvedAt = &now
		writeJSON(w, deps.Logger, http.StatusOK, issueToDTO(issue))
	}
}

func issueToDTO(i scheduler.Issue) WikiIssue {
	dto := WikiIssue{
		ID:         i.ID,
		Kind:       i.Kind,
		Severity:   i.Severity,
		Slug:       i.Slug,
		BrokenLink: i.BrokenLink,
		Message:    i.Message,
		Status:     i.Status,
		CreatedAt:  i.CreatedAt.UTC().Format(time.RFC3339),
	}
	if i.ResolvedAt != nil {
		dto.ResolvedAt = i.ResolvedAt.UTC().Format(time.RFC3339)
	}
	return dto
}

package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/learning"
)

type quarantineDecider interface {
	SetStatusFrom(ctx context.Context, id int64, fromStatus string, newStatus string) (summarizer.ProposedUpdate, error)
	DeleteStatus(ctx context.Context, id int64, status string) error
}

func handleQuarantineList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Summaries == nil {
			writeJSON(w, deps.Logger, http.StatusOK, []ProposedUpdate{})
			return
		}
		limit := 100
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
				limit = l
			}
		}
		rows, err := deps.Summaries.List(r.Context(), "quarantine", limit)
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list quarantine")
			return
		}
		out := make([]ProposedUpdate, len(rows))
		for i, p := range rows {
			out[i] = proposalToDTO(p)
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func handleQuarantineApprove(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathParamInt64(w, deps.Logger, r, "id")
		if !ok {
			return
		}
		repo, ok := deps.Summaries.(quarantineDecider)
		if deps.Summaries == nil || !ok {
			writeError(w, deps.Logger, http.StatusNotFound, "quarantine store unavailable")
			return
		}
		updated, err := repo.SetStatusFrom(r.Context(), id, "quarantine", "accepted")
		if err != nil {
			writeQuarantineDecisionError(w, deps, err)
			return
		}
		if updated.Kind == "operational_memory" && deps.OperationalMemory != nil {
			lp := learning.Proposal{
				Kind:          updated.Kind,
				Fact:          updated.Fact,
				SignatureHash: updated.SignatureHash,
				ToolName:      updated.TargetSlug,
				ErrorClass:    updated.Category,
			}
			if err := learning.WriteApprovedLesson(r.Context(), deps.OperationalMemory, lp); err != nil {
				deps.Logger.Warn("quarantine approve: write operational lesson failed", "id", updated.ID, "error", err)
			}
		}
		writeJSON(w, deps.Logger, http.StatusOK, proposalToDTO(updated))
	}
}

func handleQuarantineDelete(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathParamInt64(w, deps.Logger, r, "id")
		if !ok {
			return
		}
		repo, ok := deps.Summaries.(quarantineDecider)
		if deps.Summaries == nil || !ok {
			writeError(w, deps.Logger, http.StatusNotFound, "quarantine store unavailable")
			return
		}
		if err := repo.DeleteStatus(r.Context(), id, "quarantine"); err != nil {
			writeQuarantineDecisionError(w, deps, err)
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"ok": true, "id": id})
	}
}

func writeQuarantineDecisionError(w http.ResponseWriter, deps Deps, err error) {
	switch {
	case errors.Is(err, summarizer.ErrProposalNotFound):
		writeError(w, deps.Logger, http.StatusNotFound, "quarantine item not found")
	case errors.Is(err, summarizer.ErrProposalConflict):
		writeError(w, deps.Logger, http.StatusConflict, "quarantine item already decided")
	default:
		writeError(w, deps.Logger, http.StatusInternalServerError, "failed to update quarantine item")
	}
}

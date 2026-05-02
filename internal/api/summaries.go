package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aura/aura/internal/conversation/summarizer"
)

func handleSummariesList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Summaries == nil {
			writeJSON(w, deps.Logger, http.StatusOK, []ProposedUpdate{})
			return
		}
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "pending"
		}
		limit := 100
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
				limit = l
			}
		}
		rows, err := deps.Summaries.List(r.Context(), status, limit)
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list summaries")
			return
		}
		out := make([]ProposedUpdate, len(rows))
		for i, p := range rows {
			out[i] = proposalToDTO(p)
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func handleSummariesApprove(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseProposalID(r)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		if deps.Summaries == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "summaries store unavailable")
			return
		}

		// Fetch the proposal first to reconstruct the Decision for AutoApplier.
		proposal, err := deps.Summaries.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, summarizer.ErrProposalNotFound) {
				writeError(w, deps.Logger, http.StatusNotFound, "proposal not found")
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to fetch proposal")
			return
		}
		if proposal.Status != "pending" {
			writeError(w, deps.Logger, http.StatusConflict, "proposal already decided")
			return
		}

		// Apply via AutoApplier if a wiki writer is wired.
		if deps.SummariesWiki != nil {
			applier := summarizer.NewAutoApplier(deps.SummariesWiki)
			dec := summarizer.Decision{
				Candidate: summarizer.Candidate{
					Fact:          proposal.Fact,
					Category:      "fact",
					SourceTurnIDs: proposal.SourceTurnIDs,
				},
				Action:     summarizer.Action(proposal.Action),
				TargetSlug: proposal.TargetSlug,
				Similarity: proposal.Similarity,
			}
			if applyErr := applier.Apply(r.Context(), dec); applyErr != nil {
				deps.Logger.Warn("summaries approve: apply failed", "id", id, "error", applyErr)
				// Don't block the status flip — log and continue.
			}
		}

		updated, err := deps.Summaries.SetStatus(r.Context(), id, "approved")
		if err != nil {
			if errors.Is(err, summarizer.ErrProposalConflict) {
				writeError(w, deps.Logger, http.StatusConflict, "proposal already decided")
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to approve proposal")
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, proposalToDTO(updated))
	}
}

func handleSummariesReject(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseProposalID(r)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		if deps.Summaries == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "summaries store unavailable")
			return
		}
		updated, err := deps.Summaries.SetStatus(r.Context(), id, "rejected")
		if err != nil {
			if errors.Is(err, summarizer.ErrProposalNotFound) {
				writeError(w, deps.Logger, http.StatusNotFound, "proposal not found")
				return
			}
			if errors.Is(err, summarizer.ErrProposalConflict) {
				writeError(w, deps.Logger, http.StatusConflict, "proposal already decided")
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to reject proposal")
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, proposalToDTO(updated))
	}
}

func parseProposalID(r *http.Request) (int64, error) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id %q", idStr)
	}
	return id, nil
}

func proposalToDTO(p summarizer.ProposedUpdate) ProposedUpdate {
	ids := p.SourceTurnIDs
	if ids == nil {
		ids = []int64{}
	}
	return ProposedUpdate{
		ID:            p.ID,
		ChatID:        p.ChatID,
		Fact:          p.Fact,
		Action:        p.Action,
		TargetSlug:    p.TargetSlug,
		Similarity:    p.Similarity,
		SourceTurnIDs: ids,
		Status:        p.Status,
		CreatedAt:     p.CreatedAt.UTC().Format(time.RFC3339),
	}
}

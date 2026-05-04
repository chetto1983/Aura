package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

		proposal, err := deps.Summaries.SetStatus(r.Context(), id, "approved")
		if err != nil {
			if errors.Is(err, summarizer.ErrProposalNotFound) {
				writeError(w, deps.Logger, http.StatusNotFound, "proposal not found")
				return
			}
			if errors.Is(err, summarizer.ErrProposalConflict) {
				writeError(w, deps.Logger, http.StatusConflict, "proposal already decided")
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to approve proposal")
			return
		}

		// Apply via AutoApplier if a wiki writer is wired. The status flip
		// happens first so concurrent approve/reject requests cannot both apply.
		if deps.SummariesWiki != nil {
			applier := summarizer.NewAutoApplier(deps.SummariesWiki)
			dec := summarizer.Decision{
				Candidate: summarizer.Candidate{
					Fact:          proposal.Fact,
					Category:      proposalCategory(proposal.Category),
					RelatedSlugs:  proposal.RelatedSlugs,
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

		writeJSON(w, deps.Logger, http.StatusOK, proposalToDTO(proposal))
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

func handleSummariesBatchApprove(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleSummariesBatchDecision(w, r, deps, "approved")
	}
}

func handleSummariesBatchReject(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleSummariesBatchDecision(w, r, deps, "rejected")
	}
}

func handleSummariesBatchDecision(w http.ResponseWriter, r *http.Request, deps Deps, status string) {
	if deps.Summaries == nil {
		writeError(w, deps.Logger, http.StatusNotFound, "summaries store unavailable")
		return
	}
	req, err := parseSummaryBatchRequest(r)
	if err != nil {
		writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
		return
	}

	resp := SummaryBatchResponse{
		Updated: []ProposedUpdate{},
		Failed:  []SummaryBatchFailure{},
	}
	for _, id := range req.IDs {
		proposal, err := deps.Summaries.SetStatus(r.Context(), id, status)
		if err != nil {
			resp.Failed = append(resp.Failed, SummaryBatchFailure{
				ID:    id,
				Error: summaryDecisionError(err),
			})
			continue
		}
		if status == "approved" {
			applyApprovedSummary(r.Context(), deps, proposal)
		}
		resp.Updated = append(resp.Updated, proposalToDTO(proposal))
	}
	writeJSON(w, deps.Logger, http.StatusOK, resp)
}

func parseSummaryBatchRequest(r *http.Request) (SummaryBatchRequest, error) {
	var req SummaryBatchRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return SummaryBatchRequest{}, fmt.Errorf("invalid JSON body")
	}
	if len(req.IDs) == 0 {
		return SummaryBatchRequest{}, fmt.Errorf("ids is required")
	}
	if len(req.IDs) > 100 {
		return SummaryBatchRequest{}, fmt.Errorf("ids is limited to 100")
	}
	seen := map[int64]struct{}{}
	ids := make([]int64, 0, len(req.IDs))
	for _, id := range req.IDs {
		if id <= 0 {
			return SummaryBatchRequest{}, fmt.Errorf("ids must contain positive proposal ids")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	req.IDs = ids
	return req, nil
}

func applyApprovedSummary(ctx context.Context, deps Deps, proposal summarizer.ProposedUpdate) {
	// Apply via AutoApplier if a wiki writer is wired. The status flip
	// happens first so concurrent approve/reject requests cannot both apply.
	if deps.SummariesWiki == nil {
		return
	}
	applier := summarizer.NewAutoApplier(deps.SummariesWiki)
	dec := summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:          proposal.Fact,
			Category:      proposalCategory(proposal.Category),
			RelatedSlugs:  proposal.RelatedSlugs,
			SourceTurnIDs: proposal.SourceTurnIDs,
		},
		Action:     summarizer.Action(proposal.Action),
		TargetSlug: proposal.TargetSlug,
		Similarity: proposal.Similarity,
	}
	if applyErr := applier.Apply(ctx, dec); applyErr != nil {
		deps.Logger.Warn("summaries approve: apply failed", "id", proposal.ID, "error", applyErr)
		// Don't block the status flip; log and continue.
	}
}

func summaryDecisionError(err error) string {
	switch {
	case errors.Is(err, summarizer.ErrProposalNotFound):
		return "proposal not found"
	case errors.Is(err, summarizer.ErrProposalConflict):
		return "proposal already decided"
	default:
		return "decision failed"
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
	related := p.RelatedSlugs
	if related == nil {
		related = []string{}
	}
	return ProposedUpdate{
		ID:            p.ID,
		ChatID:        p.ChatID,
		Fact:          p.Fact,
		Action:        p.Action,
		TargetSlug:    p.TargetSlug,
		Similarity:    p.Similarity,
		SourceTurnIDs: ids,
		Category:      p.Category,
		RelatedSlugs:  related,
		Provenance:    provenanceToDTO(p.Provenance),
		Status:        p.Status,
		CreatedAt:     p.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func provenanceToDTO(p summarizer.Provenance) Provenance {
	evidence := make([]EvidenceRef, 0, len(p.Evidence))
	for _, ref := range p.Evidence {
		evidence = append(evidence, EvidenceRef{
			Kind:    ref.Kind,
			ID:      ref.ID,
			Title:   ref.Title,
			Page:    ref.Page,
			Snippet: ref.Snippet,
		})
	}
	return Provenance{
		OriginTool:   p.OriginTool,
		OriginReason: p.OriginReason,
		Evidence:     evidence,
		AgentJobID:   p.AgentJobID,
		SwarmRunID:   p.SwarmRunID,
		SwarmTaskID:  p.SwarmTaskID,
	}
}

func proposalCategory(category string) string {
	if category == "" {
		return "fact"
	}
	return category
}

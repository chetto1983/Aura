package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/learning"
)

type SkillProposalApplyRequest struct {
	Action  string
	Name    string
	Content string
}

type SkillProposalApplier interface {
	ApplySkillProposal(ctx context.Context, proposal SkillProposalApplyRequest) error
}

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
		id, ok := pathParamInt64(w, deps.Logger, r, "id")
		if !ok {
			return
		}
		if deps.Summaries == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "summaries store unavailable")
			return
		}

		proposal, err := approveSummaryProposal(r.Context(), deps, id)
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

		writeJSON(w, deps.Logger, http.StatusOK, proposalToDTO(proposal))
	}
}

func handleSummariesReject(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathParamInt64(w, deps.Logger, r, "id")
		if !ok {
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
		var proposal summarizer.ProposedUpdate
		var err error
		if status == "approved" {
			proposal, err = approveSummaryProposal(r.Context(), deps, id)
		} else {
			proposal, err = deps.Summaries.SetStatus(r.Context(), id, status)
		}
		if err != nil {
			resp.Failed = append(resp.Failed, SummaryBatchFailure{
				ID:    id,
				Error: summaryDecisionError(err),
			})
			continue
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

func approveSummaryProposal(ctx context.Context, deps Deps, id int64) (summarizer.ProposedUpdate, error) {
	proposal, err := deps.Summaries.Get(ctx, id)
	if err != nil {
		return summarizer.ProposedUpdate{}, err
	}
	if proposal.Status != "pending" {
		return summarizer.ProposedUpdate{}, summarizer.ErrProposalConflict
	}

	nextStatus := "approved"
	if summarizer.IsSkillAction(proposal.Action) {
		nextStatus = "reviewed"
		if deps.SkillsAdmin && deps.SkillProposals != nil && proposal.Provenance.Skill != nil {
			if err := deps.SkillProposals.ApplySkillProposal(ctx, skillProposalApplyRequest(*proposal.Provenance.Skill)); err != nil {
				return summarizer.ProposedUpdate{}, fmt.Errorf("apply skill proposal: %w", err)
			}
			nextStatus = "approved"
		}
	}

	updated, err := deps.Summaries.SetStatus(ctx, id, nextStatus)
	if err != nil {
		return summarizer.ProposedUpdate{}, err
	}
	if nextStatus == "approved" {
		applyApprovedSummary(ctx, deps, updated)
	}
	return updated, nil
}

func skillProposalApplyRequest(p summarizer.SkillProposal) SkillProposalApplyRequest {
	action := strings.TrimSpace(p.Action)
	if action != "" && !strings.HasPrefix(action, "skill_") {
		action = "skill_" + action
	}
	return SkillProposalApplyRequest{
		Action:  action,
		Name:    strings.TrimSpace(p.Name),
		Content: p.Content,
	}
}

func applyApprovedSummary(ctx context.Context, deps Deps, proposal summarizer.ProposedUpdate) {
	// Operational memory proposals are written to compact_memory_documents
	// via learning.WriteApprovedLesson instead of the wiki AutoApplier.
	if proposal.Kind == "operational_memory" {
		if deps.OperationalMemory != nil {
			lp := learning.Proposal{
				Kind:          proposal.Kind,
				Fact:          proposal.Fact,
				SignatureHash: proposal.SignatureHash,
				ToolName:      proposal.TargetSlug,
				ErrorClass:    proposal.Category,
			}
			if err := learning.WriteApprovedLesson(ctx, deps.OperationalMemory, lp); err != nil {
				deps.Logger.Warn("summaries approve: write operational lesson failed", "id", proposal.ID, "error", err)
			}
		}
		return
	}
	// User memory proposals are written to compact_memory_documents
	// via learning.WriteApprovedUserFact (Phase-O / US-O03).
	if proposal.Kind == "user_memory" {
		if deps.UserMemory != nil {
			lp := learning.Proposal{
				Kind:          proposal.Kind,
				Fact:          proposal.Fact,
				SignatureHash: proposal.SignatureHash,
				Category:      proposal.Category,
			}
			if err := learning.WriteApprovedUserFact(ctx, deps.UserMemory, lp); err != nil {
				deps.Logger.Warn("summaries approve: write user fact failed", "id", proposal.ID, "error", err)
			}
		}
		return
	}
	// Apply via AutoApplier if a wiki writer is wired. The status flip
	// happens first so concurrent approve/reject requests cannot both apply.
	if !summarizer.IsWikiAction(proposal.Action) {
		return
	}
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
		ID:             p.ID,
		ChatID:         p.ChatID,
		Fact:           p.Fact,
		Action:         p.Action,
		TargetSlug:     p.TargetSlug,
		Similarity:     p.Similarity,
		SourceTurnIDs:  ids,
		Category:       p.Category,
		RelatedSlugs:   related,
		Provenance:     provenanceToDTO(p.Provenance),
		SkillLifecycle: skillLifecycleToDTO(p),
		Status:         p.Status,
		CreatedAt:      p.CreatedAt.UTC().Format(time.RFC3339),
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
		ProposalKind: p.ProposalKind,
		Evidence:     evidence,
		Skill:        skillProposalToDTO(p.Skill),
		AgentJobID:   p.AgentJobID,
		SwarmRunID:   p.SwarmRunID,
		SwarmTaskID:  p.SwarmTaskID,
	}
}

func skillProposalToDTO(p *summarizer.SkillProposal) *SkillProposal {
	if p == nil {
		return nil
	}
	return &SkillProposal{
		Action:       p.Action,
		Name:         p.Name,
		Description:  p.Description,
		AllowedTools: append([]string(nil), p.AllowedTools...),
		SmokePrompt:  p.SmokePrompt,
		Content:      p.Content,
		Reason:       p.Reason,
	}
}

func skillLifecycleToDTO(p summarizer.ProposedUpdate) *SkillLifecycle {
	if !summarizer.IsSkillAction(p.Action) {
		return nil
	}
	name := ""
	action := p.Action
	if p.Provenance.Skill != nil {
		name = p.Provenance.Skill.Name
		if p.Provenance.Skill.Action != "" {
			action = "skill_" + p.Provenance.Skill.Action
		}
	}
	nextStep := "Use the explicit admin skill workflow to install/update/delete the reviewed skill, then run the smoke prompt and record the result."
	if name != "" {
		nextStep = fmt.Sprintf("Use the explicit admin skill workflow for %q to install/update/delete the reviewed skill, then run the smoke prompt and record the result.", name)
	}
	mode := "review_only"
	reviewStatus := "pending_review"
	installStatus := "not_installed_by_summary_approval"
	smokeStatus := "operator_required"
	switch p.Status {
	case "approved":
		mode = "admin_apply_requested"
		reviewStatus = "reviewed"
		installStatus = "approval_applied_check_list_skills"
	case "rejected":
		reviewStatus = "rejected"
	case "reviewed":
		reviewStatus = "reviewed"
	}
	return &SkillLifecycle{
		Mode:          mode,
		ReviewStatus:  reviewStatus,
		InstallStatus: installStatus,
		SmokeStatus:   smokeStatus,
		NextStep:      strings.TrimSpace(action + ": " + nextStep),
	}
}

func proposalCategory(category string) string {
	if category == "" {
		return "fact"
	}
	return category
}

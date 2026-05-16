// Package learning implements the Phase-N lesson promotion pipeline.
// It aggregates repeated tool failures from tool_attempts and creates
// review-gated proposals in proposed_updates (kind='operational_memory').
package learning

import (
	"context"
	"fmt"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
)

// CreateProposalParams holds parameters for creating an operational memory proposal.
type CreateProposalParams struct {
	SignatureHash     string
	Summary          string
	ToolName         string
	ErrorClass       string
	AttemptCount     int
	SampleAttemptIDs []string
	FirstSeen        time.Time
	LastSeen         time.Time
}

// ProposalStore is the write side for operational memory proposals.
// Satisfied by SQLProposalStore in proposal_adapter.go.
type ProposalStore interface {
	// CreateOperationalMemoryProposal inserts a pending proposal.
	// Returns created=false if a row with the same signature_hash already exists.
	CreateOperationalMemoryProposal(ctx context.Context, params CreateProposalParams) (created bool, err error)
}

// PromoteLessons reads aggregated failure statistics from repo and writes
// review-gated operational memory proposals to store.
// Returns (promoted, skipped, error): promoted = new proposals created,
// skipped = proposals already present (idempotency via signature_hash).
func PromoteLessons(
	ctx context.Context,
	repo attempts.Repo,
	store ProposalStore,
	minOccurrences int,
	sinceDays int,
) (promoted int, skipped int, err error) {
	candidates, err := repo.AggregateForPromotion(ctx, minOccurrences, sinceDays)
	if err != nil {
		return 0, 0, fmt.Errorf("promote lessons: aggregate: %w", err)
	}
	for _, lc := range candidates {
		summary := fmt.Sprintf(
			"Tool `%s` repeated `%s` failures (%d times in last %d days). Reason pattern: %s. Sample attempts: %v.",
			lc.ToolName, lc.ErrorClass, lc.AttemptCount, sinceDays, lc.Reason, lc.SampleAttemptIDs,
		)
		created, createErr := store.CreateOperationalMemoryProposal(ctx, CreateProposalParams{
			SignatureHash:    lc.SignatureHash,
			Summary:         summary,
			ToolName:        lc.ToolName,
			ErrorClass:      lc.ErrorClass,
			AttemptCount:    lc.AttemptCount,
			SampleAttemptIDs: lc.SampleAttemptIDs,
			FirstSeen:       lc.FirstSeen,
			LastSeen:        lc.LastSeen,
		})
		if createErr != nil {
			return promoted, skipped, fmt.Errorf("promote lessons: create proposal: %w", createErr)
		}
		if created {
			promoted++
		} else {
			skipped++
		}
	}
	return promoted, skipped, nil
}

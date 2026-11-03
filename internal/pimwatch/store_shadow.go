package pimwatch

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// MailFacts are the four Signals the urgency centroid reads, as a mail's row keeps them. WrittenTo is
// true only when the provider said so.
type MailFacts struct{ WrittenTo, ProviderImportant, AuthFailed, Bulk bool }

// Shadow is what a mail's row keeps beside the ladder for the scoring
// (docs/superpowers/specs/2026-11-03-urgency-centroid-design.md). Nothing in it changes a verdict.
type Shadow struct {
	Facts MailFacts
	// SenderMemory is the per-sender baseline's tier, "" when it could not be read.
	SenderMemory Tier
	// Centroid is the centroid's tier, "" when it did not answer, and Margin its top-2 margin.
	Centroid Tier
	Margin   float64
}

// mailVerdictParams writes a mail's ladder answer from its decision and the rest from its shadow.
func mailVerdictParams(id pgtype.UUID, p PageCommit, draft VerdictDraft, decidedAt time.Time) sqlc.InsertPIMWatchVerdictParams {
	params := sqlc.InsertPIMWatchVerdictParams{
		IdentityID: id, AccountID: p.AccountID, Resource: p.Resource, ItemID: draft.ItemID,
		CalendarID: draft.CalendarID, Tier: string(draft.Decision.Tier), LabelID: optionalUUID(draft.Decision.LabelID),
		DecidedBy: string(draft.Decision.By), AuthFailed: draft.Decision.AuthFailed, Fingerprint: draft.Fingerprint,
		DecidedAt:  timestamp(decidedAt),
		LadderTier: optionalText(string(draft.Decision.Tier)), LadderBy: optionalText(string(draft.Decision.By)),
	}
	if shadow := draft.Shadow; shadow != nil {
		params.WrittenTo, params.ProviderImportant, params.Bulk = shadow.Facts.WrittenTo, shadow.Facts.ProviderImportant, shadow.Facts.Bulk
		params.SenderMemoryTier = optionalText(string(shadow.SenderMemory))
		params.CentroidTier = optionalText(string(shadow.Centroid))
		params.CentroidMargin = pgtype.Float8{Float64: shadow.Margin, Valid: shadow.Centroid != ""}
	}
	return params
}

// SenderMemory reads each sender's last corrected tier, §7 G2b's baseline. A sender never corrected
// has no key.
func (s *Store) SenderMemory(ctx context.Context, identityID string, senderKeys []string) (map[string]Tier, error) {
	out := make(map[string]Tier, len(senderKeys))
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		rows, err := q.ListPIMWatchSenderMemory(ctx, sqlc.ListPIMWatchSenderMemoryParams{IdentityID: id, SenderKeys: senderKeys})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out[r.SenderKey] = Tier(r.Tier.String)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read pim watch sender memory: %w", err)
	}
	return out, nil
}

// CorrectedAccounts lists the accounts with a correction: the owners whose examples still count.
func (s *Store) CorrectedAccounts(ctx context.Context, identityID string) ([]string, error) {
	var out []string
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		var err error
		out, err = q.ListPIMWatchCorrectedAccounts(ctx, id)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read pim watch corrected accounts: %w", err)
	}
	return out, nil
}

// ShadowRow is one mail the report scores: what stands now, the ladder's answer and the shadow's.
type ShadowRow struct {
	Tier, LadderTier, Centroid, SenderMemory Tier
	By, LadderBy                             DecidedBy
	Margin                                   float64
	DecidedAt                                time.Time
	Notified                                 bool
}

// ShadowRows reads the mail decided since then that carries a ladder answer, so written after 0152.
func (s *Store) ShadowRows(ctx context.Context, identityID string, since time.Time) ([]ShadowRow, error) {
	var out []ShadowRow
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		rows, err := q.ListPIMWatchShadowRows(ctx, sqlc.ListPIMWatchShadowRowsParams{IdentityID: id, Since: timestamp(since)})
		if err != nil {
			return err
		}
		out = make([]ShadowRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, ShadowRow{
				Tier: Tier(r.Tier), LadderTier: Tier(r.LadderTier.String), Centroid: Tier(r.CentroidTier.String),
				SenderMemory: Tier(r.SenderMemoryTier.String), By: DecidedBy(r.DecidedBy), LadderBy: DecidedBy(r.LadderBy.String),
				Margin: r.CentroidMargin.Float64, DecidedAt: r.DecidedAt.Time, Notified: r.Notified,
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read pim watch shadow rows: %w", err)
	}
	return out, nil
}

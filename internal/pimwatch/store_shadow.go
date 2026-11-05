package pimwatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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

// ModelAnswer is what the decision model said about a mail (docs/superpowers/specs/2026-11-03-decision-model-labels-design.md,
// decision 11). Nothing in it is mail text.
type ModelAnswer struct {
	// LabelID is the top option's label, "" when it chose none.
	LabelID     string
	Probability float64
	// Name is the model that answered.
	Name string
	// Options is how many options were offered.
	Options int
	// Applied says the Decision carries the outcome: the label was sure enough and within the floor.
	Applied bool
}

// centroidDisagrees is the centroid's tier where it differs from the ladder's, else "". It compares
// with the ladder, never with a correction: the mark says where the centroid would have decided
// otherwise than the rules did.
func (v Verdict) centroidDisagrees() Tier {
	if v.Centroid != "" && v.Centroid != v.LadderTier {
		return v.Centroid
	}
	return ""
}

// mailVerdictParams writes a mail's decision, the ladder's answer apart from it, the model's answer
// and the rest from its shadow. A draft with no Ladder is one the ladder alone decided.
func mailVerdictParams(id pgtype.UUID, p PageCommit, draft VerdictDraft, decidedAt time.Time) sqlc.InsertPIMWatchVerdictParams {
	ladder := draft.Ladder
	if ladder.Tier == "" {
		ladder = draft.Decision
	}
	params := sqlc.InsertPIMWatchVerdictParams{
		IdentityID: id, AccountID: p.AccountID, Resource: p.Resource, ItemID: draft.ItemID,
		CalendarID: draft.CalendarID, Tier: string(draft.Decision.Tier), LabelID: optionalUUID(draft.Decision.LabelID),
		DecidedBy: string(draft.Decision.By), AuthFailed: draft.Decision.AuthFailed, Fingerprint: draft.Fingerprint,
		DecidedAt:  timestamp(decidedAt),
		LadderTier: optionalText(string(ladder.Tier)), LadderBy: optionalText(string(ladder.By)),
		Discarded: draft.Decision.Discarded, ModelSkipped: draft.ModelSkipped,
	}
	if model := draft.Model; model != nil {
		params.ModelProbability = pgtype.Float8{Float64: model.Probability, Valid: true}
		params.ModelName = optionalText(model.Name)
		params.ModelOptions = pgtype.Int2{Int16: int16(model.Options), Valid: model.Options > 0} //nolint:gosec // G115: the CHECK bounds it to 255.
		params.ModelLabelID = optionalUUID(model.LabelID)
		if model.Applied {
			params.ModelTier = optionalText(string(draft.Decision.Tier))
			params.ModelDiscarded = draft.Decision.Discarded
		}
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

// LastDigest is when the last digest window closed: zero when none was sent or the identity has no
// watch. Unlike Settings it writes nothing, so the report stays a read.
func (s *Store) LastDigest(ctx context.Context, identityID string) (time.Time, error) {
	var out time.Time
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		row, err := q.GetPIMWatchSettings(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out = row.LastDigestAt.Time
		return nil
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("read pim watch last digest: %w", err)
	}
	return out, nil
}

// ShadowRow is one mail the report scores: what stands now, the ladder's answer and the shadow's.
// ModelTier is the tier the decision model's label gave, "" when it gave none.
type ShadowRow struct {
	Tier, LadderTier, Centroid, SenderMemory, ModelTier Tier
	By, LadderBy                                        DecidedBy
	Margin                                              float64
	DecidedAt                                           time.Time
	Notified                                            bool
}

// ShadowRows reads the mail decided since then that carries a ladder answer, so written after 0152.
// A zero since reads them all: timestamp would make it NULL, and nothing compares to NULL.
func (s *Store) ShadowRows(ctx context.Context, identityID string, since time.Time) ([]ShadowRow, error) {
	var out []ShadowRow
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		rows, err := q.ListPIMWatchShadowRows(ctx, sqlc.ListPIMWatchShadowRowsParams{
			IdentityID: id, Since: pgtype.Timestamptz{Time: since.UTC(), Valid: true},
		})
		if err != nil {
			return err
		}
		out = make([]ShadowRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, ShadowRow{
				Tier: Tier(r.Tier), LadderTier: Tier(r.LadderTier.String), Centroid: Tier(r.CentroidTier.String),
				SenderMemory: Tier(r.SenderMemoryTier.String), ModelTier: Tier(r.ModelTier.String), By: DecidedBy(r.DecidedBy), LadderBy: DecidedBy(r.LadderBy.String),
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

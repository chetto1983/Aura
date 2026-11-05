package pimwatch

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// SeedLabelDescriptions gives the labels an identity already had the default descriptions, once in
// its life. It fills only a label named exactly like a default, whatever the language, whose
// description is empty, so what a person wrote stays. It returns false when an earlier call already
// claimed the seed.
func (s *Store) SeedLabelDescriptions(ctx context.Context, identityID string, defaults []Label) (bool, error) {
	var claimed bool
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		if err := q.EnsurePIMWatchSettings(ctx, id); err != nil {
			return err
		}
		n, err := q.ClaimPIMWatchLabelDescriptions(ctx, id)
		if err != nil || n == 0 {
			return err
		}
		claimed = true
		for _, label := range defaults {
			if err := q.FillPIMWatchLabelDescription(ctx, sqlc.FillPIMWatchLabelDescriptionParams{
				IdentityID: id, Name: label.Name, Description: label.Description,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("seed pim watch label descriptions: %w", err)
	}
	return claimed, nil
}

// PatchLabel changes the fields of a label that the patch names and keeps the others. It reads the
// label under a row lock, so two patches never overwrite each other's fields, and it checks only
// what the patch sends: a label that already carries a reserved name can still get a description.
func (s *Store) PatchLabel(ctx context.Context, identityID, labelID string, patch LabelPatch) (Label, error) {
	lid, err := db.ParseUUID("label_id", labelID)
	if err != nil {
		return Label{}, fmt.Errorf("update label: %w", ErrNotFound)
	}
	var out Label
	err = s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		row, err := q.GetPIMWatchLabelForUpdate(ctx, sqlc.GetPIMWatchLabelForUpdateParams{IdentityID: id, ID: lid})
		if err != nil {
			return err
		}
		in, err := patch.apply(labelFromRow(row))
		if err != nil {
			return err
		}
		updated, err := q.UpdatePIMWatchLabel(ctx, sqlc.UpdatePIMWatchLabelParams{
			IdentityID: id, ID: lid, Name: in.Name, Tier: optionalText(string(in.Tier)),
			Description: in.Description, Discard: in.Discard,
		})
		if err != nil {
			return err
		}
		out = labelFromRow(updated)
		return nil
	})
	if err != nil {
		return Label{}, labelError("update", err)
	}
	return out, nil
}

// apply checks each field the patch sends and lays it over the stored label.
func (p LabelPatch) apply(stored Label) (LabelInput, error) {
	in := LabelInput{Name: stored.Name, Tier: stored.Tier, Description: stored.Description, Discard: stored.Discard}
	if p.Name != nil {
		if err := checkLabelName(*p.Name); err != nil {
			return LabelInput{}, err
		}
		in.Name = *p.Name
	}
	if p.Tier != nil {
		if err := checkLabelTier(*p.Tier); err != nil {
			return LabelInput{}, err
		}
		in.Tier = *p.Tier
	}
	if p.Description != nil {
		if err := checkLabelDescription(*p.Description); err != nil {
			return LabelInput{}, err
		}
		in.Description = *p.Description
	}
	if p.Discard != nil {
		in.Discard = *p.Discard
	}
	return in, nil
}

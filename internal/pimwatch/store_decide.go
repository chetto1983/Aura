package pimwatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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

// DiscardedOfDay counts an account's discarded verdicts decided in [since, until): what the day's
// list leaves out.
func (s *Store) DiscardedOfDay(ctx context.Context, identityID, accountID string, since, until time.Time) (int, error) {
	var n int64
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		var err error
		n, err = q.CountPIMWatchDiscardedOfDay(ctx, sqlc.CountPIMWatchDiscardedOfDayParams{
			IdentityID: id, AccountID: accountID, Since: timestamp(since), Until: timestamp(until),
		})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("count discarded pim watch decisions: %w", err)
	}
	return int(n), nil
}

// DiscardedInWindow counts the identity's discarded verdicts decided in (since, until]: what a
// digest leaves out.
func (s *Store) DiscardedInWindow(ctx context.Context, identityID string, since, until time.Time) (int, error) {
	var n int64
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		var err error
		n, err = q.CountPIMWatchDiscardedInWindow(ctx, sqlc.CountPIMWatchDiscardedInWindowParams{
			IdentityID: id, Since: timestamp(since), Until: timestamp(until),
		})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("count discarded pim watch verdicts: %w", err)
	}
	return int(n), nil
}

// AccountSettings is what the person decided for an account and what the model last did to it.
// DecideError is the latest failure's class, with no text, and DecideFailedAt the first failure's
// time; the next answer clears both.
type AccountSettings struct {
	CloudDecide    bool
	DecideError    string
	DecideFailedAt time.Time
}

func accountSettingsFromRow(r sqlc.AuraPimWatchAccounts) AccountSettings {
	return AccountSettings{CloudDecide: r.CloudDecide, DecideError: r.DecideError, DecideFailedAt: r.DecideFailedAt.Time}
}

// AccountSettings lists the identity's accounts that hold a setting, by account id. An account
// with no row has the defaults: no consent, no failure.
func (s *Store) AccountSettings(ctx context.Context, identityID string) (map[string]AccountSettings, error) {
	var rows []sqlc.AuraPimWatchAccounts
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		var err error
		rows, err = q.ListPIMWatchAccounts(ctx, id)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list pim watch accounts: %w", err)
	}
	out := make(map[string]AccountSettings, len(rows))
	for _, row := range rows {
		out[row.AccountID] = accountSettingsFromRow(row)
	}
	return out, nil
}

// CloudDecide reports whether the person let the hosted decision model read this account.
func (s *Store) CloudDecide(ctx context.Context, identityID, accountID string) (bool, error) {
	var on bool
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		row, err := q.GetPIMWatchAccount(ctx, sqlc.GetPIMWatchAccountParams{IdentityID: id, AccountID: accountID})
		on = row.CloudDecide
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read pim watch consent: %w", err)
	}
	return on, nil
}

// RecordDecideFailure keeps the decision model's last failure on an account for the cockpit: a class
// from decide.ErrorClass, never the answer's text. An empty class clears it.
func (s *Store) RecordDecideFailure(ctx context.Context, identityID, accountID, class string) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		if class == "" {
			return q.ClearPIMWatchDecideFailure(ctx, sqlc.ClearPIMWatchDecideFailureParams{IdentityID: id, AccountID: accountID})
		}
		return q.RecordPIMWatchDecideFailure(ctx, sqlc.RecordPIMWatchDecideFailureParams{IdentityID: id, AccountID: accountID, DecideError: class})
	})
	if err != nil {
		return fmt.Errorf("record pim watch decide failure: %w", err)
	}
	return nil
}

// SetCloudDecide records the person's consent for an account. It leaves the model's last failure
// as it was.
func (s *Store) SetCloudDecide(ctx context.Context, identityID, accountID string, on bool) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.SetPIMWatchCloudDecide(ctx, sqlc.SetPIMWatchCloudDecideParams{IdentityID: id, AccountID: accountID, CloudDecide: on})
	})
	if err != nil {
		return fmt.Errorf("set pim watch consent: %w", err)
	}
	return nil
}

// ModelRow is one mail the decision model's report counts: what stands now, the ladder's answer and
// the model's. ModelTier is the tier its label gave, "" when it gave none.
type ModelRow struct {
	Tier, LadderTier, ModelTier                         Tier
	By, LadderBy                                        DecidedBy
	LabelID, ModelLabelID, ModelName                    string
	ModelAsked, ModelDiscarded, ModelSkipped, Discarded bool
	Notified                                            bool
	ModelOptions                                        int
}

// ModelRows reads the mail decided since then that carries a ladder answer. A zero since reads them
// all, as ShadowRows does.
func (s *Store) ModelRows(ctx context.Context, identityID string, since time.Time) ([]ModelRow, error) {
	var out []ModelRow
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		rows, err := q.ListPIMWatchModelRows(ctx, sqlc.ListPIMWatchModelRowsParams{
			IdentityID: id, Since: pgtype.Timestamptz{Time: since.UTC(), Valid: true},
		})
		if err != nil {
			return err
		}
		out = make([]ModelRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, ModelRow{
				Tier: Tier(r.Tier), LadderTier: Tier(r.LadderTier.String), ModelTier: Tier(r.ModelTier.String),
				By: DecidedBy(r.DecidedBy), LadderBy: DecidedBy(r.LadderBy.String),
				LabelID: r.LabelID.String(), ModelLabelID: r.ModelLabelID.String(), ModelName: r.ModelName.String,
				ModelAsked: r.ModelProbability.Valid, ModelDiscarded: r.ModelDiscarded, ModelSkipped: r.ModelSkipped,
				Discarded: r.Discarded, Notified: r.Notified, ModelOptions: int(r.ModelOptions.Int16),
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read pim watch model rows: %w", err)
	}
	return out, nil
}

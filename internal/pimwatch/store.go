package pimwatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

var (
	// ErrNotFound is a row this identity cannot see, whether it does not exist or belongs to
	// someone else: RLS makes the two indistinguishable, on purpose.
	ErrNotFound = errors.New("not found")
	// ErrInvalid matches every refusal of a caller's input, each one an *InvalidError.
	ErrInvalid = errors.New("invalid mail watch input")
)

// InvalidError refuses a caller's input twice over: Code is fixed, for the cockpit to translate,
// and Message names what to fix, for the model reading the tool's result.
type InvalidError struct {
	Code    string
	Message string
}

func (e *InvalidError) Error() string { return ErrInvalid.Error() + ": " + e.Message }

// Is makes every refusal an ErrInvalid.
func (e *InvalidError) Is(target error) bool { return target == ErrInvalid }

func invalid(code, message string) error { return &InvalidError{Code: code, Message: message} }

func invalidTier() error { return invalid("invalid_tier", "the tier is now, today or later") }

// Store is the watch's state in Postgres. Every method binds the identity before it touches a
// table, because all six carry the fail-closed RLS pair: another identity's verdict is invisible.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store over an open pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Settings are an identity's watch settings, read at use and never snapshotted at boot.
type Settings struct {
	DigestHour           int
	DailyAlertCap        int
	IntervalMinutes      int
	LastDigestAt         time.Time
	VerdictRetentionDays int
	CreatedAt            time.Time
}

func (s *Store) tx(ctx context.Context, identityID string, fn func(*sqlc.Queries, pgtype.UUID) error) error {
	id, err := db.ParseUUID("identity_id", identityID)
	if err != nil {
		return err
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error { return fn(q, id) })
}

// list runs one listing query under the identity and converts its rows.
func list[R, T any](ctx context.Context, s *Store, identityID string, query func(*sqlc.Queries, pgtype.UUID) ([]R, error), convert func(R) T) ([]T, error) {
	var rows []R
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		var err error
		rows, err = query(q, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert(r))
	}
	return out, nil
}

// SeedLabels writes the default labels once in an identity's life. A person who removes them all
// does not get them back on the next enable, and a name they already use stays theirs.
func (s *Store) SeedLabels(ctx context.Context, identityID string, labels []Label) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		if err := q.EnsurePIMWatchSettings(ctx, id); err != nil {
			return err
		}
		claimed, err := q.ClaimPIMWatchLabelSeed(ctx, id)
		if err != nil || claimed == 0 {
			return err
		}
		for _, label := range labels {
			if err := q.SeedPIMWatchLabel(ctx, sqlc.SeedPIMWatchLabelParams{
				IdentityID: id, Name: label.Name, Tier: optionalText(string(label.Tier)), Position: int32(label.Position), //nolint:gosec // G115: a list position.
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("seed pim watch labels: %w", err)
	}
	return nil
}

// Settings reads the identity's settings, writing the defaults on the first read.
func (s *Store) Settings(ctx context.Context, identityID string) (Settings, error) {
	var out Settings
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		if err := q.EnsurePIMWatchSettings(ctx, id); err != nil {
			return err
		}
		row, err := q.GetPIMWatchSettings(ctx, id)
		if err != nil {
			return err
		}
		out = settingsFromRow(row)
		return nil
	})
	if err != nil {
		return Settings{}, fmt.Errorf("read pim watch settings: %w", err)
	}
	return out, nil
}

// SaveSettings changes the digest hour and the daily alert cap.
func (s *Store) SaveSettings(ctx context.Context, identityID string, digestHour, dailyCap int) (Settings, error) {
	if digestHour < 0 || digestHour > 23 {
		return Settings{}, invalid("invalid_digest_hour", "the digest hour is 0 to 23")
	}
	if dailyCap < 0 || dailyCap > 100 {
		return Settings{}, invalid("invalid_alert_cap", "the daily alert cap is 0 to 100")
	}
	var out Settings
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		if err := q.EnsurePIMWatchSettings(ctx, id); err != nil {
			return err
		}
		row, err := q.UpdatePIMWatchSettings(ctx, sqlc.UpdatePIMWatchSettingsParams{
			IdentityID: id, DigestHour: int16(digestHour), DailyAlertCap: int32(dailyCap), //nolint:gosec // G115: bounded above.
		})
		if err != nil {
			return err
		}
		out = settingsFromRow(row)
		return nil
	})
	if err != nil {
		return Settings{}, fmt.Errorf("save pim watch settings: %w", err)
	}
	return out, nil
}

// SetLastDigest moves the digest's window.
func (s *Store) SetLastDigest(ctx context.Context, identityID string, at time.Time) error {
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.SetPIMWatchLastDigest(ctx, sqlc.SetPIMWatchLastDigestParams{IdentityID: id, LastDigestAt: timestamp(at)})
	})
	if err != nil {
		return fmt.Errorf("set last digest: %w", err)
	}
	return nil
}

// Book reads the identity's rules and labels.
func (s *Store) Book(ctx context.Context, identityID string) (Book, error) {
	rules, err := s.Rules(ctx, identityID)
	if err != nil {
		return Book{}, err
	}
	labels, err := s.Labels(ctx, identityID)
	if err != nil {
		return Book{}, err
	}
	return NewBook(rules, labels), nil
}

// Labels lists the identity's labels in display order.
func (s *Store) Labels(ctx context.Context, identityID string) ([]Label, error) {
	labels, err := list(ctx, s, identityID, func(q *sqlc.Queries, id pgtype.UUID) ([]sqlc.AuraPimWatchLabels, error) {
		return q.ListPIMWatchLabels(ctx, id)
	}, labelFromRow)
	if err != nil {
		return nil, fmt.Errorf("list pim watch labels: %w", err)
	}
	return labels, nil
}

// AddLabel adds a label at the end of the list.
func (s *Store) AddLabel(ctx context.Context, identityID, name string, tier Tier, position int) (Label, error) {
	var out Label
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		row, err := q.InsertPIMWatchLabel(ctx, sqlc.InsertPIMWatchLabelParams{
			IdentityID: id, Name: name, Tier: optionalText(string(tier)), Position: int32(position), //nolint:gosec // G115: a list position.
		})
		if err != nil {
			return err
		}
		out = labelFromRow(row)
		return nil
	})
	if err != nil {
		return Label{}, labelError("add", err)
	}
	return out, nil
}

// UpdateLabel renames a label and sets its tier. An empty tier clears it.
func (s *Store) UpdateLabel(ctx context.Context, identityID, labelID, name string, tier Tier) (Label, error) {
	lid, err := db.ParseUUID("label_id", labelID)
	if err != nil {
		return Label{}, fmt.Errorf("update label: %w", ErrNotFound)
	}
	var out Label
	err = s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		row, err := q.UpdatePIMWatchLabel(ctx, sqlc.UpdatePIMWatchLabelParams{
			IdentityID: id, ID: lid, Name: name, Tier: optionalText(string(tier)),
		})
		if err != nil {
			return err
		}
		out = labelFromRow(row)
		return nil
	})
	if err != nil {
		return Label{}, labelError("update", err)
	}
	return out, nil
}

// RemoveLabel removes a label. Its verdicts keep their tier and read as unsorted, and a rule that
// only gave this label goes with it, since it would say nothing.
func (s *Store) RemoveLabel(ctx context.Context, identityID, labelID string) error {
	lid, err := db.ParseUUID("label_id", labelID)
	if err != nil {
		return fmt.Errorf("remove label: %w", ErrNotFound)
	}
	return s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		if err := q.DeletePIMWatchRulesOfLabel(ctx, sqlc.DeletePIMWatchRulesOfLabelParams{IdentityID: id, LabelID: lid}); err != nil {
			return fmt.Errorf("remove label rules: %w", err)
		}
		n, err := q.DeletePIMWatchLabel(ctx, sqlc.DeletePIMWatchLabelParams{IdentityID: id, ID: lid})
		if err != nil {
			return fmt.Errorf("remove label: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("remove label: %w", ErrNotFound)
		}
		return nil
	})
}

func labelError(verb string, err error) error {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%s label: %w", verb, ErrNotFound)
	case db.IsUniqueViolation(err):
		return invalid("label_exists", "a label with that name already exists")
	default:
		return fmt.Errorf("%s label: %w", verb, err)
	}
}

// Rules lists the identity's rules, oldest first.
func (s *Store) Rules(ctx context.Context, identityID string) ([]Rule, error) {
	rules, err := list(ctx, s, identityID, func(q *sqlc.Queries, id pgtype.UUID) ([]sqlc.AuraPimWatchRules, error) {
		return q.ListPIMWatchRules(ctx, id)
	}, ruleFromRow)
	if err != nil {
		return nil, fmt.Errorf("list pim watch rules: %w", err)
	}
	return rules, nil
}

// PutRule writes the person's rule for a key, replacing what the key said before.
func (s *Store) PutRule(ctx context.Context, identityID string, rule Rule) (Rule, error) {
	var out Rule
	err := s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		row, err := q.UpsertPIMWatchRule(ctx, sqlc.UpsertPIMWatchRuleParams{
			IdentityID: id, Key: rule.Key, KeyKind: string(rule.Kind), Tier: optionalText(string(rule.Tier)),
			LabelID: optionalUUID(rule.LabelID), Source: sourcePerson,
		})
		if err != nil {
			return err
		}
		out = ruleFromRow(row)
		return nil
	})
	if err != nil {
		return Rule{}, fmt.Errorf("put pim watch rule: %w", err)
	}
	return out, nil
}

// RemoveRule removes a rule.
func (s *Store) RemoveRule(ctx context.Context, identityID, ruleID string) error {
	rid, err := db.ParseUUID("rule_id", ruleID)
	if err != nil {
		return fmt.Errorf("remove rule: %w", ErrNotFound)
	}
	return s.tx(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		n, err := q.DeletePIMWatchRule(ctx, sqlc.DeletePIMWatchRuleParams{IdentityID: id, ID: rid})
		if err != nil {
			return fmt.Errorf("remove rule: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("remove rule: %w", ErrNotFound)
		}
		return nil
	})
}

// Where a rule came from.
const (
	sourcePerson     = "person"
	sourceCorrection = "correction"
)

func settingsFromRow(r sqlc.AuraPimWatchSettings) Settings {
	return Settings{
		DigestHour: int(r.DigestHour), DailyAlertCap: int(r.DailyAlertCap), IntervalMinutes: int(r.IntervalMinutes),
		LastDigestAt: r.LastDigestAt.Time, VerdictRetentionDays: int(r.VerdictRetentionDays), CreatedAt: r.CreatedAt.Time,
	}
}

func labelFromRow(r sqlc.AuraPimWatchLabels) Label {
	return Label{ID: r.ID.String(), Name: r.Name, Tier: Tier(r.Tier.String), Position: int(r.Position)}
}

func ruleFromRow(r sqlc.AuraPimWatchRules) Rule {
	return Rule{
		ID: r.ID.String(), Key: r.Key, Kind: KeyKind(r.KeyKind), Tier: Tier(r.Tier.String),
		LabelID: r.LabelID.String(), Source: r.Source, CreatedAt: r.CreatedAt.Time,
	}
}

func optionalText(v string) pgtype.Text { return pgtype.Text{String: v, Valid: v != ""} }

// optionalUUID is NULL for an empty id. A malformed one is NULL too: every id here came from a
// row or was checked against one.
func optionalUUID(v string) pgtype.UUID {
	id, err := db.ParseUUID("id", v)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}

func timestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC(), Valid: !t.IsZero()}
}

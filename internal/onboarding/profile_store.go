package onboarding

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProfileStore holds the operator profile in Postgres (migration 0097), where the runtime
// can read it.
//
// Until 2026-08-16 the only copy lived in the agent-memory graph, which made the
// deterministic profile compete with everything the agent had actually learned -- a recall
// for "what did we decide about the invoices" ranked against "role: programmatore" -- and
// put the operator's timezone in a per-identity ArcadeDB database that nothing on the turn
// path opens. So the clock stayed UTC and the model was left to convert it, which it did
// wrong.
//
// Settings, not memories. The graph keeps what the agent LEARNS.
type ProfileStore struct {
	pool *pgxpool.Pool
}

// NewProfileStore returns nil for a nil pool so a caller can wire it unconditionally and a
// deployment without Postgres simply has no profile.
func NewProfileStore(pool *pgxpool.Pool) *ProfileStore {
	if pool == nil {
		return nil
	}
	return &ProfileStore{pool: pool}
}

// Save writes the answers for one identity, leaving untouched every field the caller left
// empty (the upsert's COALESCE/NULLIF). A partial form is a partial update, never an erase.
//
// This is the ONBOARDING write. The editor uses Replace: it renders every field, so there
// an empty value is a deletion and merging would make one impossible.
func (s *ProfileStore) Save(ctx context.Context, identityID string, a Answers) error {
	return s.write(ctx, identityID, a, false)
}

// Replace overwrites the whole profile — the editor's write, where a cleared field means
// the operator cleared it.
func (s *ProfileStore) Replace(ctx context.Context, identityID string, a Answers) error {
	return s.write(ctx, identityID, a, true)
}

func (s *ProfileStore) write(ctx context.Context, identityID string, a Answers, replace bool) error {
	if s == nil || s.pool == nil {
		return nil
	}
	id, err := parseIdentity(identityID)
	if err != nil {
		return err
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		// The two statements take the same parameter struct by construction (sqlc generates
		// one per query over the same argument list), so the shape is written once here.
		params := sqlc.UpsertIdentityProfileParams{
			IdentityID:          id,
			DisplayName:         a.Name,
			Role:                a.Role,
			Company:             a.Company,
			Location:            a.Location,
			Timezone:            a.Timezone,
			Lang:                a.Lang,
			TonePreference:      a.TonePreference,
			ResponseLength:      a.ResponseLength,
			CustomInstructions:  a.CustomInstructions,
			VoiceMode:           optionalBool(a.VoiceMode),
			CanProactiveMessage: optionalBool(a.CanProactiveMessage),
			// list() on every array: pgx sends a nil Go slice as SQL NULL, and these columns
			// are NOT NULL, so an Answers that simply did not mention expertise was rejected
			// outright -- which is every onboarding seed, since that form collects six scalars
			// and no lists at all. Caught by the db_integration tier, never by a unit test.
			Expertise: list(a.Expertise),
			Stack:     list(a.Stack),
			Projects:  list(a.Projects),
			Goals:     list(a.Goals),
			Interests: list(a.Interests),
			People:    list(a.People),
			Vetoes:    list(a.Vetoes),
		}
		var err error
		if replace {
			_, err = q.ReplaceIdentityProfile(ctx, sqlc.ReplaceIdentityProfileParams(params))
		} else {
			_, err = q.UpsertIdentityProfile(ctx, params)
		}
		if err != nil {
			return fmt.Errorf("save identity profile %s: %w", identityID, err)
		}
		return nil
	})
}

// StoreConfirmed saves the answers and opens the gate, in that order: the gate is what
// stops the cockpit asking again, so it must never open over a failed write.
func (s *ProfileStore) StoreConfirmed(ctx context.Context, identityID string, a Answers) error {
	if s == nil || s.pool == nil {
		return nil
	}
	if identityID == "" {
		return fmt.Errorf("onboarding: store confirmed with empty identity")
	}
	if err := s.Save(ctx, identityID, a); err != nil {
		return err
	}
	return s.mark(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.MarkOnboardingCompleted(ctx, id)
	})
}

// StoreSkipped records the decline. It writes no answers: an operator who skipped the form
// has none, and a row of empty strings would be indistinguishable from one they filled in
// blank.
func (s *ProfileStore) StoreSkipped(ctx context.Context, identityID string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	if identityID == "" {
		return fmt.Errorf("onboarding: store skipped with empty identity")
	}
	return s.mark(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.MarkOnboardingSkipped(ctx, id)
	})
}

// MarkNudged records that a channel has pointed this operator at the form.
func (s *ProfileStore) MarkNudged(ctx context.Context, identityID string) error {
	if s == nil || s.pool == nil || identityID == "" {
		return nil
	}
	return s.mark(ctx, identityID, func(q *sqlc.Queries, id pgtype.UUID) error {
		return q.MarkOnboardingNudged(ctx, id)
	})
}

// Status reads the gate. A missing row is the honest zero value: never asked.
func (s *ProfileStore) Status(ctx context.Context, identityID string) (OnboardingState, error) {
	if s == nil || s.pool == nil || identityID == "" {
		return OnboardingState{}, nil
	}
	id, err := parseIdentity(identityID)
	if err != nil {
		return OnboardingState{}, err
	}
	var st OnboardingState
	if err := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		row, qErr := q.GetOnboardingState(ctx, id)
		if qErr != nil {
			if errors.Is(qErr, pgx.ErrNoRows) {
				return nil
			}
			return qErr
		}
		st = OnboardingState{
			Completed: row.CompletedAt.Valid,
			Skipped:   row.SkippedAt.Valid,
			Nudged:    row.SeedNudgedAt.Valid,
		}
		return nil
	}); err != nil {
		return OnboardingState{}, fmt.Errorf("onboarding status %s: %w", identityID, err)
	}
	return st, nil
}

func (s *ProfileStore) mark(ctx context.Context, identityID string, write func(*sqlc.Queries, pgtype.UUID) error) error {
	id, err := parseIdentity(identityID)
	if err != nil {
		return err
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		if err := write(q, id); err != nil {
			return fmt.Errorf("onboarding gate %s: %w", identityID, err)
		}
		return nil
	})
}

// Timezone is the IANA zone for one identity, empty when unknown.
//
// It never fails the caller: a clock is a display concern, and a turn that cannot read the
// zone should render UTC rather than refuse to answer.
func (s *ProfileStore) Timezone(ctx context.Context, identityID string) string {
	if s == nil || s.pool == nil {
		return ""
	}
	id, err := parseIdentity(identityID)
	if err != nil {
		return ""
	}
	var zone string
	_ = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		found, qErr := q.GetIdentityTimezone(ctx, id)
		if qErr != nil {
			if errors.Is(qErr, pgx.ErrNoRows) {
				return nil // no profile yet: not an error, just no zone
			}
			return qErr
		}
		zone = found
		return nil
	})
	return zone
}

// Load returns the stored profile, and false when the identity has none.
func (s *ProfileStore) Load(ctx context.Context, identityID string) (Answers, bool, error) {
	if s == nil || s.pool == nil {
		return Answers{}, false, nil
	}
	id, err := parseIdentity(identityID)
	if err != nil {
		return Answers{}, false, err
	}
	var (
		row   sqlc.AuraIdentityProfiles
		found bool
	)
	if err := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		loaded, qErr := q.GetIdentityProfile(ctx, id)
		if qErr != nil {
			if errors.Is(qErr, pgx.ErrNoRows) {
				return nil
			}
			return qErr
		}
		row, found = loaded, true
		return nil
	}); err != nil {
		return Answers{}, false, fmt.Errorf("load identity profile %s: %w", identityID, err)
	}
	if !found {
		return Answers{}, false, nil
	}
	return Answers{
		Name: row.DisplayName, Role: row.Role, Company: row.Company, Location: row.Location,
		Timezone: row.Timezone, Lang: row.Lang, TonePreference: row.TonePreference,
		ResponseLength: row.ResponseLength, CustomInstructions: row.CustomInstructions,
		VoiceMode: boolOrNil(row.VoiceMode), CanProactiveMessage: boolOrNil(row.CanProactiveMessage),
		Expertise: row.Expertise, Stack: row.Stack, Projects: row.Projects,
		Goals: row.Goals, Interests: row.Interests, People: row.People, Vetoes: row.Vetoes,
	}, true, nil
}

// list keeps a nil slice off the wire: the array columns are NOT NULL, and "not mentioned"
// means an empty list, not an absent one.
func list(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func parseIdentity(identityID string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(identityID); err != nil {
		return pgtype.UUID{}, fmt.Errorf("identity profile: invalid identity id %q: %w", identityID, err)
	}
	return id, nil
}

// optionalBool keeps "not answered" distinct from "no": the form leaves these unset until
// the operator chooses, and a NULL says so where false would lie.
func optionalBool(v *bool) pgtype.Bool {
	if v == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *v, Valid: true}
}

func boolOrNil(v pgtype.Bool) *bool {
	if !v.Valid {
		return nil
	}
	out := v.Bool
	return &out
}

// ProfileBlock renders this identity's profile for messages[1], or "" when there is none.
//
// It is on the store so *ProfileStore satisfies the runner's ProfileProvider directly: an
// adapter between two types that already agree is a file nobody reads.
//
// A read failure costs the block, never the turn. The profile is context, and a turn that
// refuses to run because it could not decorate itself would be a worse failure than one
// that runs without knowing the operator's preferred tone.
func (s *ProfileStore) ProfileBlock(ctx context.Context, identityID string) string {
	if s == nil {
		return ""
	}
	answers, ok, err := s.Load(ctx, identityID)
	if err != nil || !ok {
		return ""
	}
	return RenderProfileBlock(answers)
}

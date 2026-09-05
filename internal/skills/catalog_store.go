package skills

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// catalog_store.go is the Postgres half of a personal skill (migration 0118, amendment
// #214): who owns it, what it is called, whether it is always-on. The BODY is not here — it
// stays on disk under the owner's root, because a skill is docker-cp'd into a box at every
// create and resume and a row would have to be re-rendered into a tree on each one (D-214-1).
//
// Only PERSONAL skills are rows. The deployment's own skills stay a read-only overlay with
// no rows at all, the way migration 0101 keeps declared MCP recipes out of the registry
// table: an upgrade still updates them, and nothing has to migrate to receive them.
//
// Every method runs inside db.WithIdentityTx, so aura.skill_catalog's RLS is the isolation
// rather than a WHERE clause a future caller can forget (D-214-5).

// ErrCatalogUnknownSkill reports that an identity has no catalog row under that name — the
// answer both to "it does not exist" and to "it is not yours", which are the same answer
// from behind RLS and must not be distinguished for the caller.
var ErrCatalogUnknownSkill = errors.New("skills: no catalog row for that name")

// CatalogRow is one catalog entry as the rest of Aura reads it: plain Go types, no pgtype at
// the package boundary.
type CatalogRow struct {
	ID          string
	OwnerID     string
	Name        string
	Description string
	AlwaysApply bool
	ContentHash string
	UpdatedAt   time.Time
}

// CatalogStore is the per-identity skill catalog over an open pool. It holds the pool, not a
// *sqlc.Queries: the RLS carrier is the transaction, so there is no useful pool-level query.
type CatalogStore struct {
	pool *pgxpool.Pool
}

// NewCatalogStore builds a CatalogStore over an open pool.
func NewCatalogStore(pool *pgxpool.Pool) *CatalogStore {
	if pool == nil {
		return nil
	}
	return &CatalogStore{pool: pool}
}

// CatalogUpsert carries the fields one catalog row is written from.
type CatalogUpsert struct {
	OwnerID     string
	Name        string
	Description string
	AlwaysApply bool
	ContentHash string
}

// UpsertCatalogTx writes one catalog row inside a caller-owned transaction, mirroring
// InsertAuditTx. The Writer uses it so the row, the audit row and the skill becoming visible
// on disk are one atomic event: a catalog row for a skill that was never written is a lie
// the cockpit would render, and a written skill with no row is a skill its owner cannot
// share.
//
// The transaction MUST already carry app.current_identity (db.WithIdentityTx), or the RLS
// floor rejects the insert — which is the intended failure, not a case to work around.
func UpsertCatalogTx(ctx context.Context, q *sqlc.Queries, in CatalogUpsert) (CatalogRow, error) {
	owner, err := db.ParseUUID("owner identity", in.OwnerID)
	if err != nil {
		return CatalogRow{}, fmt.Errorf("skill catalog upsert %q: %w", in.Name, err)
	}
	row, err := q.UpsertSkillCatalog(ctx, sqlc.UpsertSkillCatalogParams{
		OwnerIdentityID: owner,
		Name:            in.Name,
		Description:     db.PostgresTextSafe(in.Description),
		AlwaysApply:     in.AlwaysApply,
		ContentHash:     in.ContentHash,
	})
	if err != nil {
		return CatalogRow{}, fmt.Errorf("skill catalog upsert %q (tx): %w", in.Name, err)
	}
	return catalogRowFrom(row), nil
}

// DeleteCatalogTx removes one catalog row inside a caller-owned transaction. A missing row
// is not an error: delete is the one verb that must stay idempotent, because a skill written
// before this migration existed has no row and its removal must still succeed.
//
// The row's grants go with it — the AFTER DELETE trigger of migration 0118 collects them, so
// a revoked-by-deletion share cannot outlive the thing it shared.
func DeleteCatalogTx(ctx context.Context, q *sqlc.Queries, ownerID, name string) error {
	owner, err := db.ParseUUID("owner identity", ownerID)
	if err != nil {
		return fmt.Errorf("skill catalog delete %q: %w", name, err)
	}
	if _, err := q.DeleteSkillCatalog(ctx, sqlc.DeleteSkillCatalogParams{OwnerIdentityID: owner, Name: name}); err != nil {
		return fmt.Errorf("skill catalog delete %q (tx): %w", name, err)
	}
	return nil
}

// ListOwned returns the catalog rows one identity owns, name-ordered.
func (s *CatalogStore) ListOwned(ctx context.Context, ownerID string) ([]CatalogRow, error) {
	owner, err := db.ParseUUID("owner identity", ownerID)
	if err != nil {
		return nil, fmt.Errorf("skill catalog list: %w", err)
	}
	return s.listRows(ctx, ownerID, "skill catalog list", func(q *sqlc.Queries) ([]sqlc.AuraSkillCatalog, error) {
		return q.ListSkillCatalogForOwner(ctx, owner)
	})
}

// ListAlwaysApply returns the identity's always-on skills through the partial index
// skill_catalog_always_apply_idx (acceptance criterion 7).
//
// NOT WIRED YET, and saying so here rather than leaving the comment to imply otherwise. The
// always-on block is still rendered from the FILESYSTEM loader (cmd/aura/skills_roots.go),
// which already scopes per identity and already knows the always flag from the frontmatter;
// the row exists so the block can one day include a skill somebody SHARED, whose body lives
// under its owner's root and which no filesystem scan of the reader's roots can find. That
// step is the same unresolved design as the rest of the shared-in read (see ListByIDs), so
// this method has a test and an EXPLAIN proof and no production caller.
func (s *CatalogStore) ListAlwaysApply(ctx context.Context, ownerID string) ([]CatalogRow, error) {
	owner, err := db.ParseUUID("owner identity", ownerID)
	if err != nil {
		return nil, fmt.Errorf("skill catalog always-apply: %w", err)
	}
	return s.listRows(ctx, ownerID, "skill catalog always-apply", func(q *sqlc.Queries) ([]sqlc.AuraSkillCatalog, error) {
		return q.ListAlwaysApplySkills(ctx, owner)
	})
}

// ListByIDs resolves catalog rows by id for the reader identity — the second half of the
// two-query share read (the ids come from the ACL, never from user input). RLS lets a reader
// see a row that is not theirs ONLY when a grant names them or the row is public, so an id
// that was not really shared resolves to nothing rather than to somebody else's skill.
//
// NOT WIRED YET (amendment #214 acceptance criterion 5 is open). A row is not a skill: to
// show a shared skill in a listing, or to put it in the grantee's box, something has to read
// a BODY out of the OWNER's filesystem root — which neither the Loader (roots are the
// reader's) nor usersandbox.SourceResolver (func(identityID) []MaterializeSource: no ctx,
// no error, so it cannot query the ACL) can do today. Widening either seam is a design
// decision, not an implementation detail, so this half is deliberately left unconnected
// rather than half-connected.
func (s *CatalogStore) ListByIDs(ctx context.Context, readerID string, ids []string) ([]CatalogRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	parsed := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		u, err := db.ParseUUID("skill id", id)
		if err != nil {
			return nil, fmt.Errorf("skill catalog by ids: %w", err)
		}
		parsed = append(parsed, u)
	}
	return s.listRows(ctx, readerID, "skill catalog by ids", func(q *sqlc.Queries) ([]sqlc.AuraSkillCatalog, error) {
		return q.ListSkillCatalogByIDs(ctx, parsed)
	})
}

// listRows runs one catalog SELECT under identity's RLS and projects the result. The three
// List methods differed only in which query they called, and dupl was right that writing that
// difference out three times is the same code three times (CI run 1789). Keeping the identity
// and the message as parameters is what lets the three keep their own error prefixes, which
// is the only part a caller can actually see.
func (s *CatalogStore) listRows(
	ctx context.Context,
	identity, what string,
	query func(*sqlc.Queries) ([]sqlc.AuraSkillCatalog, error),
) ([]CatalogRow, error) {
	var out []CatalogRow
	if err := db.WithIdentityTx(ctx, s.pool, identity, func(q *sqlc.Queries) error {
		rows, qerr := query(q)
		if qerr != nil {
			return qerr
		}
		out = catalogRowsFrom(rows)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}

// ResolveID returns the catalog id of one of the identity's own skills, so a share can name
// a skill the way a person does — by name — while the ACL keys on an id that survives a
// rename. An unknown name is ErrCatalogUnknownSkill, which is also the answer when the skill
// exists but belongs to somebody else: from behind RLS those are the same fact, and telling
// them apart would leak the other person's library one probe at a time.
func (s *CatalogStore) ResolveID(ctx context.Context, ownerID, name string) (string, error) {
	rows, err := s.ListOwned(ctx, ownerID)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if r.Name == name {
			return r.ID, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrCatalogUnknownSkill, name)
}

func catalogRowsFrom(rows []sqlc.AuraSkillCatalog) []CatalogRow {
	out := make([]CatalogRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, catalogRowFrom(r))
	}
	return out
}

func catalogRowFrom(r sqlc.AuraSkillCatalog) CatalogRow {
	return CatalogRow{
		ID:          catalogUUID(r.ID),
		OwnerID:     catalogUUID(r.OwnerIdentityID),
		Name:        r.Name,
		Description: r.Description,
		AlwaysApply: r.AlwaysApply,
		ContentHash: r.ContentHash,
		UpdatedAt:   r.UpdatedAt.Time,
	}
}

// catalogUUID renders a nullable uuid column; a NULL becomes the empty string rather than
// the zero uuid, which would read as a real identity.
func catalogUUID(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}

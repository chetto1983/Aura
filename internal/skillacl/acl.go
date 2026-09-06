// Package skillacl is Aura's resource ACL: who may see a thing somebody else owns.
//
// It is generic on purpose (amendment #214, D-214-4). This slice shares exactly one kind of
// resource — a skill — but the per-identity MCP work behind it wants the same answer for
// servers, and a second bespoke sharing table is how a deployment ends up with two that
// disagree about what a grant means. So the shape is LibreChat's aclEntry
// (packages/data-schemas/src/schema/aclEntry.ts): principal_type × resource_type × a
// permission bitmask, one table for every shareable resource, read in two steps — "which
// ids may I see" here, then the domain query filtered on those ids.
//
// Principals are an IDENTITY or PUBLIC. Groups are admitted by the database enum and are
// deliberately NOT built here (D-214-4): "shared with the team" has no answer while Aura has
// no principal that is not one person, and a half-built group is worse than an absent one.
//
// The isolation is the database's, not this package's: aura.resource_acl carries the two RLS
// layers of migration 0118, and every method here goes through db.WithIdentityTx, which sets
// app.current_identity for the transaction. A caller with no identity therefore reads
// nothing and writes nothing — the failure mode is emptiness, never somebody else's rows.
package skillacl

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// ResourceType names the kind of thing a grant is about. The database CHECK admits only
// ResourceSkill today; widening it is one ALTER when the second consumer lands.
type ResourceType string

// ResourceSkill is the only resource type this slice shares.
const ResourceSkill ResourceType = "skill"

// Perm is the permission bitmask stored in perm_bits, mirroring LibreChat's PermissionBits
// so the two are readable side by side. VIEW is what sharing a skill means today; the rest
// exist so a later verb is a constant rather than a migration.
type Perm int32

// The permission bits. They are powers of two and combine with |.
const (
	PermView   Perm = 1
	PermEdit   Perm = 2
	PermDelete Perm = 4
	PermShare  Perm = 8
)

// permAll is the union of every defined bit — the validation bound, so a caller cannot store
// a bit no reader will ever test for.
const permAll = PermView | PermEdit | PermDelete | PermShare

// Valid reports whether p is a non-empty subset of the defined bits.
func (p Perm) Valid() bool { return p > 0 && p&^permAll == 0 }

// ErrInvalidPerm reports a permission mask that is empty or carries an undefined bit.
var ErrInvalidPerm = errors.New("skillacl: permission bits are empty or undefined")

// Store is the Postgres-backed ACL over an open pool. It holds the pool rather than a
// *sqlc.Queries because every statement must run inside an identity-scoped transaction:
// the RLS carrier is the transaction, so there is no meaningful pool-level query here.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store over an open pool.
func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("skillacl: a database pool is required")
	}
	return &Store{pool: pool}, nil
}

// GrantToIdentity gives one identity access to one resource, at perm. Re-granting the same
// pair replaces the bits, which is what "share it again, with edit this time" means.
//
// granter is both the RLS principal and the recorded granted_by. Both halves of "may you
// grant this" are the database's answer, not this code's: migration 0118's restrictive write
// policy admits an insert only when granted_by is the calling identity AND the resource is
// one that identity owns, so neither a grant forged in somebody else's name nor a re-share of
// a skill somebody shared WITH the caller can reach the table.
func (s *Store) GrantToIdentity(ctx context.Context, granter string, rt ResourceType, resourceID, grantee string, perm Perm) error {
	args, err := grantArgs(granter, rt, resourceID, perm)
	if err != nil {
		return err
	}
	principal, err := db.ParseUUID("grantee identity", grantee)
	if err != nil {
		return fmt.Errorf("skillacl: %w", err)
	}
	return db.WithIdentityTx(ctx, s.pool, granter, func(q *sqlc.Queries) error {
		return q.GrantResourceToIdentity(ctx, sqlc.GrantResourceToIdentityParams{
			ResourceType: string(rt),
			ResourceID:   args.resource,
			PrincipalID:  principal,
			PermBits:     int32(perm),
			GrantedBy:    args.granter,
		})
	})
}

// GrantPublic opens one resource to every identity in the deployment. It is a separate
// method rather than a nil grantee because "everyone" is a decision, and a nil that means
// everyone is the kind of default nobody re-reads before shipping.
func (s *Store) GrantPublic(ctx context.Context, granter string, rt ResourceType, resourceID string, perm Perm) error {
	args, err := grantArgs(granter, rt, resourceID, perm)
	if err != nil {
		return err
	}
	return db.WithIdentityTx(ctx, s.pool, granter, func(q *sqlc.Queries) error {
		return q.GrantResourcePublic(ctx, sqlc.GrantResourcePublicParams{
			ResourceType: string(rt),
			ResourceID:   args.resource,
			PermBits:     int32(perm),
			GrantedBy:    args.granter,
		})
	})
}

// RevokeFromIdentity withdraws one identity's access. It reports whether a grant was
// actually removed: a caller that says "revoked" without knowing cannot tell a real
// revocation from a no-op on a grant that was never there — or on one RLS filtered away.
func (s *Store) RevokeFromIdentity(ctx context.Context, granter string, rt ResourceType, resourceID, grantee string) (bool, error) {
	resource, err := db.ParseUUID("resource id", resourceID)
	if err != nil {
		return false, fmt.Errorf("skillacl: %w", err)
	}
	principal, err := db.ParseUUID("grantee identity", grantee)
	if err != nil {
		return false, fmt.Errorf("skillacl: %w", err)
	}
	var removed int64
	err = db.WithIdentityTx(ctx, s.pool, granter, func(q *sqlc.Queries) error {
		n, qerr := q.RevokeResourceFromIdentity(ctx, sqlc.RevokeResourceFromIdentityParams{
			ResourceType: string(rt),
			ResourceID:   resource,
			PrincipalID:  principal,
		})
		removed = n
		return qerr
	})
	return removed > 0, err
}

// RevokePublic closes a resource that was open to everyone.
func (s *Store) RevokePublic(ctx context.Context, granter string, rt ResourceType, resourceID string) (bool, error) {
	resource, err := db.ParseUUID("resource id", resourceID)
	if err != nil {
		return false, fmt.Errorf("skillacl: %w", err)
	}
	var removed int64
	err = db.WithIdentityTx(ctx, s.pool, granter, func(q *sqlc.Queries) error {
		n, qerr := q.RevokeResourcePublic(ctx, sqlc.RevokeResourcePublicParams{
			ResourceType: string(rt),
			ResourceID:   resource,
		})
		removed = n
		return qerr
	})
	return removed > 0, err
}

// AccessibleResourceIDs answers the grantee's question: which resources of this type may I
// see with at least these permission bits. It is the first of LibreChat's two queries; the
// domain query filtered on these ids is the second.
//
// A caller with no identity gets an empty list rather than an error, because that is the
// truthful answer — nothing is shared with nobody — and the RLS floor would return the same
// emptiness one layer down.
//
// It is the first step of skills.SharedReader, which joins it to the catalog and the layout
// to answer the question a turn actually asks — which BODIES may this reader load — and both
// consumers of that answer (the reader's Loader and the sources their box is filled from) go
// through it, so the listing and the box cannot disagree about what a grant means.
func (s *Store) AccessibleResourceIDs(ctx context.Context, identityID string, rt ResourceType, perm Perm) ([]string, error) {
	if !perm.Valid() {
		return nil, fmt.Errorf("%w: %d", ErrInvalidPerm, perm)
	}
	if identityID == "" {
		return nil, nil
	}
	principal, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return nil, fmt.Errorf("skillacl: %w", err)
	}
	var out []string
	err = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		rows, qerr := q.ListAccessibleResources(ctx, sqlc.ListAccessibleResourcesParams{
			ResourceType: string(rt),
			Want:         int32(perm),
			PrincipalID:  principal,
		})
		if qerr != nil {
			return qerr
		}
		out = uuidStrings(rows)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("skillacl: accessible %s resources: %w", rt, err)
	}
	return out, nil
}

// Grant is one standing grant on a resource, as an operator reads it.
type Grant struct {
	ResourceType  ResourceType
	ResourceID    string
	PrincipalType string
	PrincipalID   string // empty for a public grant
	Perm          Perm
	GrantedBy     string
}

// ListGrants returns every grant standing on one resource, for the operator asking "who can
// read this?" before deciding whether to revoke. RLS narrows it to the grants this caller
// may see, which for the owner is all of them.
func (s *Store) ListGrants(ctx context.Context, identityID string, rt ResourceType, resourceID string) ([]Grant, error) {
	resource, err := db.ParseUUID("resource id", resourceID)
	if err != nil {
		return nil, fmt.Errorf("skillacl: %w", err)
	}
	var out []Grant
	err = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		rows, qerr := q.ListResourceGrants(ctx, sqlc.ListResourceGrantsParams{
			ResourceType: string(rt),
			ResourceID:   resource,
		})
		if qerr != nil {
			return qerr
		}
		out = grantsFrom(rows)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("skillacl: list grants on %s %s: %w", rt, resourceID, err)
	}
	return out, nil
}

// grantsFrom projects the query rows onto the boundary type: plain strings, and an empty
// PrincipalID for the NULL a public grant carries rather than the zero uuid, which would read
// as a real identity in a listing and in a comparison.
func grantsFrom(rows []sqlc.AuraResourceAcl) []Grant {
	out := make([]Grant, 0, len(rows))
	for _, r := range rows {
		out = append(out, Grant{
			ResourceType:  ResourceType(r.ResourceType),
			ResourceID:    uuidString(r.ResourceID),
			PrincipalType: r.PrincipalType,
			PrincipalID:   uuidString(r.PrincipalID),
			Perm:          Perm(r.PermBits),
			GrantedBy:     uuidString(r.GrantedBy),
		})
	}
	return out
}

// grantIDs are the two uuids every grant write needs, parsed and validated once.
type grantIDs struct {
	granter  pgtype.UUID
	resource pgtype.UUID
}

// grantArgs validates the shared preconditions of the write paths: a real granter, a real
// resource, a known resource type and a permission mask made only of defined bits.
func grantArgs(granter string, rt ResourceType, resourceID string, perm Perm) (grantIDs, error) {
	if rt != ResourceSkill {
		return grantIDs{}, fmt.Errorf("skillacl: unsupported resource type %q", rt)
	}
	if !perm.Valid() {
		return grantIDs{}, fmt.Errorf("%w: %d", ErrInvalidPerm, perm)
	}
	granterID, err := db.ParseUUID("granter identity", granter)
	if err != nil {
		return grantIDs{}, fmt.Errorf("skillacl: %w", err)
	}
	resource, err := db.ParseUUID("resource id", resourceID)
	if err != nil {
		return grantIDs{}, fmt.Errorf("skillacl: %w", err)
	}
	return grantIDs{granter: granterID, resource: resource}, nil
}

// uuidStrings renders a column of uuids as plain strings, dropping the NULLs a LEFT-joined
// column could carry (this query cannot produce one, but the projection must not invent a
// zero uuid if one ever appears).
func uuidStrings(in []pgtype.UUID) []string {
	out := make([]string, 0, len(in))
	for _, id := range in {
		if s := uuidString(id); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// uuidString renders one nullable uuid; a NULL becomes the empty string, never a zero uuid
// that would read as a real principal.
func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}

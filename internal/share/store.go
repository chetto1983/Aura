// store.go persists aura.shared_links (migration 0040) over raw pgx — the PgAuditStore
// precedent (internal/agui/audit_store.go), NOT sqlc. No sqlc query is added for this
// table: hand-written SQL strings against a *pgxpool.Pool, `%w`-wrapped errors at every
// Query/Scan/rows.Err() site, `defer rows.Close()`, and `make([]T, 0, limit)` — copied
// verbatim from PgAuditStore.
//
// Two security properties live here, not in the handlers:
//
//  1. Every owner-scoped method routes through db.WithIdentityTxRaw(identityID), the raw-pgx
//     sibling of db.WithIdentityTx (internal/db/tx.go) added by this plan: sqlc.Queries.db is
//     unexported, so a raw-pgx store has no way to reach a pgx.Tx through db.WithIdentityTx's
//     *sqlc.Queries callback without adding a sqlc-generated query — which this store
//     deliberately does not carry. WithIdentityTxRaw sets the identical `SET LOCAL
//     app.current_identity` session var so migration 0032's owner-isolation RLS backstops a
//     forgotten WHERE clause, exactly as it does for conversations/paused_states.
//
//     NOTE (read this before "fixing" a missing RLS policy assumption): as of migration 0040,
//     aura.shared_links carries NO RLS policy — 0032 enables row-level security only on
//     aura.conversations, aura.paused_states, and aura.conversation_turns. Calling
//     WithIdentityTxRaw here is still correct and forward-compatible (it is the same choke
//     point every other owner-scoped web store already routes through, and it costs nothing),
//     but today it is inert for this table: the explicit `owner_identity_id = $N` predicate in
//     every query below is the ONLY enforcement layer, not a backstop-plus-predicate pair. A
//     future migration adding RLS to aura.shared_links would make the backstop live with zero
//     Go-side changes.
//
//  2. ResolveByToken and ResolveLiveByID are the two deliberate, documented exceptions: they
//     read on the plain pool, never through WithIdentityTxRaw. See each function's doc comment.
package share

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrShareNotFound is returned for a foreign or absent share id/token/conversation: the
// handler's uniform 404 (never 403, never a distinguishable body — a read hides foreign
// existence, mirroring conversations.ErrConversationNotFound / D-06).
var ErrShareNotFound = errors.New("share: not found")

// Link is the row projection of aura.shared_links. TokenHash is deliberately NOT a field
// here: Insert takes it as a separate parameter (so a Link value can never be threaded
// back out through a read path carrying a hash it has no business exposing), and no
// resolver in this file ever selects token_hash back out of the database.
type Link struct {
	ID              uuid.UUID
	OwnerIdentityID uuid.UUID
	ConversationID  uuid.UUID
	Tier            string
	SnapshotID      uuid.UUID
	SnapshotBucket  string
	FormatOptions   []byte // raw jsonb bytes, NOT NULL DEFAULT '{}' at the DB
	ExpiresAt       *time.Time
	RevokedAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Store is the shared_links persistence layer, raw pgx over the shared app pool.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a share Store over the shared Postgres pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// defaultFormatOptions mirrors the column's own `NOT NULL DEFAULT '{}'::jsonb` for the Go
// side of Insert, so a caller that never sets Link.FormatOptions still writes a valid
// (non-NULL) empty JSON object rather than relying on Postgres to catch an omitted column.
var defaultFormatOptions = []byte("{}")

// linkColumns is the single positional column list every read/RETURNING query in this file
// shares, keeping Link's field order and the SQL projection order from drifting apart.
const linkColumns = `id, owner_identity_id, conversation_id, tier, snapshot_id, snapshot_bucket, format_options, expires_at, revoked_at, created_at, updated_at`

// rowScanner is satisfied by both pgx.Row (QueryRow) and pgx.Rows (Query, via rows.Next()
// iteration) — scanLink works against either without duplicating the column list per call site.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanLink scans one linkColumns-shaped row into a Link.
func scanLink(row rowScanner) (Link, error) {
	var l Link
	err := row.Scan(
		&l.ID, &l.OwnerIdentityID, &l.ConversationID, &l.Tier,
		&l.SnapshotID, &l.SnapshotBucket, &l.FormatOptions,
		&l.ExpiresAt, &l.RevokedAt, &l.CreatedAt, &l.UpdatedAt,
	)
	return l, err
}

// parseUUID validates a caller-supplied id string before it ever reaches a query,
// mirroring conversations/store_helpers.go's parseUUID (that package returns pgtype.UUID
// for its sqlc callers; this raw-pgx store returns uuid.UUID directly, which pgx binds and
// scans natively — see internal/conversations/store_compaction.go for the same convention).
func parseUUID(field, s string) (uuid.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("%s %q: %w", field, s, err)
	}
	return u, nil
}

// Insert persists a new share link, owner-scoped to link.OwnerIdentityID. tokenHash is a
// separate parameter (never a Link field, see the Link doc comment) — NULL for an internal
// tier link (the migration 0040 tier-shape CHECK enforces this at the database level; Insert
// does not pre-validate it in Go, so the CHECK's rejection is observable end-to-end, e.g. by
// TestSharePublicRequiresExpiry).
func (s *Store) Insert(ctx context.Context, link Link, tokenHash []byte) error {
	fo := link.FormatOptions
	if len(fo) == 0 {
		fo = defaultFormatOptions
	}
	return db.WithIdentityTxRaw(ctx, s.pool, link.OwnerIdentityID.String(), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO aura.shared_links
				(id, owner_identity_id, conversation_id, tier, token_hash, snapshot_id, snapshot_bucket, format_options, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			link.ID, link.OwnerIdentityID, link.ConversationID, link.Tier, tokenHash,
			link.SnapshotID, link.SnapshotBucket, fo, link.ExpiresAt,
		)
		if err != nil {
			return fmt.Errorf("insert share link %s: %w", link.ID, err)
		}
		return nil
	})
}

// GetForIdentity fetches one share link scoped to identityID, mapping a miss (unknown id OR
// an id owned by another identity) to ErrShareNotFound — the handler's 404 (a read hides
// foreign existence, mirroring conversations.GetForIdentity).
func (s *Store) GetForIdentity(ctx context.Context, shareID, identityID string) (Link, error) {
	id, err := parseUUID("id", shareID)
	if err != nil {
		return Link{}, fmt.Errorf("get share: %w", err)
	}
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return Link{}, fmt.Errorf("get share: %w", err)
	}
	var link Link
	txErr := db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `SELECT `+linkColumns+` FROM aura.shared_links WHERE id = $1 AND owner_identity_id = $2`, id, owner)
		l, sErr := scanLink(row)
		if sErr != nil {
			if errors.Is(sErr, pgx.ErrNoRows) {
				return ErrShareNotFound
			}
			return fmt.Errorf("get share %s: %w", shareID, sErr)
		}
		link = l
		return nil
	})
	if txErr != nil {
		return Link{}, txErr
	}
	return link, nil
}

// ListForIdentity returns identityID's share links newest-first (the shared_links_owner_idx
// order: owner_identity_id, created_at DESC), paginated.
func (s *Store) ListForIdentity(ctx context.Context, identityID string, limit, offset int) ([]Link, error) {
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	var out []Link
	txErr := db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		rows, qErr := tx.Query(ctx, `SELECT `+linkColumns+` FROM aura.shared_links WHERE owner_identity_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, owner, limit, offset)
		if qErr != nil {
			return fmt.Errorf("list shares for %s: %w", identityID, qErr)
		}
		defer rows.Close()
		out = make([]Link, 0, limit)
		for rows.Next() {
			l, sErr := scanLink(rows)
			if sErr != nil {
				return fmt.Errorf("scan share link: %w", sErr)
			}
			out = append(out, l)
		}
		if rErr := rows.Err(); rErr != nil {
			return fmt.Errorf("iterate share links: %w", rErr)
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return out, nil
}

// ListForConversation returns identityID's share links for one conversation, newest first —
// drives the conversation's "Condiviso" section. Owner-scoped: a foreign conversation id
// yields an empty (not an error) list, since a caller is never told a foreign conversation
// exists via this path.
func (s *Store) ListForConversation(ctx context.Context, conversationID, identityID string) ([]Link, error) {
	conv, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("list shares for conversation: %w", err)
	}
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return nil, fmt.Errorf("list shares for conversation: %w", err)
	}
	var out []Link
	txErr := db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		rows, qErr := tx.Query(ctx, `SELECT `+linkColumns+` FROM aura.shared_links WHERE conversation_id = $1 AND owner_identity_id = $2 ORDER BY created_at DESC`, conv, owner)
		if qErr != nil {
			return fmt.Errorf("list shares for conversation %s: %w", conversationID, qErr)
		}
		defer rows.Close()
		out = make([]Link, 0, 4)
		for rows.Next() {
			l, sErr := scanLink(rows)
			if sErr != nil {
				return fmt.Errorf("scan share link: %w", sErr)
			}
			out = append(out, l)
		}
		if rErr := rows.Err(); rErr != nil {
			return fmt.Errorf("iterate share links: %w", rErr)
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return out, nil
}

// UpdateSnapshot re-points a share link at a freshly rebuilt snapshot (D-06 "Update") and
// bumps updated_at to at, owner-scoped. A foreign or absent id returns ErrShareNotFound
// (rows-affected 0), never a distinct error.
func (s *Store) UpdateSnapshot(ctx context.Context, shareID, identityID string, snapshotID uuid.UUID, at time.Time) error {
	id, err := parseUUID("id", shareID)
	if err != nil {
		return fmt.Errorf("update share snapshot: %w", err)
	}
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return fmt.Errorf("update share snapshot: %w", err)
	}
	return db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		tag, xErr := tx.Exec(ctx, `UPDATE aura.shared_links SET snapshot_id = $1, updated_at = $2 WHERE id = $3 AND owner_identity_id = $4`, snapshotID, at, id, owner)
		if xErr != nil {
			return fmt.Errorf("update share snapshot %s: %w", shareID, xErr)
		}
		if tag.RowsAffected() == 0 {
			return ErrShareNotFound
		}
		return nil
	})
}

// RevokeForIdentity stamps revoked_at = at on a live share link, owner-scoped. A foreign id,
// an absent id, or an already-revoked link all return ErrShareNotFound (rows-affected 0) —
// revoke is not idempotent-silent, matching the resource-already-gone REST convention every
// other *ForIdentity mutation in this codebase follows.
func (s *Store) RevokeForIdentity(ctx context.Context, shareID, identityID string, at time.Time) error {
	id, err := parseUUID("id", shareID)
	if err != nil {
		return fmt.Errorf("revoke share: %w", err)
	}
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return fmt.Errorf("revoke share: %w", err)
	}
	return db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		tag, xErr := tx.Exec(ctx, `UPDATE aura.shared_links SET revoked_at = $1, updated_at = $1 WHERE id = $2 AND owner_identity_id = $3 AND revoked_at IS NULL`, at, id, owner)
		if xErr != nil {
			return fmt.Errorf("revoke share %s: %w", shareID, xErr)
		}
		if tag.RowsAffected() == 0 {
			return ErrShareNotFound
		}
		return nil
	})
}

// RevokeForConversation revokes every still-live share link for one conversation, owner-
// scoped, and returns the revoked rows (snapshot_id/snapshot_bucket intact) so the D-15
// cascade (Runner.DeleteConversationLifecycle) can drop their Garage bytes. An empty result
// is not an error — a conversation with zero live links revokes zero rows.
func (s *Store) RevokeForConversation(ctx context.Context, conversationID, identityID string, at time.Time) ([]Link, error) {
	conv, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("revoke shares for conversation: %w", err)
	}
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return nil, fmt.Errorf("revoke shares for conversation: %w", err)
	}
	var out []Link
	txErr := db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		rows, qErr := tx.Query(ctx, `UPDATE aura.shared_links SET revoked_at = $1, updated_at = $1
			WHERE conversation_id = $2 AND owner_identity_id = $3 AND revoked_at IS NULL
			RETURNING `+linkColumns, at, conv, owner)
		if qErr != nil {
			return fmt.Errorf("revoke shares for conversation %s: %w", conversationID, qErr)
		}
		defer rows.Close()
		out = make([]Link, 0, 4)
		for rows.Next() {
			l, sErr := scanLink(rows)
			if sErr != nil {
				return fmt.Errorf("scan revoked share link: %w", sErr)
			}
			out = append(out, l)
		}
		if rErr := rows.Err(); rErr != nil {
			return fmt.Errorf("iterate revoked share links: %w", rErr)
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return out, nil
}

// DueForExpiry returns the sweep's due set — still-live links whose expires_at has passed
// now — over shared_links_expiry_idx, oldest-expiring first. It is NOT owner-scoped (the
// expiry sweep runs system-wide, with no principal) and reads on the plain pool, mirroring
// ResolveByToken/ResolveLiveByID's plain-pool reasoning: there is no caller identity to set.
// now is caller-supplied (the sweep's own transaction clock), matching expiry.go's
// deliberately clock-free, deterministically-testable convention — this is a different
// concern from ResolveByToken/ResolveLiveByID's security-critical liveness predicate, which
// MUST use the DB clock; DueForExpiry only selects a batch for the sweep to process, and
// that resolve-time predicate is the actual fail-closed gate regardless of what is "due".
func (s *Store) DueForExpiry(ctx context.Context, now time.Time, limit int) ([]Link, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+linkColumns+` FROM aura.shared_links
		WHERE revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at <= $1
		ORDER BY expires_at ASC LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("due for expiry: %w", err)
	}
	defer rows.Close()
	out := make([]Link, 0, limit)
	for rows.Next() {
		l, sErr := scanLink(rows)
		if sErr != nil {
			return nil, fmt.Errorf("scan due share link: %w", sErr)
		}
		out = append(out, l)
	}
	if rErr := rows.Err(); rErr != nil {
		return nil, fmt.Errorf("iterate due share links: %w", rErr)
	}
	return out, nil
}

// ResolveByToken is the lazy fail-closed gate for the public tier (OQ3/D-15): its single
// predicate — hash-indexed equality on token_hash AND revoked_at IS NULL AND (expires_at IS
// NULL OR expires_at > now()) — makes an expired or revoked link 404 even if the expiry
// sweep never runs (scheduler down, task unseeded, worker crash-looping). A sweep-only design
// has a live window between expires_at and the next tick; this predicate closes it.
//
// Two deliberate deviations from every other method in this file:
//
//  1. It does NOT use WithIdentityTxRaw. A public recipient has no principal — there is no
//     identity to set. It reads on the plain pool, mirroring PgAuditStore's documented plain-
//     pool read (audit_store.go:11-17): the token hash is the capability, and the predicate
//     below is the trust boundary for this read, not an RLS session var.
//  2. The lookup is hash-indexed equality (`token_hash = $1`), NOT a constant-time scan. The
//     amended D-13 rationale: the lookup key is already SHA-256(token), so exploiting a
//     timing signal on the index probe to recover the plaintext token would require inverting
//     SHA-256 — the literal "constant-time compare" reading is slower and no more secure.
//     crypto/subtle.ConstantTimeCompare is correct only where a secret is compared in Go
//     memory, and this design has no such site. Do not "fix" this into a table scan.
//
// The predicate uses the DB clock (now()), never a Go-side time.Now(): a skewed application
// clock must never be able to resurrect an expired link.
func (s *Store) ResolveByToken(ctx context.Context, tokenHash []byte) (Link, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+linkColumns+` FROM aura.shared_links
		WHERE token_hash = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`, tokenHash)
	l, err := scanLink(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Link{}, ErrShareNotFound
		}
		return Link{}, fmt.Errorf("resolve share by token: %w", err)
	}
	return l, nil
}

// ResolveLiveByID is the internal-tier sibling of ResolveByToken (D-10 bearer-within-auth) —
// the method share.Service.ResolveInternal (plan 37F-08) calls, and the ONLY method on this
// store that can serve a non-owner authenticated read. Three deliberate properties:
//
//  1. No owner filter, by design. Any authenticated identity holding the link may open the
//     already-redacted snapshot (D-10) — the absence of an `AND owner_identity_id = $2`
//     predicate is intentional, not forgotten. Adding one would silently reduce the internal
//     tier to an owner-only view and break the D-10 cross-identity contract (SC4 row 3). The
//     gate is RequireAuth at the mount (plan 37F-12) plus the 122-bit unguessable id; the
//     snapshot's own redaction (D-08) bounds what a bearer can see.
//  2. tier = 'internal' is folded into the SQL predicate, not a post-hoc Go check. A public
//     share's id must never resolve here: admitting it would open a second, id-addressed path
//     to a public snapshot that bypasses ResolveByToken's token predicate entirely. Because the
//     wrong-tier row never reaches Go, the handler cannot leak a tier mismatch as a
//     distinguishable status.
//  3. The identical lazy fail-closed predicate as ResolveByToken — revoked_at IS NULL AND
//     (expires_at IS NULL OR expires_at > now()), on the DB clock. An expired or revoked
//     internal link 404s even if the sweep never runs. This predicate is duplicated
//     deliberately; the two resolvers must never drift apart.
//
// Like ResolveByToken, this reads on the plain pool without WithIdentityTxRaw — but unlike
// ResolveByToken the caller DOES have a principal, so the omission can look like a bug at a
// glance. It is not: the principal is deliberately not the scope of this read (that is the
// whole point of D-10), and setting it as the RLS session var would make a future owner
// policy on this table reject a legitimate bearer. The id + tier + liveness predicate is the
// trust boundary for this read, not the caller's identity.
func (s *Store) ResolveLiveByID(ctx context.Context, shareID uuid.UUID) (Link, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+linkColumns+` FROM aura.shared_links
		WHERE id = $1 AND tier = 'internal' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`, shareID)
	l, err := scanLink(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Link{}, ErrShareNotFound
		}
		return Link{}, fmt.Errorf("resolve internal share %s: %w", shareID, err)
	}
	return l, nil
}

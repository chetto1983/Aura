package agui

// audit_store.go is the raw-pgx read store behind the D-28 admin per-user audit API
// (audit_api.go). It unions the three identity-keyed audit ledgers — aura.mcp_audit
// (keyed on actor_identity_id), aura.skill_audit (keyed on identity_id), and
// aura.tool_invocations (conversation-keyed, joined to aura.conversations.identity_id) —
// into ONE newest-first activity feed for a single identity, with pagination. It mirrors
// the raw-pgx store precedent shipped this phase (internal/objectstore/identity_store.go)
// and internal/documents/catalog_store.go; no sqlc query is added.
//
// The tool-plane leg joins aura.conversations, which carries owner RLS (migration 0032).
// PgAuditStore reads on a plain pool connection (app.current_identity unset), where the
// 0032 policy is permissive-on-unset, so the join sees the target identity's rows. That
// is correct BY DESIGN here: the ONLY caller is the admin audit handler, which is gated
// server-side by RequireCapability(governance.write) at the route mount — the admin is
// explicitly authorized to observe another identity's activity (D-28). The route gate,
// not the RLS session var, is the trust boundary for this read.

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditEvent is one normalized row of the D-28 admin per-user activity feed: a single
// event unioned from the three identity-keyed audit ledgers, projected to a common shape.
// Target is the affected object (an MCP server_name / skill_name / tool_name); Detail is
// the ledger-specific note (an mcp reason / skill actor_id / tool status). Both may carry
// user-authored text, so the handler SanitizeStrings them before the wire (T-36-10-I).
type AuditEvent struct {
	Source    string    `json:"source"` // "mcp" | "skill" | "tool"
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// PgAuditStore reads the identity-keyed audit ledgers for the admin audit UI (D-28).
type PgAuditStore struct {
	pool *pgxpool.Pool
}

// NewPgAuditStore builds the admin audit read store over the shared pool.
func NewPgAuditStore(pool *pgxpool.Pool) *PgAuditStore { return &PgAuditStore{pool: pool} }

// auditActivityQuery unions the three identity-keyed ledgers into one newest-first feed
// for a single identity. $1 is the text[] set of audit keys (the identity UUID, plus the
// literal 'local' when it is the seeded operator — skill_audit defaults identity_id to
// 'local'); $2 is the identity UUID for the conversation-owner join; $3/$4 are LIMIT/
// OFFSET. Column names come from the first SELECT (UNION ALL matches by position).
const auditActivityQuery = `
SELECT source, action, target, detail, created_at FROM (
    SELECT 'mcp'   AS source, action           AS action, server_name  AS target, COALESCE(reason, '')     AS detail, created_at AS created_at
      FROM aura.mcp_audit
      WHERE actor_identity_id = ANY($1::text[])
    UNION ALL
    SELECT 'skill' AS source, action, skill_name, actor_id, created_at
      FROM aura.skill_audit
      WHERE identity_id = ANY($1::text[])
    UNION ALL
    SELECT 'tool'  AS source, ti.event_kind, ti.tool_name, COALESCE(ti.status, ''), ti.ts
      FROM aura.tool_invocations ti
      JOIN aura.conversations c ON c.id = ti.conversation_id
      WHERE c.identity_id = $2::uuid
) feed
ORDER BY created_at DESC
LIMIT $3 OFFSET $4`

// ListActivityForIdentity returns the identity's audit activity newest-first, paginated.
// The caller (admin audit handler) has already validated identityID is a UUID (required
// for the $2::uuid cast) and clamped limit/offset.
func (s *PgAuditStore) ListActivityForIdentity(ctx context.Context, identityID string, limit, offset int) ([]AuditEvent, error) {
	rows, err := s.pool.Query(ctx, auditActivityQuery, auditIdentityKeys(identityID), identityID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("audit activity for %s: %w", identityID, err)
	}
	defer rows.Close()
	out := make([]AuditEvent, 0, limit)
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.Source, &e.Action, &e.Target, &e.Detail, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return out, nil
}

// auditIdentityKeys returns the text audit keys for an identity: the UUID itself, plus the
// literal "local" when the identity is the seeded local operator. skill_audit defaults
// identity_id to 'local', so the operator's skill rows are keyed by the literal, while
// mcp_audit + the tool_invocations→conversations join always carry the UUID. A provisioned
// identity has one consistent UUID key across all three ledgers.
func auditIdentityKeys(identityID string) []string {
	if identityID == localIdentityID {
		return []string{identityID, "local"}
	}
	return []string{identityID}
}

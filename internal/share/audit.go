// audit.go is the aura.share_audit append-only writer (migration 0040, D-14 / T-37F-10 —
// Repudiation defense). Two non-obvious choices, both already decided at the schema level
// (see migration 0040's own comments) and repeated here because they shape this file:
//
//   - identity_id is text with NO FK — matching skill_audit (0010) / mcp_audit (0022)
//     verbatim. An FK with ON DELETE CASCADE would destroy the audit trail the moment an
//     identity is deleted, the exact opposite of a ledger's purpose; text is also what lets
//     this column hold the literal 'local' that auditIdentityKeys (audit_store.go:99) appends
//     for the seeded operator identity, so the union leg in audit_store.go keys on the same
//     $1::text[] array as the other three ledgers.
//   - The ledger is append-only BY GRANT (aura_app has SELECT, INSERT only — no UPDATE, no
//     DELETE). This writer must not pretend otherwise: it exposes exactly one method, Append,
//     and there is no Update/Delete surface anywhere in this file for a caller to reach for.
//
// Never audit a plaintext token (D-13): Append takes no token parameter, and the open action
// records only a timestamp + coarse tier/detail — no recipient PII, no IP, no user-agent.
package share

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditAction is one of the five share-lifecycle actions the migration 0040 CHECK
// (action IN ('create','update','revoke','expire','open')) admits.
type AuditAction string

// The five audited share-lifecycle actions (migration 0040's action CHECK admits exactly
// these five values; there is no sixth).
const (
	AuditCreate AuditAction = "create"
	AuditUpdate AuditAction = "update"
	AuditRevoke AuditAction = "revoke"
	AuditExpire AuditAction = "expire"
	AuditOpen   AuditAction = "open"
)

// AuditWriter appends to aura.share_audit over the shared app pool.
type AuditWriter struct {
	pool *pgxpool.Pool
}

// NewAuditWriter builds the share audit writer over the shared Postgres pool.
func NewAuditWriter(pool *pgxpool.Pool) *AuditWriter { return &AuditWriter{pool: pool} }

// Append inserts one append-only share_audit row: identityID is the acting principal (the
// literal "local" for the seeded operator, matching the other audit ledgers), shareLinkID +
// conversationID correlate the event to the link/conversation it describes (both columns are
// nullable/no-FK at the schema level, but every call site in this phase always has both), tier
// is the link's tier at the time of the action, and detail is a short, non-secret, coarse
// note. detail MUST NEVER carry a plaintext token, a recipient's IP, or a User-Agent string —
// this is the audit trail's own attack surface (T-37F-11 / T-37F-39), not just a style rule.
func (w *AuditWriter) Append(ctx context.Context, identityID string, shareLinkID, conversationID uuid.UUID, action AuditAction, tier, detail string) error {
	_, err := w.pool.Exec(ctx, `
		INSERT INTO aura.share_audit (identity_id, share_link_id, conversation_id, action, tier, detail)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		identityID, shareLinkID, conversationID, string(action), tier, detail,
	)
	if err != nil {
		return fmt.Errorf("append share audit (%s): %w", action, err)
	}
	return nil
}

package conversations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/secret"
	"github.com/jackc/pgx/v5"
)

const (
	defaultProjectionPageSize = 100
	maxProjectionPageSize     = 1000
)

// ProjectionCursor is the stable PostgreSQL source position used by replay and
// reconciliation. ConversationID and Seq are ordered together.
type ProjectionCursor struct {
	ConversationID string
	Seq            int
}

// ProjectionTurn is the searchable, reasoning-free projection of one
// authoritative PostgreSQL conversation turn.
type ProjectionTurn struct {
	IdentityID     string
	ConversationID string
	Seq            int
	Role           string
	Content        string
	ContentHash    string
	OccurredAt     time.Time
	SourceRef      string
}

// ProjectionTombstone is an authoritative soft-deletion marker. Hard-deleted
// rows are detected by graph pruning against a complete source replay.
type ProjectionTombstone struct {
	IdentityID     string
	ConversationID string
	DeletedAt      time.Time
}

func projectionTurnEligible(role, content, toolCallID string, toolCalls []byte) bool {
	if role != "user" && role != "assistant" {
		return false
	}
	return strings.TrimSpace(content) != "" && strings.TrimSpace(toolCallID) == "" && len(toolCalls) == 0
}

// ListProjectionTurns reads one stable page of eligible authoritative turns.
// It is deliberately independent of SearchConversationTurns: projection must
// include sidecar-backed content and has no user query to rank against.
func (s *Store) ListProjectionTurns(
	ctx context.Context,
	identityID string,
	after ProjectionCursor,
	limit int,
) ([]ProjectionTurn, ProjectionCursor, error) {
	identityUUID, err := db.ParseUUID("identity_id", identityID)
	if err != nil {
		return nil, after, fmt.Errorf("list projection turns: %w", err)
	}
	if s == nil || s.pool == nil {
		return nil, after, fmt.Errorf("list projection turns: store is not initialized")
	}
	limit = projectionPageSize(limit)
	out := make([]ProjectionTurn, 0, limit)
	next := after
	err = db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		rows, queryErr := tx.Query(ctx, `
SELECT c.identity_id::text, t.conversation_id::text, t.seq, t.role,
       COALESCE(t.content, ''), COALESCE(t.content_sidecar_path, ''), t.created_at
  FROM aura.conversation_turns AS t
  JOIN aura.conversations AS c ON c.id = t.conversation_id
 WHERE c.identity_id = $1
   AND c.status <> 'deleted'
   AND (t.conversation_id::text > $2 OR
        (t.conversation_id::text = $2 AND t.seq > $3))
   AND t.role IN ('user', 'assistant')
   AND t.tool_call_id IS NULL
   AND t.tool_calls IS NULL
   AND (NULLIF(BTRIM(t.content), '') IS NOT NULL OR t.content_sidecar_path IS NOT NULL)
 ORDER BY t.conversation_id::text, t.seq
 LIMIT $4`, identityUUID, after.ConversationID, after.Seq, limit)
		if queryErr != nil {
			return fmt.Errorf("query projection turns: %w", queryErr)
		}
		defer rows.Close()
		for rows.Next() {
			var identity, conversationID, role, content, sidecarPath string
			var seq int
			var occurredAt time.Time
			if scanErr := rows.Scan(&identity, &conversationID, &seq, &role, &content, &sidecarPath, &occurredAt); scanErr != nil {
				return fmt.Errorf("scan projection turn: %w", scanErr)
			}
			next = ProjectionCursor{ConversationID: conversationID, Seq: seq}
			if sidecarPath != "" {
				data, readErr := s.readTurnSidecarReserved(conversationID, seq, maxManagedSidecarBytes, nil)
				if readErr != nil {
					return fmt.Errorf("read projection sidecar %s seq %d: %w", conversationID, seq, readErr)
				}
				content = string(data)
			}
			content = db.PostgresTextSafe(secret.RedactConfigured(content))
			if !projectionTurnEligible(role, content, "", nil) {
				continue
			}
			out = append(out, ProjectionTurn{
				IdentityID:     identity,
				ConversationID: conversationID,
				Seq:            seq,
				Role:           role,
				Content:        content,
				ContentHash:    projectionContentHash(content),
				OccurredAt:     occurredAt.UTC(),
				SourceRef:      projectionSourceRef(conversationID, seq),
			})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, after, err
	}
	return out, next, nil
}

// ListProjectionTombstones reads deleted conversations in stable ID order.
func (s *Store) ListProjectionTombstones(
	ctx context.Context,
	identityID, afterConversationID string,
	limit int,
) ([]ProjectionTombstone, string, error) {
	return nil, afterConversationID, fmt.Errorf("list projection tombstones: not implemented")
}

func projectionPageSize(limit int) int {
	if limit <= 0 {
		return defaultProjectionPageSize
	}
	return min(limit, maxProjectionPageSize)
}

func projectionContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func projectionSourceRef(conversationID string, seq int) string {
	return fmt.Sprintf("postgres://aura/conversations/%s/turns/%d", conversationID, seq)
}

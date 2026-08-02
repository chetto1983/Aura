package conversations

// store_search.go is the cross-slice full-text search half of the Store, split out of
// store.go when that file crossed the 600-LOC ceiling (CLAUDE.md, refactor-on-touch). The
// LOCKED sqlc query (SPEC Req#13 / D-A5-03) and the owner filter are unchanged by the move.

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

// SearchResult is one FTS hit (the app-side excerpt is the CLI/channel's job).
type SearchResult struct {
	ConversationID string
	Seq            int
	Content        string
	Similarity     float32
}

// SearchConversationTurns wraps the LOCKED cross-slice FTS query (SPEC Req#13 /
// D-A5-03): content % $1 ORDER BY similarity(content,$1) DESC LIMIT $2. The query
// SQL is the contract Telegram /search (Phase 13) reuses byte-for-byte; this
// wrapper only projects pgtype at the boundary. The SQL is never rewritten here.
//
// LOOP-10 / D-10 boundary: spilled turns (content over the cap) store content=NULL
// and are therefore EXCLUDED from this search by construction — `content % $1` never
// matches a NULL. This is a documented+asserted boundary (see maybeSpill and the
// SearchSpill db_integration test), not an oversight: pg_trgm similarity() is
// length-normalized, so a >cap (≥64 KiB) body scores ~0 and would never clear the
// 0.3 threshold even if content were repopulated. The deferred upgrade path is a
// short-preview column (length-compatible with %) at a future migration, never a
// rewrite of this locked query.
func (s *Store) SearchConversationTurns(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return s.searchTurns(ctx, query, limit, "")
}

// searchTurns is the shared FTS body behind SearchConversationTurns (unscoped) and
// SearchConversationTurnsForIdentity (Phase 36 owner-scoped). The LOCKED sqlc query is
// NEVER rewritten — ownerFilter is applied Go-side alongside the existing deleted-status
// skip (a hit whose conversation is not owned by ownerFilter is dropped so an FTS query
// can never surface another identity's turn content, MUSR-01). ownerFilter == "" keeps
// the pre-Phase-36 unscoped behavior. The per-hit conversation is cached (status + owner
// both read from the one projection) so a repeated conversation costs a single Get.
func (s *Store) searchTurns(ctx context.Context, query string, limit int, ownerFilter string) ([]SearchResult, error) {
	// ownerFilter, when set, is BOTH the Go-side predicate and the RLS scope: the caller named
	// the owner explicitly, so the transaction is bound to them rather than to whoever is on
	// the context. Unscoped search falls back to the context identity.
	scope := ownerFilter
	if scope == "" {
		scope = identityctx.IdentityID(ctx)
	}
	var out []SearchResult
	if err := s.scopedTx(ctx, scope, func(q *sqlc.Queries) error {
		rows, sErr := q.SearchConversationTurns(ctx, sqlc.SearchConversationTurnsParams{
			Similarity: query,
			Limit:      normalizeSearchLimit(limit),
		})
		if sErr != nil {
			return fmt.Errorf("search conversation turns: %w", sErr)
		}
		out = make([]SearchResult, 0, len(rows))
		convByID := make(map[string]Conversation)
		for _, r := range rows {
			convID := uuid.UUID(r.ConversationID.Bytes).String()
			conv, ok := convByID[convID]
			if !ok {
				row, gErr := q.GetConversation(ctx, r.ConversationID)
				if gErr != nil {
					return fmt.Errorf("search conversation turns: load conversation %s: %w", convID, gErr)
				}
				conv = conversationFromRow(row)
				convByID[convID] = conv
			}
			if conv.Status == StatusDeleted {
				continue
			}
			if ownerFilter != "" && conv.IdentityID != ownerFilter {
				continue
			}
			out = append(out, SearchResult{
				ConversationID: convID,
				Seq:            int(r.Seq),
				Content:        r.Content.String,
				Similarity:     r.Sim,
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeSearchLimit(limit int) int32 {
	if limit <= 0 {
		return 20
	}
	if limit > 2147483647 {
		return 2147483647
	}
	return int32(limit)
}

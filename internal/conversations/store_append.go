package conversations

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
)

// AppendTurnParams carries one new turn plus its token/cost delta. Cost is the
// USD figure to fold into the running total_cost_usd aggregate (RESEARCH OQ4 —
// aggregated in SQL inside the same tx). A Seq <= 0 asks the Store to allocate the
// next per-conversation seq inside the append transaction.
type AppendTurnParams struct {
	ConversationID string
	Seq            int
	Role           string
	Content        string
	ToolCallID     string
	ToolCalls      []byte
	InputTokens    int
	OutputTokens   int
	CachedTokens   int
	CostUSD        float64
}

// AppendTurn writes one turn AND folds its token/cost delta into the conversation
// aggregates in a SINGLE db.WithTx transaction (SC-2): a failure between the turn
// INSERT and the aggregates UPDATE rolls the whole thing back, leaving no partial
// turn. When Seq <= 0, the Store row-locks the parent conversation and allocates
// MAX(seq)+1 inside the same tx so concurrent appenders cannot race. Content over
// turnCapBytes spills to a sidecar file once the final seq is known (the file write
// is not part of the DB atomicity); cleanupSidecarOnTxError removes a just-spilled
// file when the tx rolls back (M-04), and a boot scan reconciles any leftover. The
// row then stores content=NULL + content_sidecar_path.
func (s *Store) AppendTurn(ctx context.Context, p AppendTurnParams) error {
	if p.Seq > 0 {
		turn, agg, err := s.appendTurnWrites(p)
		if err != nil {
			return err
		}
		if err := cleanupSidecarOnTxError(
			func() error {
				return db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
					return insertTurnAndAggregates(ctx, q, turn, agg)
				})
			},
			func() string { return turn.ContentSidecarPath.String },
		); err != nil {
			return fmt.Errorf("append turn %s seq %d: %w", p.ConversationID, p.Seq, err)
		}
		return nil
	}

	// Seq is allocated inside the tx, so the sidecar (keyed by seq) is spilled there
	// too; record the spilled path so a rollback removes the orphaned file.
	var spilledPath string
	if err := cleanupSidecarOnTxError(
		func() error {
			return db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
				seq, err := s.allocateTurnSeq(ctx, q, p.ConversationID)
				if err != nil {
					return err
				}
				p.Seq = seq
				turn, agg, err := s.appendTurnWrites(p)
				if err != nil {
					return err
				}
				spilledPath = turn.ContentSidecarPath.String
				return insertTurnAndAggregates(ctx, q, turn, agg)
			})
		},
		func() string { return spilledPath },
	); err != nil {
		return fmt.Errorf("append turn %s seq %d: %w", p.ConversationID, p.Seq, err)
	}
	return nil
}

// AppendAssistantTurnWithCacheMetric writes a terminal assistant turn, folds its
// usage into the conversation aggregate, and appends the matching cache_metrics row
// in one database transaction. The Runner uses this for final assistant Events so a
// metric failure cannot leave the conversation with an answer the caller never
// receives.
func (s *Store) AppendAssistantTurnWithCacheMetric(ctx context.Context, p AppendTurnParams, metric sqlc.InsertCacheMetricParams) error {
	if p.Seq > 0 {
		metric.Seq = int32(p.Seq) // sync the cache_metric seq to the caller-supplied turn seq
		turn, agg, err := s.appendTurnWrites(p)
		if err != nil {
			return err
		}
		if err := cleanupSidecarOnTxError(
			func() error {
				return db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
					if err := insertTurnAndAggregates(ctx, q, turn, agg); err != nil {
						return err
					}
					if err := q.InsertCacheMetric(ctx, metric); err != nil {
						return fmt.Errorf("insert cache metric: %w", err)
					}
					return nil
				})
			},
			func() string { return turn.ContentSidecarPath.String },
		); err != nil {
			return fmt.Errorf("append assistant turn with cache metric %s seq %d: %w", p.ConversationID, p.Seq, err)
		}
		return nil
	}

	var spilledPath string
	if err := cleanupSidecarOnTxError(
		func() error {
			return db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
				seq, err := s.allocateTurnSeq(ctx, q, p.ConversationID)
				if err != nil {
					return err
				}
				p.Seq = seq
				metric.Seq = int32(seq)
				turn, agg, err := s.appendTurnWrites(p)
				if err != nil {
					return err
				}
				spilledPath = turn.ContentSidecarPath.String
				if err := insertTurnAndAggregates(ctx, q, turn, agg); err != nil {
					return err
				}
				if err := q.InsertCacheMetric(ctx, metric); err != nil {
					return fmt.Errorf("insert cache metric: %w", err)
				}
				return nil
			})
		},
		func() string { return spilledPath },
	); err != nil {
		return fmt.Errorf("append assistant turn with cache metric %s seq %d: %w", p.ConversationID, p.Seq, err)
	}
	return nil
}

func (s *Store) allocateTurnSeq(ctx context.Context, q *sqlc.Queries, conversationID string) (int, error) {
	id, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("allocate turn seq: %w", err)
	}
	if _, err := q.LockConversationForTurnAppend(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("allocate turn seq %s: %w", conversationID, ErrConversationNotFound)
		}
		return 0, fmt.Errorf("lock conversation %s for turn append: %w", conversationID, err)
	}
	seq, err := q.NextConversationTurnSeq(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("next turn seq %s: %w", conversationID, err)
	}
	return int(seq), nil
}

func (s *Store) appendTurnWrites(p AppendTurnParams) (sqlc.InsertConversationTurnParams, sqlc.UpdateConversationAggregatesParams, error) {
	convID, err := parseUUID("conversation_id", p.ConversationID)
	if err != nil {
		return sqlc.InsertConversationTurnParams{}, sqlc.UpdateConversationAggregatesParams{}, fmt.Errorf("append turn: %w", err)
	}
	safeContent := postgresTextSafe(p.Content)
	content, sidecarPath, err := s.maybeSpill(p.ConversationID, p.Seq, safeContent)
	if err != nil {
		return sqlc.InsertConversationTurnParams{}, sqlc.UpdateConversationAggregatesParams{}, fmt.Errorf("append turn %s seq %d: %w", p.ConversationID, p.Seq, err)
	}
	cost, err := numericFromFloat(p.CostUSD)
	if err != nil {
		return sqlc.InsertConversationTurnParams{}, sqlc.UpdateConversationAggregatesParams{}, fmt.Errorf("append turn %s seq %d: %w", p.ConversationID, p.Seq, err)
	}

	turn := sqlc.InsertConversationTurnParams{
		ConversationID:     convID,
		Seq:                int32(p.Seq),
		Role:               p.Role,
		Content:            content,
		ContentSidecarPath: sidecarPath,
		ToolCallID:         optionalText(p.ToolCallID),
		ToolCalls:          p.ToolCalls,
		InputTokens:        int32(p.InputTokens),
		OutputTokens:       int32(p.OutputTokens),
		CachedTokens:       int32(p.CachedTokens),
	}
	agg := sqlc.UpdateConversationAggregatesParams{
		InputTokens:  int64(p.InputTokens),
		OutputTokens: int64(p.OutputTokens),
		CachedTokens: int64(p.CachedTokens),
		CostUsd:      cost,
		ID:           convID,
	}

	return turn, agg, nil
}

func insertTurnAndAggregates(ctx context.Context, q *sqlc.Queries, turn sqlc.InsertConversationTurnParams, agg sqlc.UpdateConversationAggregatesParams) error {
	if err := q.InsertConversationTurn(ctx, turn); err != nil {
		return fmt.Errorf("insert turn: %w", err)
	}
	if err := q.UpdateConversationAggregates(ctx, agg); err != nil {
		return fmt.Errorf("update aggregates: %w", err)
	}
	return nil
}

// cleanupSidecarOnTxError runs the turn-append transaction and, when it returns an
// error (rollback), best-effort removes the sidecar file that was spilled for THAT
// turn so a rolled-back turn never leaves an orphaned file alive for the rest of a
// long-running daemon (M-04 / AP-16). spilledPath is evaluated only on the error
// path — for the seq-allocated branch the spill happens inside run, so the path is
// not known until after run returns. The remove error is intentionally ignored: the
// boot orphan scan (orphan_scan.go) remains the backstop. The success path is left
// untouched: the file is the durable home of the committed turn's content.
func cleanupSidecarOnTxError(run func() error, spilledPath func() string) error {
	err := run()
	if err != nil {
		if path := spilledPath(); path != "" {
			_ = os.Remove(path)
		}
	}
	return err
}

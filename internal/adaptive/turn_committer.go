package adaptive

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgxpool"
)

type conversationTurnTxStore interface {
	AllocateTurnSeqTx(context.Context, *sqlc.Queries, string) (int, error)
	AppendTurnTxWithSpillCleanup(context.Context, *sqlc.Queries, conversations.AppendTurnParams) (func(), error)
}

// TurnCommitter atomically pairs a durable Runner turn with its adaptive
// outcome event. The Postgres outbox is authoritative; Neo4j projection happens
// later and therefore cannot partially commit the interactive turn.
type TurnCommitter struct {
	pool     *pgxpool.Pool
	turns    conversationTurnTxStore
	adaptive *Store
}

// NewTurnCommitter binds turn persistence to the authoritative adaptive ledger.
func NewTurnCommitter(pool *pgxpool.Pool, turns conversationTurnTxStore, adaptive *Store) *TurnCommitter {
	return &TurnCommitter{pool: pool, turns: turns, adaptive: adaptive}
}

// CommitTurn atomically appends one turn, optional metric, and adaptive event.
func (c *TurnCommitter) CommitTurn(
	ctx context.Context,
	turn conversations.AppendTurnParams,
	metric *sqlc.InsertCacheMetricParams,
	event Event,
) error {
	if c == nil || c.pool == nil || c.turns == nil || c.adaptive == nil {
		return errors.New("adaptive turn committer is not fully configured")
	}
	var cleanupSpill func()
	err := db.WithIdentityTx(ctx, c.pool, event.OwnerID.String(), func(q *sqlc.Queries) error {
		_, duplicate, err := c.adaptive.RecordTx(ctx, q, event)
		if err != nil {
			return err
		}
		if duplicate {
			return nil
		}
		seq, err := c.turns.AllocateTurnSeqTx(ctx, q, turn.ConversationID)
		if err != nil {
			return err
		}
		turn.Seq = seq
		cleanupSpill, err = c.turns.AppendTurnTxWithSpillCleanup(ctx, q, turn)
		if err != nil {
			return err
		}
		if metric != nil {
			copied := *metric
			copied.Seq = int32(seq)
			if err := q.InsertCacheMetric(ctx, copied); err != nil {
				return fmt.Errorf("insert cache metric: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		if cleanupSpill != nil {
			cleanupSpill()
		}
		return fmt.Errorf("commit turn with adaptive event %s: %w", event.ID, err)
	}
	return nil
}

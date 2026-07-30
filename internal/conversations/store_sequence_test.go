//go:build db_integration

package conversations

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/pgnumeric"
)

// TestAppendTurn_AutoSeqConcurrentSerializes proves that store-assigned sequence
// numbers are allocated inside the append transaction. Concurrent turn writers for
// the same conversation must persist a gap-free, duplicate-free seq series.
func TestAppendTurn_AutoSeqConcurrentSerializes(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := context.Background()

	const writers = 12
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- s.AppendTurn(ctx, AppendTurnParams{
				ConversationID: convID,
				Role:           llm.RoleUser,
				Content:        fmt.Sprintf("turn %02d", i),
			})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("AppendTurn(auto seq) returned error: %v", err)
		}
	}

	rows, err := pool.Query(ctx,
		"SELECT seq FROM aura.conversation_turns WHERE conversation_id=$1 ORDER BY seq ASC", convID)
	if err != nil {
		t.Fatalf("query seqs: %v", err)
	}
	defer rows.Close()
	var seqs []int
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			t.Fatalf("scan seq: %v", err)
		}
		seqs = append(seqs, seq)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("seq rows: %v", err)
	}
	if len(seqs) != writers {
		t.Fatalf("want %d turns, got %d seqs=%v", writers, len(seqs), seqs)
	}
	for i, seq := range seqs {
		if want := i + 1; seq != want {
			t.Fatalf("seqs must be 1..%d, got %v", writers, seqs)
		}
	}
}

// TestAppendAssistantTurnWithCacheMetric_AutoSeqAllocatesMetricSeq locks the
// terminal-assistant append path: when the Store allocates the turn seq, the
// cache_metrics row written in the same tx must use that exact seq.
func TestAppendAssistantTurnWithCacheMetric_AutoSeqAllocatesMetricSeq(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := context.Background()

	id, err := db.ParseUUID("conversation_id", convID)
	if err != nil {
		t.Fatalf("parse conv id: %v", err)
	}
	cost, err := pgnumeric.NumericFromFloat(0.0042)
	if err != nil {
		t.Fatalf("numeric cost: %v", err)
	}
	metric := sqlc.InsertCacheMetricParams{
		ConversationID: id,
		PromptTokens:   7,
		CachedTokens:   3,
		CostUsd:        cost,
	}
	if err := s.AppendAssistantTurnWithCacheMetric(ctx, AppendTurnParams{
		ConversationID: convID,
		Role:           llm.RoleAssistant,
		Content:        "done",
		InputTokens:    7,
		OutputTokens:   11,
		CachedTokens:   3,
		CostUSD:        0.0042,
	}, metric); err != nil {
		t.Fatalf("AppendAssistantTurnWithCacheMetric(auto seq): %v", err)
	}

	var turnSeq, metricSeq int
	if err := pool.QueryRow(ctx,
		"SELECT seq FROM aura.conversation_turns WHERE conversation_id=$1 AND role='assistant'", convID,
	).Scan(&turnSeq); err != nil {
		t.Fatalf("read assistant turn seq: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT seq FROM aura.cache_metrics WHERE conversation_id=$1", convID,
	).Scan(&metricSeq); err != nil {
		t.Fatalf("read cache metric seq: %v", err)
	}
	if turnSeq != 1 || metricSeq != 1 {
		t.Fatalf("auto seq mismatch: turn=%d metric=%d, want both 1", turnSeq, metricSeq)
	}
}

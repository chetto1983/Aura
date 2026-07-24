//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/pgnumeric"
	"github.com/google/uuid"
)

func TestStoreConcurrentRecordsAreIdempotentAndGapFree(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner, "adaptive-concurrent-"+owner.String()); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})
	store := NewStore(pool, StoreConfig{})
	decision := uuid.Must(uuid.NewV7())
	duplicate, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "same-event",
		DecisionID: decision, Kind: EventDecision,
		Payload: []byte(`{"schema_version":"1.0","action":"shadow"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	const writers = 24
	var wg sync.WaitGroup
	seqs := make(chan int64, writers)
	errs := make(chan error, writers)
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seq, err := store.Record(ctx, duplicate)
			seqs <- seq
			errs <- err
		}()
	}
	wg.Wait()
	close(seqs)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent duplicate Record: %v", err)
		}
	}
	for seq := range seqs {
		if seq != 1 {
			t.Fatalf("duplicate sequence = %d, want 1", seq)
		}
	}
	rows, err := store.ListAggregate(ctx, owner, "same-event")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("duplicate rows = %d, want 1", len(rows))
	}

	const distinct = 40
	distinctSeqs := make(chan int64, distinct)
	distinctErrs := make(chan error, distinct)
	for i := range distinct {
		event, err := NewEvent(EventParams{
			ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "distinct-events",
			DecisionID: decision, Kind: EventOutcome,
			Payload: []byte(fmt.Sprintf(`{"schema_version":"1.0","sample":%d}`, i)),
		})
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(event Event) {
			defer wg.Done()
			seq, err := store.Record(ctx, event)
			distinctSeqs <- seq
			distinctErrs <- err
		}(event)
	}
	wg.Wait()
	close(distinctSeqs)
	close(distinctErrs)
	for err := range distinctErrs {
		if err != nil {
			t.Fatalf("concurrent distinct Record: %v", err)
		}
	}
	got := make([]int, 0, distinct)
	for seq := range distinctSeqs {
		got = append(got, int(seq))
	}
	sort.Ints(got)
	for i, seq := range got {
		if want := i + 1; seq != want {
			t.Fatalf("sorted sequence[%d] = %d, want gap-free %d", i, seq, want)
		}
	}
}

func TestTurnCommitterAtomicallyRollsBackConversationWhenAdaptiveEventConflicts(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner, "adaptive-committer-"+owner.String()); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	conversationStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir()})
	convID := uuid.Must(uuid.NewV7()).String()
	if _, err := conversationStore.Create(ctx, conversations.CreateParams{
		ID: convID, IdentityID: owner.String(), Model: "adaptive-proof",
	}); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})

	store := NewStore(pool, StoreConfig{})
	committer := NewTurnCommitter(pool, conversationStore, store)
	decision := uuid.Must(uuid.NewV7())
	eventID := uuid.Must(uuid.NewV7())
	event, err := NewEvent(EventParams{
		ID: eventID, OwnerID: owner, AggregateID: "request-atomic",
		DecisionID: decision, Kind: EventOutcome,
		Payload: []byte(`{"schema_version":"1.0","status":"ok"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := committer.CommitTurn(ctx, conversations.AppendTurnParams{
		ConversationID: convID, Role: llm.RoleTool, Content: "first",
	}, nil, event); err != nil {
		t.Fatalf("CommitTurn(first): %v", err)
	}

	conflict, err := NewEvent(EventParams{
		ID: eventID, OwnerID: owner, AggregateID: "request-atomic",
		DecisionID: decision, Kind: EventOutcome,
		Payload: []byte(`{"schema_version":"1.0","status":"different"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := committer.CommitTurn(ctx, conversations.AppendTurnParams{
		ConversationID: convID, Role: llm.RoleTool, Content: "must-roll-back",
	}, nil, conflict); !errors.Is(err, ErrPayloadConflict) {
		t.Fatalf("CommitTurn(conflict) error = %v, want ErrPayloadConflict", err)
	}

	var turnCount, eventCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.conversation_turns WHERE conversation_id=$1`, convID,
	).Scan(&turnCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.adaptive_outbox WHERE owner_id=$1 AND aggregate_id='request-atomic'`, owner,
	).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if turnCount != 1 || eventCount != 1 {
		t.Fatalf("atomic counts after conflict = turns:%d events:%d, want 1/1", turnCount, eventCount)
	}
}

func TestTurnCommitterAtomicallyPersistsOversizedSpilledTurn(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner, "adaptive-spill-"+owner.String()); err != nil {
		t.Fatal(err)
	}
	runDir := t.TempDir()
	conversationStore := conversations.New(pool, conversations.Config{
		RunDir: runDir, TurnCapBytes: 16,
	})
	convID := uuid.Must(uuid.NewV7()).String()
	if _, err := conversationStore.Create(ctx, conversations.CreateParams{
		ID: convID, IdentityID: owner.String(), Model: "adaptive-spill-proof",
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})

	store := NewStore(pool, StoreConfig{})
	event, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "spill-request",
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventOutcome,
		Payload: []byte(`{"schema_version":"1.0","quality_observed":false}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("oversized-content-", 20)
	if err := NewTurnCommitter(pool, conversationStore, store).CommitTurn(
		ctx,
		conversations.AppendTurnParams{
			ConversationID: convID, Role: llm.RoleAssistant, Content: content,
		},
		nil,
		event,
	); err != nil {
		t.Fatalf("CommitTurn(oversized): %v", err)
	}
	history, err := conversationStore.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(history) != 1 || history[0].Content != content {
		t.Fatalf("rehydrated spilled history = %+v, want exact oversized content", history)
	}
	var eventCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.adaptive_outbox WHERE id=$1`, event.ID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("adaptive event count = %d, want 1", eventCount)
	}

	rollbackEvent, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "spill-request",
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventOutcome,
		Payload: []byte(`{"schema_version":"1.0","quality_observed":false,"attempt":"rollback"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	cost, err := pgnumeric.NumericFromFloat(0)
	if err != nil {
		t.Fatal(err)
	}
	// The deliberately foreign conversation ID makes the cache metric fail after
	// the turn has spilled, exercising the real transaction rollback cleanup path.
	badMetric := &sqlc.InsertCacheMetricParams{
		ConversationID: dbUUID(uuid.Must(uuid.NewV7())),
		PromptTokens:   1,
		CostUsd:        cost,
	}
	rolledBackPath := filepath.Join(runDir, "conversations", convID, "2.content")
	if err := NewTurnCommitter(pool, conversationStore, store).CommitTurn(
		ctx,
		conversations.AppendTurnParams{
			ConversationID: convID, Role: llm.RoleAssistant,
			Content: strings.Repeat("rollback-oversized-content-", 20),
		},
		badMetric,
		rollbackEvent,
	); err == nil {
		t.Fatal("CommitTurn(oversized post-spill failure) succeeded")
	}
	if _, err := os.Stat(rolledBackPath); !os.IsNotExist(err) {
		t.Fatalf("rolled-back oversized sidecar still exists at %s: %v", rolledBackPath, err)
	}
	var rolledBackTurns, rolledBackEvents int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.conversation_turns WHERE conversation_id=$1`, convID,
	).Scan(&rolledBackTurns); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.adaptive_outbox WHERE id=$1`, rollbackEvent.ID,
	).Scan(&rolledBackEvents); err != nil {
		t.Fatal(err)
	}
	if rolledBackTurns != 1 || rolledBackEvents != 0 {
		t.Fatalf("post-spill rollback counts turns=%d events=%d, want 1/0",
			rolledBackTurns, rolledBackEvents)
	}
}

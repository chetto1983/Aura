//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryBatchLive_AtomicRollbackAndReplay(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	actor := MemoryBatchActor{IdentityID: "batch-live-a", WriterRole: WriterParent}
	at := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	valid := MemoryBatchRequest{IdempotencyKey: "live-valid", Operations: []MemoryBatchOperation{
		memoryBatchTestUpsert("BatchLiveDavide", "likes", "Coffee", "live-run-1"),
	}}
	first, err := client.ApplyMemoryBatch(ctx, actor, valid, at)
	if err != nil {
		t.Fatalf("first batch: %v", err)
	}
	replay, err := client.ApplyMemoryBatch(ctx, actor, valid, at.Add(time.Minute))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if first.RequestHash == "" || replay.RequestHash != first.RequestHash || !replay.Replayed {
		t.Fatalf("first=%+v replay=%+v", first, replay)
	}

	invalid := MemoryBatchRequest{IdempotencyKey: "live-invalid", Operations: []MemoryBatchOperation{
		memoryBatchTestUpsert("BatchLiveDavide", "lives_in", "Caraglio", "live-run-2"),
		{Type: MemoryBatchForget, Forget: &ForgetFilter{Subject: "BatchLiveMissing"}},
	}}
	_, err = client.ApplyMemoryBatch(ctx, actor, invalid, at.Add(2*time.Minute))
	var batchErr *MemoryBatchError
	if !errors.As(err, &batchErr) || batchErr.Index != 1 || batchErr.Code != "target_not_found" {
		t.Fatalf("invalid batch error = %v", err)
	}
	hits, err := client.FactsAbout(ctx, "BatchLiveDavide", "", 10, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if len(hits) != 1 || hits[0].Object != "Coffee" {
		t.Fatalf("invalid batch leaked a partial write: %+v", hits)
	}
}

func TestMemoryBatchLive_ConcurrentBatchesConverge(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	actor := MemoryBatchActor{IdentityID: "batch-live-concurrent", WriterRole: WriterParent}
	at := time.Date(2026, 8, 31, 13, 0, 0, 0, time.UTC)
	const writers = 8
	var group sync.WaitGroup
	errorsByWriter := make(chan error, writers)
	for writer := range writers {
		group.Add(1)
		go func() {
			defer group.Done()
			request := MemoryBatchRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", writer),
				Operations: []MemoryBatchOperation{
					memoryBatchTestUpsert(
						"BatchConcurrentDavide", "likes", "Coffee", fmt.Sprintf("run-%d", writer)),
				},
			}
			_, err := client.ApplyMemoryBatch(ctx, actor, request, at.Add(time.Duration(writer)*time.Second))
			errorsByWriter <- err
		}()
	}
	group.Wait()
	close(errorsByWriter)
	for err := range errorsByWriter {
		if err != nil {
			t.Fatalf("concurrent batch: %v", err)
		}
	}
	hits, err := client.FactsAbout(ctx, "BatchConcurrentDavide", "likes", 10, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if len(hits) != 1 || len(hits[0].Sources) != writers {
		t.Fatalf("concurrent batches did not converge: %+v", hits)
	}
}

func TestMemoryBatchLive_IndependentObserverSeesOnlyCommittedState(t *testing.T) {
	client := disposableMemoryClient(t)
	actor := MemoryBatchActor{IdentityID: "batch-live-observer", WriterRole: WriterParent}
	persisted := make(chan struct{})
	release := make(chan struct{})
	backend := memoryBatchLiveBackend{
		base: clientMemoryBatchBackend{client: client},
		wrap: func(tx memoryBatchTransaction) memoryBatchTransaction {
			return &memoryBatchPausedPersist{memoryBatchTransaction: tx, persisted: persisted, release: release}
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := applyMemoryBatch(
			context.Background(), actor,
			MemoryBatchRequest{IdempotencyKey: "observer", Operations: []MemoryBatchOperation{
				memoryBatchTestUpsert("BatchObserver", "likes", "Coffee", "observer-run"),
			}},
			time.Date(2026, 8, 31, 14, 0, 0, 0, time.UTC), client.memoryLimits(), backend,
		)
		done <- err
	}()
	<-persisted
	hits, err := client.FactsAbout(context.Background(), "BatchObserver", "likes", 10, time.Time{})
	if err != nil {
		t.Fatalf("independent observer: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("independent observer saw uncommitted state: %+v", hits)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("batch commit: %v", err)
	}
	hits, err = client.FactsAbout(context.Background(), "BatchObserver", "likes", 10, time.Time{})
	if err != nil || len(hits) != 1 {
		t.Fatalf("committed state = %+v, err=%v", hits, err)
	}
}

func TestMemoryBatchLive_AmbiguousCommitReplaysOneLogicalEffect(t *testing.T) {
	client := disposableMemoryClient(t)
	var committed atomic.Bool
	backend := memoryBatchLiveBackend{
		base: clientMemoryBatchBackend{client: client},
		wrap: func(tx memoryBatchTransaction) memoryBatchTransaction {
			return &memoryBatchAmbiguousCommit{memoryBatchTransaction: tx, failAfterCommit: &committed}
		},
	}
	result, err := applyMemoryBatch(
		context.Background(),
		MemoryBatchActor{IdentityID: "batch-live-ambiguous", WriterRole: WriterParent},
		MemoryBatchRequest{IdempotencyKey: "ambiguous", Operations: []MemoryBatchOperation{
			memoryBatchTestUpsert("BatchAmbiguous", "likes", "Coffee", "ambiguous-run"),
		}},
		time.Date(2026, 8, 31, 15, 0, 0, 0, time.UTC), client.memoryLimits(), backend,
	)
	if err != nil || !result.Replayed {
		t.Fatalf("ambiguous replay = %+v, err=%v", result, err)
	}
	hits, err := client.FactsAbout(context.Background(), "BatchAmbiguous", "likes", 10, time.Time{})
	if err != nil || len(hits) != 1 || len(hits[0].Sources) != 1 {
		t.Fatalf("ambiguous logical effects = %+v, err=%v", hits, err)
	}
}

type memoryBatchLiveBackend struct {
	base memoryBatchBackend
	wrap func(memoryBatchTransaction) memoryBatchTransaction
}

func (b memoryBatchLiveBackend) Begin(ctx context.Context, identity string) (memoryBatchTransaction, error) {
	tx, err := b.base.Begin(ctx, identity)
	if err != nil {
		return nil, err
	}
	return b.wrap(tx), nil
}

type memoryBatchPausedPersist struct {
	memoryBatchTransaction
	persisted chan struct{}
	release   chan struct{}
}

func (tx *memoryBatchPausedPersist) Persist(ctx context.Context, before, after memoryBatchState) error {
	if err := tx.memoryBatchTransaction.Persist(ctx, before, after); err != nil {
		return err
	}
	close(tx.persisted)
	<-tx.release
	return nil
}

type memoryBatchAmbiguousCommit struct {
	memoryBatchTransaction
	failAfterCommit *atomic.Bool
}

func (tx *memoryBatchAmbiguousCommit) Commit(ctx context.Context) error {
	if err := tx.memoryBatchTransaction.Commit(ctx); err != nil {
		return err
	}
	if tx.failAfterCommit.CompareAndSwap(false, true) {
		return &ServerError{Status: http.StatusServiceUnavailable, Detail: "lost commit response"}
	}
	return nil
}

//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

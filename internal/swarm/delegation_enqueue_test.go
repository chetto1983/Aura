package swarm

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

func (s *fakeDelegationStore) CreateBatch(_ context.Context, reqs []documents.CreateIngestionJobRequest) ([]documents.IngestionJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created = append(s.created, reqs...)
	jobs := make([]documents.IngestionJob, len(reqs))
	for i, req := range reqs {
		jobs[i] = documents.IngestionJob{
			ID: req.IdempotencyKey, IdentityID: req.IdentityID, JobType: req.JobType,
		}
	}
	return jobs, nil
}

type recordingDelegationEnqueueStore struct {
	calls    int
	requests []documents.CreateIngestionJobRequest
	err      error
}

func (s *recordingDelegationEnqueueStore) CreateBatch(_ context.Context, reqs []documents.CreateIngestionJobRequest) ([]documents.IngestionJob, error) {
	s.calls++
	s.requests = append([]documents.CreateIngestionJobRequest(nil), reqs...)
	if s.err != nil {
		return nil, s.err
	}
	return make([]documents.IngestionJob, len(reqs)), nil
}

func TestEnqueueDelegationSubmitsOneCompleteBatch(t *testing.T) {
	store := &recordingDelegationEnqueueStore{}
	goals := []string{"first goal", "second goal", "third goal"}
	brief := DelegationPayload{ConversationID: "conv-1", ParentRunID: "run-1", Depth: 1}

	if _, err := EnqueueDelegation(context.Background(), &DelegationEnqueuer{Store: store}, "identity-1", goals, brief); err != nil {
		t.Fatalf("EnqueueDelegation: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("CreateBatch calls = %d, want 1", store.calls)
	}
	if len(store.requests) != len(goals) {
		t.Fatalf("batch requests = %d, want %d", len(store.requests), len(goals))
	}
	for i, req := range store.requests {
		if req.IdempotencyKey != delegationIdempotencyKey("identity-1", brief.ConversationID, brief.ParentRunID, i, goals[i]) {
			t.Fatalf("request %d idempotency key = %q, want deterministic per-goal key", i, req.IdempotencyKey)
		}
		if got, _ := req.Payload["goal"].(string); got != goals[i] {
			t.Fatalf("request %d goal = %q, want %q", i, got, goals[i])
		}
	}
}

func TestEnqueueDelegationReturnsBatchFailureWithoutResult(t *testing.T) {
	store := &recordingDelegationEnqueueStore{err: errors.New("injected middle insert failure")}

	result, err := EnqueueDelegation(context.Background(), &DelegationEnqueuer{Store: store}, "identity-1",
		[]string{"first goal", "second goal", "third goal"}, DelegationPayload{ConversationID: "conv-1", ParentRunID: "run-1"})
	if err == nil {
		t.Fatal("EnqueueDelegation error = nil, want batch failure")
	}
	if result != "" {
		t.Fatalf("EnqueueDelegation result = %q, want empty on batch failure", result)
	}
	if store.calls != 1 || len(store.requests) != 3 {
		t.Fatalf("batch call = %d with %d requests, want one call with all 3", store.calls, len(store.requests))
	}
}

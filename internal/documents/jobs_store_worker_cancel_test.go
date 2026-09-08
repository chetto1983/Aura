package documents

import (
	"strings"
	"testing"
)

func TestQueuedWorkerCancellationRejectsInvalidTargetsBeforeDatabase(t *testing.T) {
	valid := QueuedWorkerCancellationRequest{
		IdentityID: "11111111-1111-1111-1111-111111111111", JobID: "22222222-2222-2222-2222-222222222222",
		ConversationID: "conversation", ChildID: "child",
	}
	var store *PostgresIngestionJobStore
	for _, tc := range []struct {
		name   string
		mutate func(*QueuedWorkerCancellationRequest)
		want   string
	}{
		{"owner", func(r *QueuedWorkerCancellationRequest) { r.IdentityID = "bad" }, "identity"},
		{"job", func(r *QueuedWorkerCancellationRequest) { r.JobID = "bad" }, "job"},
		{"child", func(r *QueuedWorkerCancellationRequest) { r.ChildID = "" }, "requires"},
		{"conversation", func(r *QueuedWorkerCancellationRequest) { r.ConversationID = "" }, "requires"},
		{"negative attempt", func(r *QueuedWorkerCancellationRequest) { r.AttemptCount = -1 }, "requires"},
		{"overflow attempt", func(r *QueuedWorkerCancellationRequest) { r.AttemptCount = 1 << 31 }, "requires"},
		{"unconfigured store", func(*QueuedWorkerCancellationRequest) {}, "not configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := valid
			tc.mutate(&req)
			if err := store.RequestQueuedWorkerCancellation(t.Context(), req); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("invalid target error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestWorkerCancellationRejectsInvalidClaimFenceBeforeDatabase(t *testing.T) {
	valid := WorkerCancellationRequest{IdentityID: "11111111-1111-1111-1111-111111111111", JobID: "22222222-2222-2222-2222-222222222222", ChildID: "child", WorkerID: "owner", LeaseGeneration: 1}
	var store *PostgresIngestionJobStore
	for _, tc := range []struct {
		name   string
		mutate func(*WorkerCancellationRequest)
		want   string
	}{
		{"owner", func(r *WorkerCancellationRequest) { r.IdentityID = "bad" }, "identity"},
		{"job", func(r *WorkerCancellationRequest) { r.JobID = "bad" }, "job"},
		{"child", func(r *WorkerCancellationRequest) { r.ChildID = "" }, "requires"},
		{"unconfigured store", func(*WorkerCancellationRequest) {}, "not configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := valid
			tc.mutate(&req)
			if err := store.RequestWorkerCancellation(t.Context(), req); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("invalid target error = %v, want %q", err, tc.want)
			}
		})
	}
}

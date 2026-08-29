//go:build db_integration

package documents

import (
	"context"
	"testing"
	"time"
)

// jobs_store_delegation_test.go is ListDelegationJobs' own db_integration proof
// (51-11 Task 4, SWARM-10's missing leg) -- runs as aura_app via
// pipelineDisposablePool, never the superuser aura role, which would give a false
// green on the identity-scoping assertion below.

// seedDelegationJobRowForTest inserts one job_type=swarm_delegation row through
// the REAL Create path (never a bare pool.Exec) so the RLS-scoped withIdentity
// write this store uses everywhere else is exercised the same way here.
func seedDelegationJobRowForTest(t *testing.T, ctx context.Context, store *PostgresIngestionJobStore, identityID, conversationID, goal, childID string) {
	t.Helper()
	if _, err := store.Create(ctx, CreateIngestionJobRequest{
		IdentityID: identityID, JobType: delegationJobType, Status: "queued",
		IdempotencyKey: goal + "|" + childID, MaxAttempts: 3,
		Payload: map[string]any{"goal": goal, "conversation_id": conversationID, "child_id": childID, "fanout_key": "f-" + conversationID},
	}); err != nil {
		t.Fatalf("seed delegation job row: %v", err)
	}
	// Distinct created_at across inserts, so the newest-first assertion below is
	// deterministic rather than racing Postgres' own now() resolution.
	time.Sleep(2 * time.Millisecond)
}

// TestListDelegationJobsScopesToConversationAndIdentity proves ListDelegationJobs
// against a REAL Postgres connection: it returns ONLY the calling identity's rows
// whose payload names the given conversation, newest first, with Goal/ChildID
// already decoded out of payload JSONB -- a sibling conversation's row and
// another identity's row (even for the SAME conversation id) are both excluded.
func TestListDelegationJobsScopesToConversationAndIdentity(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx := context.Background()
	identityID := seedDocumentTestIdentity(t, ctx, pool)
	otherIdentityID := seedDocumentTestIdentity(t, ctx, pool)
	store := NewPostgresIngestionJobStore(pool)

	const conversationID = "conv-1"
	seedDelegationJobRowForTest(t, ctx, store, identityID, conversationID, "goal one", "w1")
	seedDelegationJobRowForTest(t, ctx, store, identityID, conversationID, "goal two", "w2")
	seedDelegationJobRowForTest(t, ctx, store, identityID, "conv-2", "unrelated conversation", "w3")
	seedDelegationJobRowForTest(t, ctx, store, otherIdentityID, conversationID, "other tenant", "w4")

	rows, err := store.ListDelegationJobs(ctx, identityID, conversationID, 10)
	if err != nil {
		t.Fatalf("ListDelegationJobs: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %#v, want 2 (scoped to this identity + this conversation)", rows)
	}
	if rows[0].ChildID != "w2" || rows[1].ChildID != "w1" {
		t.Fatalf("rows = [%s, %s], want [w2, w1] newest first", rows[0].ChildID, rows[1].ChildID)
	}
	if rows[0].Goal != "goal two" {
		t.Fatalf("rows[0].Goal = %q, want it decoded from payload", rows[0].Goal)
	}
	if rows[0].Status != "queued" {
		t.Fatalf("rows[0].Status = %q, want queued", rows[0].Status)
	}

	limited, err := store.ListDelegationJobs(ctx, identityID, conversationID, 1)
	if err != nil {
		t.Fatalf("ListDelegationJobs(limit=1): %v", err)
	}
	if len(limited) != 1 || limited[0].ChildID != "w2" {
		t.Fatalf("limited rows = %#v, want newest worker w2", limited)
	}

	target, found, err := store.FindDelegationJob(ctx, identityID, conversationID, "w1")
	if err != nil {
		t.Fatalf("FindDelegationJob: %v", err)
	}
	if !found || target.ChildID != "w1" {
		t.Fatalf("target = %#v, found=%v, want w1 outside the one-row list window", target, found)
	}
	if _, found, err := store.FindDelegationJob(ctx, identityID, conversationID, "w4"); err != nil || found {
		t.Fatalf("foreign worker found=%v err=%v, want hidden", found, err)
	}
}

// TestListDelegationJobsUnknownConversationReturnsEmpty pins the non-error
// empty-result contract: a conversation with no delegation jobs yet is an empty
// slice, never an error.
func TestListDelegationJobsUnknownConversationReturnsEmpty(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx := context.Background()
	identityID := seedDocumentTestIdentity(t, ctx, pool)
	store := NewPostgresIngestionJobStore(pool)

	rows, err := store.ListDelegationJobs(ctx, identityID, "conv-never-seen", 10)
	if err != nil {
		t.Fatalf("ListDelegationJobs: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want empty", rows)
	}
}

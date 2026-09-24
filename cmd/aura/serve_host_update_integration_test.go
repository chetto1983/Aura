//go:build db_integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

const hostUpdateLocalID = "00000000-0000-0000-0000-000000000001"

func seedHostUpdateConversation(t *testing.T, ctx context.Context, activeAt time.Time) string {
	t.Helper()
	pool := bootstrapMigratedPool(t)
	convID := uuid.Must(uuid.NewV7()).String()
	if err := db.WithIdentityTxRaw(ctx, pool, hostUpdateLocalID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO aura.conversations (id, identity_id, model, status, last_active_at) VALUES ($1, $2, 'test-model', 'active', $3)",
			convID, hostUpdateLocalID, activeAt)
		return err
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	return convID
}

// Conversations are owner-only (migration 0089): read from the daemon pool with no identity,
// the newest activity is NULL, and a chat that never calls a tool would look idle while
// someone is typing. The source must read each identity's own rows.
func TestHostUpdateActivitySeesAChatThroughRowSecurity(t *testing.T) {
	ctx := context.Background()
	pool := bootstrapMigratedPool(t)
	marker := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	seedHostUpdateConversation(t, ctx, marker)

	last, _, err := hostUpdateActivity{pool: pool, identities: identity.New(pool)}.Activity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !last.Equal(marker) {
		t.Fatalf("last activity = %v, want the seeded conversation's %v", last, marker)
	}
}

func TestHostUpdateActivityCountsAToolCallUntilItEnds(t *testing.T) {
	ctx := context.Background()
	pool := bootstrapMigratedPool(t)
	convID := seedHostUpdateConversation(t, ctx, time.Now())
	src := hostUpdateActivity{pool: pool, identities: identity.New(pool)}
	store := toolinvocations.New(pool)
	owner := identityctx.WithIdentityID(ctx, hostUpdateLocalID)

	_, before, err := src.Activity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.Must(uuid.NewV7()).String()
	startedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := store.Insert(owner, toolinvocations.Event{
		ConversationID: convID, RequestID: requestID, ToolCallID: "call-update-1", ToolName: "shell_exec",
		Event: toolinvocations.EventStart, Seq: 1, StartedAt: startedAt, Arguments: "{}", ArgsBytes: 2,
	}); err != nil {
		t.Fatalf("insert start: %v", err)
	}
	last, during, err := src.Activity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if during != before+1 || last.Before(startedAt) {
		t.Fatalf("while running: live=%d (before %d) last=%v, want one more run and activity at or after %v", during, before, last, startedAt)
	}

	exitCode := 0
	if err := store.Insert(owner, toolinvocations.Event{
		ConversationID: convID, RequestID: requestID, ToolCallID: "call-update-1", ToolName: "shell_exec",
		Event: toolinvocations.EventEnd, Seq: 2, StartedAt: startedAt, EndedAt: startedAt.Add(time.Second),
		DurationMS: 1000, Status: "ok", ResultPreview: "ok", PreviewBytes: 2, ResultBytes: 2, ExitCode: &exitCode,
		Meta: map[string]any{"exit_code": 0, "timed_out": false},
	}); err != nil {
		t.Fatalf("insert end: %v", err)
	}
	if _, after, err := src.Activity(ctx); err != nil || after != before {
		t.Fatalf("after the end: live=%d err=%v, want %d", after, err, before)
	}
}

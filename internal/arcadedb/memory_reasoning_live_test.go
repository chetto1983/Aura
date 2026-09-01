//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func reasoningLiveClient(t *testing.T) *Client {
	t.Helper()
	client := disposableArcadeClient(t)
	if err := client.EnsureMemorySchema(context.Background()); err != nil {
		t.Fatalf("reasoning schema: %v", err)
	}
	return client
}

func storeLiveReasoningTrace(
	t *testing.T,
	client *Client,
	identityID, traceID, conversationID string,
	status ReasoningStatus,
	terminal time.Time,
) ReasoningTrace {
	t.Helper()
	trace := validReasoningTrace()
	trace.IdentityID, trace.TraceID, trace.ConversationID = identityID, traceID, conversationID
	trace.SourceRef = fmt.Sprintf("postgres://aura/conversations/%s/turns/7", conversationID)
	trace.Status, trace.TerminalAt, trace.ExpiresAt = status, terminal, time.Time{}
	trace.CreatedAt = terminal.Add(-time.Minute)
	trace.Steps[0].CreatedAt = trace.CreatedAt
	trace.Steps[0].ToolCalls[0].CallID = "call-" + traceID
	trace.Steps[0].ToolCalls[0].SourceRef = trace.SourceRef
	trace.Steps[0].ToolCalls[0].EntityRefs = nil
	if err := client.UpsertReasoningTrace(context.Background(), trace); err != nil {
		t.Fatalf("UpsertReasoningTrace(%s): %v", traceID, err)
	}
	return trace
}

func assertLiveReasoningTraceAbsent(t *testing.T, client *Client, identityID, traceID string) {
	t.Helper()
	for _, typeName := range []string{reasoningTraceType, reasoningStepType, reasoningToolCallType} {
		rows, err := client.Query(context.Background(),
			"SELECT count(*) AS n FROM "+typeName+" WHERE identity_id = :identity_id AND trace_id = :trace_id",
			map[string]any{"identity_id": identityID, "trace_id": traceID})
		if err != nil {
			t.Fatalf("count %s: %v", typeName, err)
		}
		if len(rows) != 1 || rowInt(rows[0], "n") != 0 {
			t.Fatalf("%s survived deletion: %+v", typeName, rows)
		}
	}
}

func TestReasoningGraphLive_DeletionPrecedence(t *testing.T) {
	client := reasoningLiveClient(t)
	now := time.Now().UTC()
	conversation := storeLiveReasoningTrace(t, client, "identity-a", "trace-conversation", "conversation-a",
		ReasoningStatusSucceeded, now)
	storeLiveReasoningTrace(t, client, "identity-b", "trace-foreign", "conversation-foreign",
		ReasoningStatusSucceeded, now)

	deleted, err := client.DeleteReasoningBySource(context.Background(), ReasoningDeleteSelector{
		IdentityID: "identity-a", ConversationID: conversation.ConversationID,
	})
	if err != nil || deleted != 1 {
		t.Fatalf("conversation delete count=%d err=%v", deleted, err)
	}
	assertLiveReasoningTraceAbsent(t, client, "identity-a", conversation.TraceID)
	if _, found, err := client.GetReasoningTrace(context.Background(), "identity-b", "trace-foreign"); err != nil || !found {
		t.Fatalf("foreign reasoning altered: found=%v err=%v", found, err)
	}
	if repeated, err := client.DeleteReasoningBySource(context.Background(), ReasoningDeleteSelector{
		IdentityID: "identity-a", ConversationID: conversation.ConversationID,
	}); err != nil || repeated != 0 {
		t.Fatalf("repeated conversation delete count=%d err=%v", repeated, err)
	}

	source := storeLiveReasoningTrace(t, client, "identity-a", "trace-source", "conversation-source",
		ReasoningStatusFailed, now)
	if deleted, err := client.DeleteReasoningBySource(context.Background(), ReasoningDeleteSelector{
		IdentityID: "identity-a", SourceRef: source.SourceRef,
	}); err != nil || deleted != 1 {
		t.Fatalf("source delete count=%d err=%v", deleted, err)
	}
	assertLiveReasoningTraceAbsent(t, client, "identity-a", source.TraceID)

	operator := storeLiveReasoningTrace(t, client, "identity-a", "trace-operator", "conversation-operator",
		ReasoningStatusCancelled, now)
	if deleted, err := client.DeleteReasoningBySource(context.Background(), ReasoningDeleteSelector{
		IdentityID: "identity-a", TraceID: operator.TraceID,
	}); err != nil || deleted != 1 {
		t.Fatalf("operator delete count=%d err=%v", deleted, err)
	}
	if repeated, err := client.DeleteReasoningBySource(context.Background(), ReasoningDeleteSelector{
		IdentityID: "identity-a", TraceID: operator.TraceID,
	}); err != nil || repeated != 0 {
		t.Fatalf("repeated operator delete count=%d err=%v", repeated, err)
	}

	identity := storeLiveReasoningTrace(t, client, "identity-delete", "trace-identity", "conversation-identity",
		ReasoningStatusSucceeded, now)
	if deleted, err := client.DeleteIdentityReasoning(context.Background(), identity.IdentityID); err != nil || deleted != 1 {
		t.Fatalf("identity delete count=%d err=%v", deleted, err)
	}
	assertLiveReasoningTraceAbsent(t, client, identity.IdentityID, identity.TraceID)
}

func TestReasoningGraphLive_ExpiryDeleteRace(t *testing.T) {
	client := reasoningLiveClient(t)
	now := time.Now().UTC()
	trace := storeLiveReasoningTrace(t, client, "identity-race", "trace-race", "conversation-race",
		ReasoningStatusFailed, now.Add(-8*24*time.Hour))
	if _, err := client.Command(context.Background(),
		"DELETE FROM HAS_STEP WHERE outV().identity_id = :identity_id AND outV().trace_id = :trace_id",
		map[string]any{"identity_id": trace.IdentityID, "trace_id": trace.TraceID}); err != nil {
		t.Fatalf("create partial graph fixture: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := client.DeleteExpiredReasoning(context.Background(), trace.IdentityID, now, 1)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := client.DeleteReasoningBySource(context.Background(), ReasoningDeleteSelector{
			IdentityID: trace.IdentityID, TraceID: trace.TraceID,
		})
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("expiry/delete race: %v", err)
		}
	}
	assertLiveReasoningTraceAbsent(t, client, trace.IdentityID, trace.TraceID)
}

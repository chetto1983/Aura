//go:build arcadedb_integration

package arcadedb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type reasoningReadCounter struct {
	base  http.RoundTripper
	mu    sync.Mutex
	reads int
}

func (c *reasoningReadCounter) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		var payload struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(body, &payload) == nil && strings.HasPrefix(strings.TrimSpace(payload.Command), "SELECT") {
			for _, token := range []string{reasoningTraceType, reasoningStepType, reasoningToolCallType} {
				if strings.Contains(payload.Command, token) {
					c.mu.Lock()
					c.reads++
					c.mu.Unlock()
					break
				}
			}
		}
	}
	return c.base.RoundTrip(request)
}

func (c *reasoningReadCounter) reset() {
	c.mu.Lock()
	c.reads = 0
	c.mu.Unlock()
}

func (c *reasoningReadCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

func observeReasoningReads(client *Client) *reasoningReadCounter {
	base := client.http.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	counter := &reasoningReadCounter{base: base}
	client.http.Transport = counter
	return counter
}

func reasoningLiveClient(t *testing.T) *Client {
	t.Helper()
	if strings.TrimSpace(os.Getenv("ARCADEDB_URL")) == "" {
		t.Fatal("ARCADEDB_URL is required: reasoning lifecycle evidence must not skip green")
	}
	if strings.TrimSpace(os.Getenv("ARCADEDB_PASSWORD")) == "" {
		t.Fatal("ARCADEDB_PASSWORD is required: reasoning lifecycle evidence must authenticate live")
	}
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

func TestReasoningGraphLive_ExplicitIsolation(t *testing.T) {
	client := reasoningLiveClient(t)
	now := time.Now().UTC()
	trace := storeLiveReasoningTrace(t, client, "identity-owner", "trace-explicit", "conversation-explicit",
		ReasoningStatusSucceeded, now)
	projection := ConversationProjection{
		IdentityID: trace.IdentityID, ConversationID: trace.ConversationID,
		Turns: []ConversationTurnProjection{
			{IdentityID: trace.IdentityID, ConversationID: trace.ConversationID, Seq: 1, Role: "user",
				Content: "ordinary isolation proof", ContentHash: conversationContentHash("ordinary isolation proof"),
				OccurredAt: now.Add(-3 * time.Minute), SourceRef: trace.SourceRef + "/user"},
			{IdentityID: trace.IdentityID, ConversationID: trace.ConversationID, Seq: 2, Role: "assistant",
				Content: "public answer", ContentHash: conversationContentHash("public answer"),
				OccurredAt: now.Add(-2 * time.Minute), SourceRef: trace.SourceRef},
			{IdentityID: trace.IdentityID, ConversationID: trace.ConversationID, Seq: 3, Role: "user",
				Content: "follow up", ContentHash: conversationContentHash("follow up"),
				OccurredAt: now.Add(-time.Minute), SourceRef: trace.SourceRef + "/follow"},
		},
	}
	if err := client.ApplyConversationProjection(context.Background(), projection); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}
	counter := observeReasoningReads(client)
	counter.reset()

	ordinary := []RecallRequest{
		{IdentityID: trace.IdentityID, Mode: RecallModeSemantic, Query: "ordinary isolation proof", Limit: 2},
		{IdentityID: trace.IdentityID, Mode: RecallModeRecent, Limit: 2},
		{IdentityID: trace.IdentityID, Mode: RecallModeOpen, ConversationID: trace.ConversationID,
			AnchorSeq: 1, Direction: RecallDirectionAfter, Limit: 2},
	}
	var cursor string
	for index, request := range ordinary {
		result, err := client.RecallMemory(context.Background(), request)
		if err != nil {
			t.Fatalf("ordinary recall %s: %v", request.Mode, err)
		}
		if result.Retrieval.ReasoningCount != 0 {
			t.Fatalf("ordinary recall %s returned reasoning: %+v", request.Mode, result.Retrieval)
		}
		if index == len(ordinary)-1 {
			cursor = result.NextCursor
		}
	}
	decoded, err := decodeRecallCursor(cursor)
	if err != nil {
		t.Fatalf("decode open cursor: %v", err)
	}
	if _, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: trace.IdentityID, Mode: RecallModeScroll, ConversationID: decoded.ConversationID,
		AnchorSeq: decoded.AnchorSeq, Direction: decoded.Direction, Limit: decoded.PageSize, Cursor: cursor,
	}); err != nil {
		t.Fatalf("ordinary scroll recall: %v", err)
	}
	if reads := counter.count(); reads != 0 {
		t.Fatalf("ordinary semantic/recent/open/scroll made %d reasoning reads", reads)
	}

	owner, err := client.SearchReasoningTraces(context.Background(), trace.IdentityID, "deployment", 1)
	if err != nil || len(owner) != 1 || owner[0].TraceID != trace.TraceID {
		t.Fatalf("explicit owner reasoning = %+v err=%v", owner, err)
	}
	if reads := counter.count(); reads == 0 {
		t.Fatal("explicit reasoning action made zero reasoning reads")
	}
	foreign, err := client.SearchReasoningTraces(context.Background(), "identity-foreign", "deployment", 1)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("foreign explicit reasoning = %+v err=%v", foreign, err)
	}
}

func TestReasoningGraphLive_FailedCancelledRetention(t *testing.T) {
	client := reasoningLiveClient(t)
	terminal := time.Now().UTC()
	failed := storeLiveReasoningTrace(t, client, "identity-terminal", "trace-failed", "conversation-failed",
		ReasoningStatusFailed, terminal)
	cancelled := storeLiveReasoningTrace(t, client, "identity-terminal", "trace-cancelled", "conversation-cancelled",
		ReasoningStatusCancelled, terminal)
	counter := observeReasoningReads(client)
	counter.reset()
	if _, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: failed.IdentityID, Mode: RecallModeRecent, Limit: 2,
	}); err != nil {
		t.Fatalf("ordinary recall with terminal traces: %v", err)
	}
	if reads := counter.count(); reads != 0 {
		t.Fatalf("terminal traces entered ordinary context through %d reasoning reads", reads)
	}
	if deleted, err := client.DeleteExpiredReasoning(context.Background(), failed.IdentityID,
		terminal.Add(6*24*time.Hour), 10); err != nil || deleted != 0 {
		t.Fatalf("terminal traces expired early: deleted=%d err=%v", deleted, err)
	}
	for _, trace := range []ReasoningTrace{failed, cancelled} {
		if _, found, err := client.GetReasoningTrace(context.Background(), trace.IdentityID, trace.TraceID); err != nil || !found {
			t.Fatalf("trace %s absent before 7d: found=%v err=%v", trace.TraceID, found, err)
		}
	}
	if deleted, err := client.DeleteExpiredReasoning(context.Background(), failed.IdentityID,
		terminal.Add(8*24*time.Hour), 10); err != nil || deleted != 2 {
		t.Fatalf("terminal traces did not expire after 7d: deleted=%d err=%v", deleted, err)
	}
	assertLiveReasoningTraceAbsent(t, client, failed.IdentityID, failed.TraceID)
	assertLiveReasoningTraceAbsent(t, client, cancelled.IdentityID, cancelled.TraceID)
}

type reasoningGrowthMetrics struct {
	databaseBytes int64
	recordBytes   int64
	vertices      int64
	edges         int64
	indexEntries  int64
}

func readReasoningGrowthMetrics(t *testing.T, client *Client) reasoningGrowthMetrics {
	t.Helper()
	var metrics reasoningGrowthMetrics
	rows, err := client.Query(context.Background(), "SELECT size FROM schema:database", nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("read schema database size: rows=%v err=%v", rows, err)
	}
	metrics.databaseBytes = rowInt(rows[0], "size")
	for _, typeName := range []string{reasoningTraceType, reasoningStepType, reasoningToolCallType} {
		rows, err = client.Query(context.Background(), "SELECT FROM "+typeName, nil)
		if err != nil {
			t.Fatalf("measure reasoning vertex %s: rows=%v err=%v", typeName, rows, err)
		}
		metrics.vertices += int64(len(rows))
		for _, row := range rows {
			encoded, marshalErr := json.Marshal(row)
			if marshalErr != nil {
				t.Fatalf("encode reasoning vertex %s: %v", typeName, marshalErr)
			}
			metrics.recordBytes += int64(len(encoded))
		}
	}
	for _, typeName := range []string{"INITIATED_BY", "HAS_STEP", "NEXT", "INVOKED", "TOUCHED"} {
		rows, err = client.Query(context.Background(), "SELECT FROM "+typeName, nil)
		if err != nil {
			t.Fatalf("measure reasoning edge %s: rows=%v err=%v", typeName, rows, err)
		}
		metrics.edges += int64(len(rows))
		for _, row := range rows {
			encoded, marshalErr := json.Marshal(row)
			if marshalErr != nil {
				t.Fatalf("encode reasoning edge %s: %v", typeName, marshalErr)
			}
			metrics.recordBytes += int64(len(encoded))
		}
	}
	for _, indexName := range []string{
		"ReasoningTrace[identity_id,trace_id]",
		"ReasoningTrace[identity_id,source_ref]",
		"ReasoningTrace[identity_id,status]",
		"ReasoningTrace[expires_at]",
		"ReasoningStep[identity_id,trace_id,step_index]",
		"ReasoningToolCall[identity_id,trace_id,call_id]",
	} {
		rows, err = client.Query(context.Background(), "SELECT FROM INDEX:`"+indexName+"`", nil)
		if err != nil {
			t.Fatalf("measure reasoning index %s: %v", indexName, err)
		}
		metrics.indexEntries += int64(len(rows))
	}
	return metrics
}

func TestReasoningGraphLive_GrowthEvidence(t *testing.T) {
	client := reasoningLiveClient(t)
	ctx := context.Background()
	before := readReasoningGrowthMetrics(t, client)
	terminal := time.Now().UTC()
	conversationID := "conversation-growth"
	if err := client.ApplyConversationProjection(ctx, ConversationProjection{
		IdentityID: "identity-growth", ConversationID: conversationID,
		Turns: []ConversationTurnProjection{{
			IdentityID: "identity-growth", ConversationID: conversationID, Seq: 7,
			Role: "assistant", Content: "public growth answer",
			ContentHash: conversationContentHash("public growth answer"), OccurredAt: terminal,
			SourceRef: "postgres://aura/conversations/conversation-growth/turns/7",
		}},
	}); err != nil {
		t.Fatalf("store growth source turn: %v", err)
	}
	if _, err := client.Command(ctx, upsertEntityStatement, map[string]any{"name": "deployment-a"}); err != nil {
		t.Fatalf("store growth touched entity: %v", err)
	}
	trace := validReasoningTrace()
	trace.IdentityID, trace.TraceID, trace.ConversationID = "identity-growth", "trace-growth", conversationID
	trace.SourceRef = "postgres://aura/conversations/conversation-growth/turns/7"
	trace.CreatedAt, trace.TerminalAt = terminal.Add(-time.Minute), terminal
	trace.Steps[0].CreatedAt = trace.CreatedAt
	trace.Steps[0].ToolCalls[0].SourceRef = trace.SourceRef
	second := trace.Steps[0]
	second.Index, second.ProviderSummary, second.CreatedAt = 2, "Verified the deployment result.", terminal
	second.ToolCalls = append([]ReasoningToolCall(nil), second.ToolCalls...)
	second.ToolCalls[0].CallID = "call-growth-2"
	trace.Steps = append(trace.Steps, second)
	if err := client.UpsertReasoningTrace(ctx, trace); err != nil {
		t.Fatalf("store growth trace: %v", err)
	}
	after := readReasoningGrowthMetrics(t, client)
	delta := reasoningGrowthMetrics{
		databaseBytes: after.databaseBytes - before.databaseBytes,
		recordBytes:   after.recordBytes - before.recordBytes,
		vertices:      after.vertices - before.vertices,
		edges:         after.edges - before.edges,
		indexEntries:  after.indexEntries - before.indexEntries,
	}
	if delta.recordBytes <= 0 || delta.vertices != 5 || delta.edges != 8 || delta.indexEntries <= 0 {
		t.Fatalf("unexpected reasoning growth delta: %+v", delta)
	}
	t.Logf("REASONING_GROWTH_EVIDENCE database_bytes=%d record_bytes=%d vertices=%d edges=%d index_entries=%d",
		delta.databaseBytes, delta.recordBytes, delta.vertices, delta.edges, delta.indexEntries)
}

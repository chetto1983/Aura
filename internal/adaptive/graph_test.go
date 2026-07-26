package adaptive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type graphWriteCall struct {
	query  string
	params map[string]any
}

type recordingGraphWriter struct {
	calls []graphWriteCall
	rows  []map[string]any
}

func (r *recordingGraphWriter) Write(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	r.calls = append(r.calls, graphWriteCall{query: query, params: params})
	return r.rows, nil
}

func TestGraphStoreReusesMemoryUserButKeepsAdaptiveNodesPrivate(t *testing.T) {
	writer := &recordingGraphWriter{rows: []map[string]any{{"id": "event-1"}}}
	graph := NewGraphStore(writer)
	owner := uuid.Must(uuid.NewV7())
	eventID := uuid.Must(uuid.NewV7())
	record := OutboxRecord{
		ID: eventID, OwnerID: owner, AggregateID: "request-1", Sequence: 1,
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventDecision,
		Payload:     []byte(`{"schema_version":"1.0","domain":"tool"}`),
		PayloadHash: make([]byte, 32), Status: "leased", Attempts: 1,
		CreatedAt: time.Now().UTC(),
	}
	if err := graph.Project(context.Background(), record); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if len(writer.calls) != 1 {
		t.Fatalf("writes = %d, want 1", len(writer.calls))
	}
	query := writer.calls[0].query
	for _, required := range []string{":User", ":AdaptiveEpisode", ":AdaptiveEvent", "HAS_ADAPTIVE_EPISODE"} {
		if !strings.Contains(query, required) {
			t.Fatalf("projection query missing %q:\n%s", required, query)
		}
	}
	if strings.Contains(query, "Preference") || strings.Contains(query, "Fact") || strings.Contains(query, "ReasoningTrace") {
		t.Fatalf("adaptive projection leaked into LLM-facing agent-memory labels:\n%s", query)
	}
	if got := writer.calls[0].params["owner_id"]; got != owner.String() {
		t.Fatalf("owner_id = %v, want %s", got, owner)
	}

	if err := graph.PurgeOwner(context.Background(), owner); err != nil {
		t.Fatalf("PurgeOwner: %v", err)
	}
	purge := writer.calls[1].query
	if strings.Contains(purge, "DELETE u") || strings.Contains(purge, "DETACH DELETE u") {
		t.Fatalf("adaptive purge must not delete the shared agent-memory User: %s", purge)
	}
}

func TestGraphStoreProjectUsesAgentMemoryCompatibleUserUpsert(t *testing.T) {
	writer := &recordingGraphWriter{rows: []map[string]any{{"id": "event-1"}}}
	record := OutboxRecord{
		ID: uuid.Must(uuid.NewV7()), OwnerID: uuid.Must(uuid.NewV7()),
		AggregateID: "request-compatible-user", Sequence: 1,
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventDecision,
		Payload:     []byte(`{"schema_version":"1.0","domain":"tool"}`),
		PayloadHash: make([]byte, 32), CreatedAt: time.Now().UTC(),
	}

	if err := NewGraphStore(writer).Project(context.Background(), record); err != nil {
		t.Fatalf("Project: %v", err)
	}
	query := writer.calls[0].query
	for _, required := range []string{
		"ON CREATE SET\n  u.id = $owner_id,\n  u.created_at = datetime($created_at),\n  u.attributes_json = '{}'",
		"ON MATCH SET\n  u.id = coalesce(u.id, $owner_id),\n  u.created_at = coalesce(u.created_at, datetime($created_at)),\n  u.attributes_json = coalesce(u.attributes_json, '{}')",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("agent-memory-compatible User upsert missing %q:\n%s", required, query)
		}
	}
}

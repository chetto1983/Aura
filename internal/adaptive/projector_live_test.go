//go:build adaptive_live && db_integration

package adaptive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/google/uuid"
)

type liveMemoryWriteEvidence struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

type liveMemoryRecallEvidence struct {
	Context        string `json:"context"`
	HasContext     bool   `json:"has_context"`
	Error          string `json:"error"`
	RecallMetadata struct {
		Results           json.RawMessage `json:"results"`
		CorpusEpochBefore *uint64         `json:"corpus_epoch_before"`
		CorpusEpochAfter  *uint64         `json:"corpus_epoch_after"`
		Coherent          bool            `json:"coherent"`
	} `json:"recall_metadata"`
}

type liveMemoryRecallResult struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Order int    `json:"order"`
}

func TestAdaptiveProjectorLiveReusesMemoryUserWithoutMemoryRecallLeak(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	password := os.Getenv("NEO4J_PASSWORD")
	if password == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("adaptive live test requires NEO4J_PASSWORD under CI")
		}
		t.Skip("adaptive live test requires NEO4J_PASSWORD")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client, err := knowledge.Open(ctx, &knowledge.Config{
		BoltURL: envOrValue("AURA_NEO4J_BOLT_URL", "bolt://127.0.0.1:7687"),
		User:    "neo4j", Password: password,
		Database: "neo4j", MCPBinary: envOrValue("AURA_MCP_NEO4J_CYPHER_BIN", "mcp-neo4j-cypher"),
		ConnectTimeoutSec: 15,
	})
	if err != nil {
		t.Fatalf("open live Neo4j MCP client: %v", err)
	}
	defer func() { _ = client.Close() }()

	owner := uuid.Must(uuid.NewV7())
	adaptiveMarker := "ADAPTIVE-PRIVATE-" + owner.String()
	memoryMarker := "MEMORY-EVIDENCE-" + owner.String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner, "adaptive-live-"+owner.String()); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	graph := NewGraphStore(client)
	preferenceID := ""
	defer func() {
		_ = graph.PurgeOwner(context.Background(), owner)
		if preferenceID != "" {
			_, _ = client.Write(context.Background(), `
MATCH (u:User {identifier:$owner_id})-[owns:HAS_PREFERENCE]->
      (preference:Preference {id:$preference_id})
DELETE owns
DETACH DELETE preference
`, map[string]any{
				"owner_id": owner.String(), "preference_id": preferenceID,
			})
		}
		_, _ = client.Write(context.Background(),
			`MATCH (u:User {identifier:$owner_id}) WHERE NOT (u)--() DETACH DELETE u`,
			map[string]any{"owner_id": owner.String()})
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	}()

	store := NewStore(pool, StoreConfig{})
	event, err := newLegacyEvent(eventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: uuid.Must(uuid.NewV7()).String(),
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventDecision,
		Payload: []byte(fmt.Sprintf(
			`{"schema_version":"1.0","domain":"tool","private_marker":%q}`, adaptiveMarker,
		)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.recordLegacyEvent(ctx, event); err != nil {
		t.Fatalf("Record: %v", err)
	}
	records, err := store.ListAggregate(ctx, owner, event.AggregateID)
	if err != nil || len(records) != 1 {
		t.Fatalf("ListAggregate = %d records, %v", len(records), err)
	}
	projector := NewProjector(store, graph, ProjectorConfig{WorkerID: "adaptive-live"})
	didWork, err := projector.ProjectOne(ctx)
	if err != nil || !didWork {
		t.Fatalf("ProjectOne = (%t,%v)", didWork, err)
	}
	rows, err := client.Read(ctx, `
MATCH (u:User {identifier:$owner_id})-[:HAS_ADAPTIVE_EPISODE]->
      (:AdaptiveEpisode)-[:HAS_ADAPTIVE_EVENT]->(event:AdaptiveEvent {id:$event_id})
RETURN event.id AS id, u.id AS user_id, u.created_at AS created_at,
       u.attributes_json AS attributes_json
`, map[string]any{"owner_id": owner.String(), "event_id": event.ID.String()})
	if err != nil {
		t.Fatalf("read projected graph: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("projected graph rows = %d, want 1", len(rows))
	}
	for _, field := range []string{"user_id", "created_at", "attributes_json"} {
		if rows[0][field] == nil {
			t.Fatalf("project-first User.%s is null: %v", field, rows[0])
		}
	}

	endpoint := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_URL"))
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8091/mcp/"
	}
	memory, err := mcp.OpenServer(ctx, "adaptive-memory-isolation", mcp.ManagedServer{
		Type: mcp.ServerTypeStreamableHTTP, URL: endpoint,
		Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	})
	if err != nil {
		t.Fatalf("open agent-memory MCP: %v", err)
	}
	defer func() {
		_ = memory.Close()
		http.DefaultClient.CloseIdleConnections()
	}()
	writeText, err := memory.CallTool(ctx, "memory_add_preference", map[string]any{
		"user_identifier": owner.String(),
		"category":        "adaptive_projector_live",
		"preference":      memoryMarker,
		"context":         "Task 9 compatible User projection proof",
		"metadata": map[string]any{
			"source": "aura-task9-live", "test_owner_id": owner.String(),
		},
	})
	if err != nil {
		t.Fatalf("owner-scoped memory_add_preference: %v", err)
	}
	var writeEvidence liveMemoryWriteEvidence
	if err := json.Unmarshal([]byte(writeText), &writeEvidence); err != nil {
		t.Fatalf("decode memory write evidence: %v: %s", err, writeText)
	}
	if writeEvidence.Error != "" {
		t.Fatalf("memory write evidence = %+v", writeEvidence)
	}
	if parsed, err := uuid.Parse(writeEvidence.ID); err != nil || parsed == uuid.Nil {
		t.Fatalf("memory write returned invalid preference ID %q", writeEvidence.ID)
	}
	preferenceID = writeEvidence.ID

	beforeRecall, beforeResults := callLiveLongTermRecall(
		t, ctx, memory, owner, memoryMarker,
	)
	if !containsLiveRecallResult(beforeResults, preferenceID) {
		t.Fatalf("returned memory evidence omits preference %s: %+v", preferenceID, beforeResults)
	}
	if strings.Contains(beforeRecall.Context, adaptiveMarker) ||
		strings.Contains(beforeRecall.Context, event.ID.String()) {
		t.Fatalf("private adaptive event leaked through agent-memory context: %s", beforeRecall.Context)
	}

	agentMemoryAttributes := `{"source":"agent-memory","preserve":true}`
	if _, err := client.Write(ctx, `
MATCH (u:User {identifier:$owner_id})
SET u.id = null,
    u.created_at = null,
    u.attributes_json = $attributes_json
`, map[string]any{
		"owner_id": owner.String(), "attributes_json": agentMemoryAttributes,
	}); err != nil {
		t.Fatalf("prepare legacy-null User repair: %v", err)
	}
	if err := graph.Project(ctx, records[0]); err != nil {
		t.Fatalf("idempotent reproject: %v", err)
	}

	afterRecall, afterResults := callLiveLongTermRecall(
		t, ctx, memory, owner, memoryMarker,
	)
	requireLiveRecallUnchanged(
		t, "reproject", beforeRecall, beforeResults, afterRecall, afterResults,
	)

	survivors, err := client.Read(ctx, `
MATCH (u:User {identifier:$owner_id})-[:HAS_ADAPTIVE_EPISODE]->
      (:AdaptiveEpisode)-[:HAS_ADAPTIVE_EVENT]->(event:AdaptiveEvent {id:$event_id})
MATCH (u)-[:HAS_PREFERENCE]->(preference:Preference {id:$preference_id})
RETURN u.id AS user_id, u.created_at AS created_at,
       u.attributes_json AS attributes_json,
       event.id AS event_id, preference.id AS preference_id
`, map[string]any{
		"owner_id": owner.String(), "event_id": event.ID.String(),
		"preference_id": preferenceID,
	})
	if err != nil {
		t.Fatalf("verify reproject survivors: %v", err)
	}
	if len(survivors) != 1 ||
		survivors[0]["user_id"] == nil ||
		survivors[0]["created_at"] == nil ||
		survivors[0]["attributes_json"] != agentMemoryAttributes {
		t.Fatalf("reproject overwrote or lost shared/private state: %v", survivors)
	}

	if err := graph.PurgeOwner(ctx, owner); err != nil {
		t.Fatalf("PurgeOwner: %v", err)
	}
	remaining, err := client.Read(ctx,
		`MATCH (u:User {identifier:$owner_id})-[:HAS_PREFERENCE]->(p:Preference {id:$preference_id})
		 OPTIONAL MATCH (e:AdaptiveEpisode {owner_id:$owner_id})
		 RETURN u.identifier AS user, p.id AS preference, e.id AS adaptive`,
		map[string]any{
			"owner_id": owner.String(), "preference_id": preferenceID,
		})
	if err != nil {
		t.Fatalf("verify purge: %v", err)
	}
	if len(remaining) != 1 || remaining[0]["preference"] != preferenceID ||
		remaining[0]["adaptive"] != nil {
		t.Fatalf("purge must retain shared User memory only, rows=%v", remaining)
	}

	afterPurgeRecall, afterPurgeResults := callLiveLongTermRecall(
		t, ctx, memory, owner, memoryMarker,
	)
	if !containsLiveRecallResult(afterPurgeResults, preferenceID) {
		t.Fatalf("post-purge recall omits preference %s: %+v", preferenceID, afterPurgeResults)
	}
	requireLiveRecallUnchanged(
		t, "purge", afterRecall, afterResults, afterPurgeRecall, afterPurgeResults,
	)
}

func requireLiveRecallUnchanged(
	t *testing.T,
	operation string,
	before liveMemoryRecallEvidence,
	beforeResults []liveMemoryRecallResult,
	after liveMemoryRecallEvidence,
	afterResults []liveMemoryRecallResult,
) {
	t.Helper()
	if before.RecallMetadata.CorpusEpochAfter == nil ||
		after.RecallMetadata.CorpusEpochAfter == nil ||
		*before.RecallMetadata.CorpusEpochAfter !=
			*after.RecallMetadata.CorpusEpochAfter {
		t.Fatalf(
			"%s advanced corpus epoch: before=%v after=%v",
			operation,
			before.RecallMetadata.CorpusEpochAfter,
			after.RecallMetadata.CorpusEpochAfter,
		)
	}
	if !bytes.Equal(
		before.RecallMetadata.Results,
		after.RecallMetadata.Results,
	) || !reflect.DeepEqual(beforeResults, afterResults) {
		t.Fatalf(
			"%s changed recalled IDs/order: before=%s after=%s",
			operation,
			before.RecallMetadata.Results,
			after.RecallMetadata.Results,
		)
	}
}

func callLiveLongTermRecall(
	t *testing.T,
	ctx context.Context,
	memory mcp.Transport,
	owner uuid.UUID,
	query string,
) (liveMemoryRecallEvidence, []liveMemoryRecallResult) {
	t.Helper()
	text, err := memory.CallTool(ctx, "memory_get_context", map[string]any{
		"user_identifier":    owner.String(),
		"query":              query,
		"include_short_term": false,
		"include_long_term":  true,
		"include_reasoning":  false,
		"max_items":          4,
	})
	if err != nil {
		t.Fatalf("owner-scoped memory_get_context: %v", err)
	}
	var evidence liveMemoryRecallEvidence
	if err := json.Unmarshal([]byte(text), &evidence); err != nil {
		t.Fatalf("decode recall evidence: %v: %s", err, text)
	}
	metadata := evidence.RecallMetadata
	if evidence.Error != "" || !evidence.HasContext ||
		!strings.Contains(evidence.Context, query) ||
		!metadata.Coherent ||
		metadata.CorpusEpochBefore == nil ||
		metadata.CorpusEpochAfter == nil ||
		*metadata.CorpusEpochBefore == 0 ||
		*metadata.CorpusEpochBefore != *metadata.CorpusEpochAfter {
		t.Fatalf("ineligible recall evidence: %s", text)
	}
	var results []liveMemoryRecallResult
	if err := json.Unmarshal(metadata.Results, &results); err != nil {
		t.Fatalf("decode ordered recall results: %v: %s", err, metadata.Results)
	}
	for order, result := range results {
		if result.Order != order {
			t.Fatalf("recall result order[%d] = %d", order, result.Order)
		}
	}
	return evidence, results
}

func containsLiveRecallResult(results []liveMemoryRecallResult, id string) bool {
	for _, result := range results {
		if result.ID == id {
			return true
		}
	}
	return false
}

func envOrValue(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

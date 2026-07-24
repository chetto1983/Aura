//go:build adaptive_live && db_integration

package adaptive

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/google/uuid"
)

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
	marker := "ADAPTIVE-PRIVATE-" + owner.String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner, "adaptive-live-"+owner.String()); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	graph := NewGraphStore(client)
	t.Cleanup(func() {
		_ = graph.PurgeOwner(context.Background(), owner)
		_, _ = client.Write(context.Background(),
			`MATCH (u:User {identifier:$owner_id}) WHERE NOT (u)--() DETACH DELETE u`,
			map[string]any{"owner_id": owner.String()})
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})

	store := NewStore(pool, StoreConfig{})
	event, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: uuid.Must(uuid.NewV7()).String(),
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventDecision,
		Payload: []byte(fmt.Sprintf(
			`{"schema_version":"1.0","domain":"tool","private_marker":%q}`, marker,
		)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(ctx, event); err != nil {
		t.Fatalf("Record: %v", err)
	}
	projector := NewProjector(store, graph, ProjectorConfig{WorkerID: "adaptive-live"})
	didWork, err := projector.ProjectOne(ctx)
	if err != nil || !didWork {
		t.Fatalf("ProjectOne = (%t,%v)", didWork, err)
	}
	rows, err := client.Read(ctx, `
MATCH (u:User {identifier:$owner_id})-[:HAS_ADAPTIVE_EPISODE]->
      (:AdaptiveEpisode)-[:HAS_ADAPTIVE_EVENT]->(event:AdaptiveEvent {id:$event_id})
RETURN event.id AS id
`, map[string]any{"owner_id": owner.String(), "event_id": event.ID.String()})
	if err != nil {
		t.Fatalf("read projected graph: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("projected graph rows = %d, want 1", len(rows))
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
	contextText, err := memory.CallTool(ctx, "memory_get_context", map[string]any{
		"user_identifier": owner.String(), "include_reasoning": true,
	})
	if err != nil {
		t.Fatalf("memory_get_context: %v", err)
	}
	if strings.Contains(contextText, marker) || strings.Contains(contextText, event.ID.String()) {
		t.Fatalf("private adaptive event leaked through agent-memory context: %s", contextText)
	}

	if err := graph.PurgeOwner(ctx, owner); err != nil {
		t.Fatalf("PurgeOwner: %v", err)
	}
	remaining, err := client.Read(ctx,
		`MATCH (u:User {identifier:$owner_id}) OPTIONAL MATCH (e:AdaptiveEpisode {owner_id:$owner_id})
		 RETURN u.identifier AS user, e.id AS adaptive`,
		map[string]any{"owner_id": owner.String()})
	if err != nil {
		t.Fatalf("verify purge: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("purge should retain exactly the shared memory User, rows=%v", remaining)
	}
}

func envOrValue(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

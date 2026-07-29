//go:build neo4j_integration

package main

import (
	"context"
	"os"
	"testing"

	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/google/uuid"
)

func TestMemoryGraphPurgeLivePreservesSharedEntity(t *testing.T) {
	password := os.Getenv("NEO4J_PASSWORD")
	if password == "" {
		t.Fatal("NEO4J_PASSWORD is required for the live graph purge test")
	}
	cfg := &knowledge.Config{
		BoltURL:           envOrTest("AURA_NEO4J_BOLT_URL", "bolt://127.0.0.1:7687"),
		User:              envOrTest("NEO4J_USER", "neo4j"),
		Password:          password,
		Database:          envOrTest("AURA_NEO4J_DATABASE", "neo4j"),
		MCPBinary:         envOrTest("AURA_MCP_NEO4J_CYPHER_BIN", "mcp-neo4j-cypher"),
		ConnectTimeoutSec: 10,
	}
	ctx := context.Background()
	client, err := knowledge.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	owner := uuid.New().String()
	other := uuid.New().String()
	run := uuid.New().String()
	_, err = client.Write(ctx, `
CREATE (u1:User {identifier:$owner, id:$owner, purge_test:$run})
CREATE (u2:User {identifier:$other, id:$other, purge_test:$run})
CREATE (private:Entity {id:$run + '-private', name:'private', purge_test:$run})
CREATE (shared:Entity {id:$run + '-shared', name:'shared', purge_test:$run})
CREATE (p:Preference {id:$run + '-pref', preference:'private', purge_test:$run})
CREATE (f:Fact {id:$run + '-fact', subject:'private', purge_test:$run})
CREATE (c:Conversation {id:$run + '-conversation', session_id:$run, user_identifier:$owner, purge_test:$run})
CREATE (m:Message {id:$run + '-message', content:'private', purge_test:$run})
CREATE (rt:ReasoningTrace {id:$run + '-trace', user_identifier:$owner, purge_test:$run})
CREATE (rs:ReasoningStep {id:$run + '-step', purge_test:$run})
CREATE (a:MemoryReadAudit {id:$run + '-audit', user_identifier:$owner, purge_test:$run})
CREATE (d:Document {id:$run + '-document', purge_test:$run})
CREATE (ch:Chunk {id:$run + '-chunk', document_id:$run + '-document', purge_test:$run})
CREATE (u1)-[:HAS_ENTITY]->(private)
CREATE (u1)-[:HAS_ENTITY]->(shared)
CREATE (u2)-[:HAS_ENTITY]->(shared)
CREATE (u1)-[:HAS_PREFERENCE]->(p)
CREATE (u1)-[:HAS_FACT]->(f)
CREATE (u1)-[:HAS_CONVERSATION]->(c)
CREATE (c)-[:HAS_MESSAGE]->(m)
CREATE (u1)-[:HAS_TRACE]->(rt)
CREATE (rt)-[:HAS_STEP]->(rs)
CREATE (u1)-[:PERFORMED_READ]->(a)
CREATE (u1)-[:HAS_DOCUMENT]->(d)
CREATE (d)-[:HAS_CHUNK]->(ch)
`, map[string]any{"owner": owner, "other": other, "run": run})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = client.Write(context.Background(),
			"MATCH (n {purge_test:$run}) DETACH DELETE n",
			map[string]any{"run": run},
		)
	})

	if err := (memoryGraphPurgeAdapter{cfg: cfg}).PurgeGraph(ctx, owner); err != nil {
		t.Fatalf("PurgeGraph: %v", err)
	}
	rows, err := client.Read(ctx, `
MATCH (shared:Entity {id:$shared})
MATCH (:User {identifier:$other})-[:HAS_ENTITY]->(shared)
RETURN count(shared) AS shared_count
`, map[string]any{"shared": run + "-shared", "other": other})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || integerResult(rows[0]["shared_count"]) != 1 {
		t.Fatalf("shared entity or foreign ownership was removed: %#v", rows)
	}
	rows, err = client.Read(ctx,
		"MATCH (n {purge_test:$run}) WHERE NOT n:User AND NOT n:Entity RETURN count(n) AS private_count",
		map[string]any{"run": run},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || integerResult(rows[0]["private_count"]) != 0 {
		t.Fatalf("private owner graph survived purge: %#v", rows)
	}
}

func envOrTest(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

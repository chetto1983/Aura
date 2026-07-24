//go:build neo4j_integration && db_integration

package knowledge

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestLegacyLearningMigrationPreservesUnrelatedFixtures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mcp := openTestMCP(ctx, t)
	defer mcp.Close()

	runID := fmt.Sprintf("legacy-migration-%d", time.Now().UnixNano())
	params := map[string]any{
		"run":     runID,
		"user":    runID + "-user",
		"episode": runID + "-episode",
		"event":   runID + "-event",
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := mcp.Write(cleanupCtx, `MATCH (n {test_run_id:$run}) DETACH DELETE n`, map[string]any{"run": runID}); err != nil {
			t.Logf("cleanup legacy migration fixture %q: %v", runID, err)
		}
	}()

	const seed = `CREATE (u:User {identifier:$user, test_run_id:$run})
CREATE (episode:AdaptiveEpisode {id:$episode, owner_id:$user, test_run_id:$run})
CREATE (event:AdaptiveEvent {id:$event, owner_id:$user, aggregate_id:$episode, sequence:1, test_run_id:$run})
CREATE (u)-[:HAS_ADAPTIVE_EPISODE {test_run_id:$run}]->(episode)
CREATE (episode)-[:HAS_ADAPTIVE_EVENT {test_run_id:$run}]->(event)
CREATE (:ReasoningExample {test_run_id:$run})
CREATE (:ReasoningSeed {test_run_id:$run})
CREATE (:ToolSelectionExample {test_run_id:$run})
CREATE (:ToolSelectionSeed {test_run_id:$run})`
	if _, err := mcp.Write(ctx, seed, params); err != nil {
		t.Fatalf("seed migration preservation fixture: %v", err)
	}

	before, err := mcp.Read(ctx, `MATCH (n {test_run_id:$run}) RETURN count(n) AS total`, map[string]any{"run": runID})
	if err != nil {
		t.Fatalf("count fixture before migration: %v", err)
	}
	if len(before) != 1 || before[0]["total"] != float64(7) {
		t.Fatalf("fixture before migration = %#v, want seven nodes", before)
	}

	body, err := cypherFS.ReadFile("migrations/0004_decommission_legacy_learning.cypher")
	if err != nil {
		t.Fatalf("read embedded migration 0004: %v", err)
	}
	statements := splitCypherStatements(string(body))
	if len(statements) != 1 {
		t.Fatalf("migration 0004 has %d executable statements, want one", len(statements))
	}
	if _, err := mcp.Write(ctx, statements[0], nil); err != nil {
		t.Fatalf("apply embedded migration 0004: %v", err)
	}

	const preserved = `MATCH (n {test_run_id:$run})
RETURN count(n) AS total,
       count(CASE WHEN n:User THEN 1 END) AS users,
       count(CASE WHEN n:AdaptiveEpisode THEN 1 END) AS episodes,
       count(CASE WHEN n:AdaptiveEvent THEN 1 END) AS events,
       count(CASE WHEN n:ReasoningExample OR n:ReasoningSeed OR n:ToolSelectionExample OR n:ToolSelectionSeed THEN 1 END) AS legacy`
	after, err := mcp.Read(ctx, preserved, map[string]any{"run": runID})
	if err != nil {
		t.Fatalf("read fixture after migration: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("fixture after migration returned %d rows, want one", len(after))
	}
	row := after[0]
	for key, want := range map[string]float64{
		"total": 3, "users": 1, "episodes": 1, "events": 1, "legacy": 0,
	} {
		if got := row[key]; got != want {
			t.Errorf("fixture after migration %s = %#v, want %v (row=%#v)", key, got, want, row)
		}
	}
	t.Logf("migration 0004 fixture proof: before_total=7 after=%v", row)
}

//go:build arcadedb_integration

package arcadedb

import (
	"net/http"
	"strings"
	"testing"
)

func TestMemoryTemporalRepeatableReadProtocol(t *testing.T) {
	client := disposableMemoryClient(t)
	for _, sql := range []string{"CREATE VERTEX Entity SET name='Before'", "CREATE VERTEX Entity SET name='After'", "CREATE EDGE FACT FROM (SELECT FROM Entity WHERE name='Before') TO (SELECT FROM Entity WHERE name='After') SET fact_key='rr',statement='supported',valid_from='2026-01-01 00:00:00'"} {
		if _, err := client.Command(t.Context(), sql, nil); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, client.beginURL, strings.NewReader(`{"isolationLevel":"REPEATABLE_READ"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", client.authHeader)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("begin status=%d", resp.StatusCode)
	}
	session := resp.Header.Get(sessionHeader)
	defer client.rollbackTx(t.Context(), session)
	if _, err := client.queryInTx(t.Context(), session, "SELECT FROM FACT", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Command(t.Context(), "UPDATE FACT SET valid_to='2026-06-01 00:00:00' WHERE fact_key='rr'", nil); err != nil {
		t.Fatal(err)
	}
	query := "MATCH (a:Entity {name:'Before'}), (b:Entity {name:'After'}), p=shortestPath((a)-[r:FACT*..2 WHERE r.valid_from <= localdatetime($at) AND (r.valid_to IS NULL OR r.valid_to > localdatetime($at))]->(b)) RETURN relationships(p) AS edges"
	params := map[string]any{"at": "2026-07-01T00:00:00"}
	inside, err := client.executeSession(t.Context(), client.queryURL, session, "cypher", query, params)
	if err != nil {
		t.Fatal(err)
	}
	outside, err := client.Read(t.Context(), query, params)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("same record after concurrent close: repeatable path rows=%d fresh path rows=%d", len(inside), len(outside))
	if len(inside) != 1 || len(outside) != 0 {
		t.Fatal("read isolation does not provide the required record consistency")
	}
	edge := inside[0]["edges"].([]any)[0].(map[string]any)
	if edge["valid_to"] != nil {
		t.Fatalf("returned evidence disagrees with predicate: %v", edge["valid_to"])
	}
}

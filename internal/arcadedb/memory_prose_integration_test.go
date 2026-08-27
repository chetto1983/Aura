//go:build arcadedb_integration

// Live-graph proof for MEM-05's prose guard (D-18): a rejected write reaches
// no statement at all, so it cannot mint a junk Entity vertex under the
// UNIQUE index on Entity.name -- not even the subject's, since validation
// runs before the entity-upsert loop.
//
// Run: go test -tags arcadedb_integration ./internal/arcadedb/
package arcadedb

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestUpsertFactRejectsProseObjectAndCreatesNoEntity(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)

	before := countEntitiesLike(t, client, subject)
	if before != 0 {
		t.Fatalf("isolate() left %d stale entities behind: not a clean baseline", before)
	}

	_, err := client.UpsertFact(context.Background(), Fact{
		Subject: subject, Predicate: "learned_lesson",
		Object:    "This reads like a full sentence and must be rejected outright.",
		Statement: subject + " learned a lesson.",
		Source:    FactSource{RunID: runID, WriterRole: WriterParent},
	}, time.Now())
	if err == nil {
		t.Fatal("a prose object was accepted")
	}
	if !strings.Contains(err.Error(), "object") {
		t.Fatalf("error must name the object field: %v", err)
	}

	after := countEntitiesLike(t, client, subject)
	if after != before {
		t.Fatalf("entity count changed on a rejected write: before=%d after=%d", before, after)
	}
}

func countEntitiesLike(t *testing.T, client *Client, subjectPrefix string) int {
	t.Helper()
	rows, err := client.Query(context.Background(),
		"SELECT count(*) as c FROM Entity WHERE name LIKE :p",
		map[string]any{"p": subjectPrefix + "%"})
	if err != nil {
		t.Fatalf("count entities: %v", err)
	}
	if len(rows) == 0 {
		return 0
	}
	return int(rowInt(rows[0], "c"))
}

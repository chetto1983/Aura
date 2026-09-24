//go:build arcadedb_integration

// The stamp, against a live ArcadeDB. The unit tests prove what is written; this proves the
// index does not hide the rows the gate and the pass must find. With ArcadeDB's default null
// strategy an unstamped row answers no query that uses the index (arcadedb-docs
// reference/sql/sql-indexes.adoc), and the gate would open over it.
//
// Run: arcade-it.sh MemorySpace
package arcadedb

import (
	"context"
	"testing"
	"time"
)

func TestMemorySpaceStampsFactsAndFindsTheUnstampedThroughTheIndex(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	write := func(embedder DenseEmbedder, subject string) {
		t.Helper()
		fact := mergeFact(subject, "knows", "SpaceObject", subject+" knows the space object.")
		if _, err := client.WithEmbedder(embedder).UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("UpsertFact(%s): %v", subject, err)
		}
	}
	write(constantEmbedder{value: 1, space: "es1-route-a"}, "SpaceA")
	write(constantEmbedder{value: 2, space: "es1-route-b"}, "SpaceB")
	write(nil, "SpaceNone")

	count := func(where string, params map[string]any) int {
		t.Helper()
		rows, err := client.Query(ctx, "SELECT count(*) AS n FROM "+factEdgeType+" WHERE "+where, params)
		if err != nil {
			t.Fatalf("count %q: %v", where, err)
		}
		return int(rowInt(rows[0], "n"))
	}
	if n := count("embed_space IS NULL", nil); n != 1 {
		t.Fatalf("unstamped facts = %d, want 1: the stamp index hides NULLs", n)
	}
	if n := count("embed_space = :s", map[string]any{"s": "es1-route-a"}); n != 1 {
		t.Fatalf("facts in es1-route-a = %d, want 1", n)
	}
	if n := count("embedding IS NOT NULL AND (embed_space IS NULL OR embed_space <> :space)",
		map[string]any{"space": "es1-route-a"}); n != 1 {
		t.Fatalf("vectors outside es1-route-a = %d, want 1 (the es1-route-b fact)", n)
	}
}

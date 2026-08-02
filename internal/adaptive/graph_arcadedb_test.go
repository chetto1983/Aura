//go:build arcadedb_integration

// The adaptive projection, run against a live ArcadeDB.
//
// GraphStore leans on the wide end of Cypher — MERGE with ON CREATE SET on both
// nodes and relationships, datetime(), coalesce(), WITH…WHERE chains. How much of
// that ArcadeDB accepts is not a thing to assume: this runs the real projection,
// the real idempotency path, and the real purge against a live database.
//
//	go test -tags arcadedb_integration -run AdaptiveGraph -v ./internal/adaptive/
package adaptive

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/google/uuid"
)

func arcadeGraphStore(t *testing.T) (*GraphStore, *arcadedb.Client) {
	t.Helper()
	password := strings.TrimSpace(os.Getenv("ARCADEDB_PASSWORD"))
	if password == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_PASSWORD must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_PASSWORD not set")
	}
	base := strings.TrimSpace(os.Getenv("ARCADEDB_URL"))
	if base == "" {
		base = "http://127.0.0.1:2480"
	}
	database := strings.TrimSpace(os.Getenv("ARCADEDB_DATABASE"))
	if database == "" {
		database = "aura_memory"
	}
	client, err := arcadedb.New(arcadedb.Config{
		BaseURL: base, Database: database, User: "root", Password: password,
	})
	if err != nil {
		t.Skipf("arcadedb client: %v", err)
	}
	// *arcadedb.Client satisfies GraphWriter with no adapter: Write takes Cypher
	// and returns rows, which is exactly the interface GraphStore declares.
	return NewGraphStore(client), client
}

func adaptiveRecord(owner uuid.UUID, aggregate string, sequence int64) OutboxRecord {
	payload, _ := json.Marshal(map[string]any{"seq": sequence})
	sum := sha256.Sum256(payload)
	return OutboxRecord{
		ID:          uuid.New(),
		OwnerID:     owner,
		AggregateID: aggregate,
		Sequence:    sequence,
		DecisionID:  uuid.New(),
		Kind:        EventKind("assignment"),
		Payload:     payload,
		PayloadHash: sum[:],
		CreatedAt:   time.Now().UTC(),
	}
}

func TestAdaptiveGraphProjectsAndPurgesOnArcadeDB(t *testing.T) {
	store, client := arcadeGraphStore(t)
	ctx := context.Background()
	owner := uuid.New()
	aggregate := "episode-" + owner.String()[:8]
	t.Cleanup(func() { _ = store.PurgeOwner(context.Background(), owner) })

	first := adaptiveRecord(owner, aggregate, 1)
	if err := store.Project(ctx, first); err != nil {
		t.Fatalf("project seq 1: %v", err)
	}
	if err := store.Project(ctx, adaptiveRecord(owner, aggregate, 2)); err != nil {
		t.Fatalf("project seq 2: %v", err)
	}

	count := func(what string) int {
		t.Helper()
		rows, err := client.Query(ctx,
			"SELECT count(*) AS n FROM "+what+" WHERE owner_id = :owner",
			map[string]any{"owner": owner.String()})
		if err != nil {
			t.Fatalf("count %s: %v", what, err)
		}
		if len(rows) == 0 {
			return 0
		}
		switch n := rows[0]["n"].(type) {
		case float64:
			return int(n)
		case int:
			return n
		default:
			return 0
		}
	}
	if got := count("AdaptiveEvent"); got != 2 {
		t.Fatalf("AdaptiveEvent = %d, want 2", got)
	}
	if got := count("AdaptiveEpisode"); got != 1 {
		t.Fatalf("AdaptiveEpisode = %d, want 1 — the episode must MERGE, not duplicate", got)
	}

	// Idempotency is the property the projector depends on: the outbox retries,
	// and a retry must not double-write. Same record, projected twice.
	if err := store.Project(ctx, first); err != nil {
		t.Fatalf("re-project seq 1: %v", err)
	}
	if got := count("AdaptiveEvent"); got != 2 {
		t.Fatalf("AdaptiveEvent = %d after a retry, want 2 — the projection is not idempotent", got)
	}

	// A DIFFERENT payload under an id already written must be refused: the
	// projection's WHERE clause requires the stored payload_hash to match, so a
	// rewrite returns no rows and surfaces as an immutability conflict. The
	// payload has to actually differ — adaptiveRecord(…, 1) twice produces the
	// same bytes and therefore the same hash, which is the idempotent case above,
	// not this one.
	conflict := first
	conflict.Payload = []byte(`{"seq":1,"rewritten":true}`)
	rewritten := sha256.Sum256(conflict.Payload)
	conflict.PayloadHash = rewritten[:]
	if err := store.Project(ctx, conflict); err == nil {
		t.Error("a rewritten payload under an existing event id was accepted")
	}

	if err := store.PurgeOwner(ctx, owner); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if got := count("AdaptiveEvent"); got != 0 {
		t.Fatalf("AdaptiveEvent = %d after purge, want 0", got)
	}
	if got := count("AdaptiveEpisode"); got != 0 {
		t.Fatalf("AdaptiveEpisode = %d after purge, want 0", got)
	}
}

// One owner's purge must not touch another's — deprovisioning is per identity.
func TestAdaptiveGraphPurgeIsScopedToOneOwner(t *testing.T) {
	store, client := arcadeGraphStore(t)
	ctx := context.Background()
	keep, drop := uuid.New(), uuid.New()
	t.Cleanup(func() {
		_ = store.PurgeOwner(context.Background(), keep)
		_ = store.PurgeOwner(context.Background(), drop)
	})

	if err := store.Project(ctx, adaptiveRecord(keep, "keep-"+keep.String()[:8], 1)); err != nil {
		t.Fatalf("project keep: %v", err)
	}
	if err := store.Project(ctx, adaptiveRecord(drop, "drop-"+drop.String()[:8], 1)); err != nil {
		t.Fatalf("project drop: %v", err)
	}
	if err := store.PurgeAdaptiveGraph(ctx, drop.String()); err != nil {
		t.Fatalf("purge: %v", err)
	}

	rows, err := client.Query(ctx,
		"SELECT count(*) AS n FROM AdaptiveEvent WHERE owner_id = :owner",
		map[string]any{"owner": keep.String()})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if len(rows) == 0 || rows[0]["n"] == float64(0) {
		t.Fatal("purging one owner removed another owner's events")
	}
}

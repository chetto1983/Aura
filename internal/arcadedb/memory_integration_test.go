//go:build arcadedb_integration

// These run against a live ArcadeDB. They exist because the unit tests in this
// package assert on the SQL this package emits -- which is a restatement of the
// author's assumptions, not evidence. Every defect below was green under those
// tests and only appeared when a real database answered:
//
//   - `out.name` on an edge returns NULL instead of failing, so subject and
//     object came back blank with no diagnostic
//   - vector.neighbors() on an EDGE index yields records whose endpoints no
//     longer resolve: outV() over that subquery dies with "iRecord is null"
//   - with no temporal filter a superseded fact won at rank 1
//   - supersession filtered on the object, which is the one thing that changes,
//     so it could never fire
//
// Run: go test -tags arcadedb_integration ./internal/arcadedb/
package arcadedb

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func integrationClient(t *testing.T) *Client {
	t.Helper()
	base := os.Getenv("ARCADEDB_URL")
	if base == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_URL must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_URL not set")
	}
	client, err := New(Config{
		BaseURL:  base,
		Database: envOr("ARCADEDB_DATABASE", "aura_memory_it"),
		User:     envOr("ARCADEDB_USER", "root"),
		Password: os.Getenv("ARCADEDB_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := client.EnsureMemorySchema(context.Background()); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}
	return client
}

func disposableMemoryClient(t *testing.T) *Client {
	client := disposableArcadeClient(t)
	if err := client.EnsureMemorySchema(context.Background()); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}
	return client
}

func disposableArcadeClient(t *testing.T) *Client {
	t.Helper()
	base := os.Getenv("ARCADEDB_URL")
	if base == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_URL must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_URL not set")
	}
	admin, err := New(Config{
		BaseURL: base, Database: "unused", User: envOr("ARCADEDB_USER", "root"),
		Password: os.Getenv("ARCADEDB_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	database := fmt.Sprintf("aura_memory_provenance_%d", time.Now().UnixNano())
	if _, err := admin.CreateDatabase(context.Background(), database); err != nil {
		t.Fatalf("create disposable database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.DropDatabase(context.Background(), database); err != nil {
			t.Errorf("drop disposable database %s: %v", database, err)
		}
	})
	client, err := New(Config{
		BaseURL: base, Database: database, User: envOr("ARCADEDB_USER", "root"),
		Password: os.Getenv("ARCADEDB_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("disposable client: %v", err)
	}
	return client
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// isolate scopes a test to its own subject and run id, and removes what it wrote.
func isolate(t *testing.T, client *Client) (subject, runID string) {
	t.Helper()
	subject = fmt.Sprintf("Subject_%s", strings.ReplaceAll(t.Name(), "/", "_"))
	runID = "it-" + subject
	cleanup := func() {
		ctx := context.Background()
		_, _ = client.Command(ctx,
			"DELETE FROM FACT WHERE outV().name LIKE :p OR inV().name LIKE :p",
			map[string]any{"p": subject + "%"})
		_, _ = client.Command(ctx, "DELETE FROM Entity WHERE name LIKE :p",
			map[string]any{"p": subject + "%"})
	}
	cleanup()
	t.Cleanup(cleanup)
	return subject, runID
}

func TestMemoryProvenanceReplayAttachDetachLifecycle(t *testing.T) {
	client := disposableMemoryClient(t)
	subject, runID := isolate(t, client)
	object := subject + "_Caraglio"
	fact := Fact{
		Subject: subject, SubjectKind: "Person", Predicate: "lives_in",
		Object: object, ObjectKind: "City", Statement: subject + " lives in Caraglio.",
		Source: FactSource{RunID: runID, MemoryIDs: []string{"msg-1"}, WriterRole: WriterParent},
	}
	write(t, client, fact, time.Now().UTC())
	write(t, client, fact, time.Now().UTC().Add(time.Second))

	hits, err := client.FactsAbout(context.Background(), subject, "lives_in", 10, time.Time{}, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout after replay: %v", err)
	}
	if len(hits) != 1 || len(hits[0].Sources) != 1 {
		t.Fatalf("exact replay duplicated edge or source: %+v", hits)
	}
	if hits[0].SubjectKind != "Person" || hits[0].ObjectKind != "City" {
		t.Fatalf("entity kinds were not persisted: %+v", hits[0])
	}

	secondRun := runID + "-second"
	fact.Source = FactSource{RunID: secondRun, MemoryIDs: []string{"msg-2"}, WriterRole: WriterParent}
	write(t, client, fact, time.Now().UTC().Add(2*time.Second))
	hits, err = client.FactsAbout(context.Background(), subject, "lives_in", 10, time.Time{}, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout after attach: %v", err)
	}
	if len(hits) != 1 || len(hits[0].Sources) != 2 {
		t.Fatalf("second source did not attach to the same edge: %+v", hits)
	}

	forgot, err := client.Forget(context.Background(), ForgetFilter{SourceRunID: runID, KeepOrphans: true})
	if err != nil {
		t.Fatalf("detach first source: %v", err)
	}
	if forgot.Facts != 1 {
		t.Fatalf("first detach = %+v", forgot)
	}
	hits, err = client.FactsAbout(context.Background(), subject, "lives_in", 10, time.Time{}, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout after first detach: %v", err)
	}
	if len(hits) != 1 || len(hits[0].Sources) != 1 || hits[0].Sources[0].RunID != secondRun {
		t.Fatalf("detaching one source destroyed other support: %+v", hits)
	}

	forgot, err = client.Forget(context.Background(), ForgetFilter{SourceRunID: secondRun, KeepOrphans: true})
	if err != nil {
		t.Fatalf("detach final source: %v", err)
	}
	if forgot.Facts != 1 {
		t.Fatalf("final detach = %+v", forgot)
	}
	hits, err = client.FactsAbout(context.Background(), subject, "lives_in", 10, time.Time{}, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout after final detach: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("edge survived without sources: %+v", hits)
	}
}

func TestMemoryProvenanceMigratesLegacyStorageAndDropsRetiredProperties(t *testing.T) {
	client := disposableArcadeClient(t)
	ctx := context.Background()
	legacySchema := []string{
		"CREATE VERTEX TYPE Entity IF NOT EXISTS",
		"CREATE PROPERTY Entity.name IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON Entity (name) UNIQUE",
		"CREATE EDGE TYPE FACT IF NOT EXISTS",
		"CREATE PROPERTY FACT.statement IF NOT EXISTS STRING",
		"CREATE PROPERTY FACT.predicate IF NOT EXISTS STRING",
		"CREATE PROPERTY FACT.valid_from IF NOT EXISTS DATETIME",
		"CREATE PROPERTY FACT.source_run_id IF NOT EXISTS STRING",
		"CREATE PROPERTY FACT.source_memory_ids IF NOT EXISTS LIST",
	}
	for _, statement := range legacySchema {
		if _, err := client.Command(ctx, statement, nil); err != nil {
			t.Fatalf("legacy schema %q: %v", statement, err)
		}
	}
	subject := "LegacyPerson"
	object := "LegacyCity"
	for _, name := range []string{subject, object} {
		if _, err := client.Command(ctx, upsertEntityStatement, map[string]any{"name": name}); err != nil {
			t.Fatalf("legacy entity %q: %v", name, err)
		}
	}
	if _, err := client.Command(ctx,
		"CREATE EDGE FACT FROM (SELECT FROM Entity WHERE name = :subject) "+
			"TO (SELECT FROM Entity WHERE name = :object) SET statement = :statement, "+
			"predicate = :predicate, valid_from = :valid_from, source_run_id = :run, source_memory_ids = :ids",
		map[string]any{
			"subject": subject, "object": object, "statement": "LegacyPerson lives in LegacyCity.",
			"predicate": "lives_in", "valid_from": now.Format(time.RFC3339),
			"run": "legacy-run", "ids": []string{"legacy-message"},
		}); err != nil {
		t.Fatalf("legacy fact: %v", err)
	}
	for range 2 {
		if err := client.EnsureMemorySchema(ctx); err != nil {
			t.Fatalf("EnsureMemorySchema migration: %v", err)
		}
	}
	state, err := client.memorySchemaState(ctx)
	if err != nil {
		t.Fatalf("memorySchemaState: %v", err)
	}
	if !state.hasIndex(factKeyIndexName) {
		t.Fatalf("fact identity index not recognized after replay: %+v", state.indexes)
	}
	hits, err := client.FactsAbout(ctx, subject, "lives_in", 10, time.Time{}, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout migrated fact: %v", err)
	}
	if len(hits) != 1 || len(hits[0].Sources) != 1 ||
		hits[0].Sources[0].RunID != "legacy-run" ||
		!slices.Equal(hits[0].Sources[0].MemoryIDs, []string{"legacy-message"}) {
		t.Fatalf("migrated provenance = %+v", hits)
	}
	schema, err := client.Schema(ctx)
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	for _, edge := range schema.Edges {
		if edge.Name != factEdgeType {
			continue
		}
		if slices.Contains(edge.Properties, legacySourceRunID) || slices.Contains(edge.Properties, legacySourceMemoryIDs) {
			t.Fatalf("retired properties survived migration: %+v", edge.Properties)
		}
		if !slices.Contains(edge.Properties, "sources") || !slices.Contains(edge.Properties, "fact_key") {
			t.Fatalf("new provenance properties missing: %+v", edge.Properties)
		}
		return
	}
	t.Fatal("FACT edge type missing after migration")
}

func TestMemoryProvenanceRecurrencePreservesHistory(t *testing.T) {
	client := disposableMemoryClient(t)
	subject, runID := isolate(t, client)
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	first := Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Torino",
		Statement: subject + " lives in Torino.", Source: FactSource{RunID: runID + "-first", WriterRole: WriterParent},
		ValidFrom: base,
	}
	write(t, client, first, base)
	second := Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Caraglio",
		Statement: subject + " lives in Caraglio.", Source: FactSource{RunID: runID + "-second", WriterRole: WriterParent},
		ValidFrom: base.Add(time.Second), Supersedes: true,
	}
	write(t, client, second, base.Add(time.Second))
	recurrent := first
	recurrent.Source = FactSource{RunID: runID + "-recurrent", WriterRole: WriterParent}
	recurrent.ValidFrom = base.Add(2 * time.Second)
	recurrent.Supersedes = true
	write(t, client, recurrent, base.Add(2*time.Second))

	assertObjectAt := func(at time.Time, object string) {
		t.Helper()
		hits, err := client.FactsAbout(context.Background(), subject, "lives_in", 10, at, FactsAboutDirect)
		if err != nil {
			t.Fatalf("FactsAbout(%s): %v", at, err)
		}
		if len(hits) != 1 || hits[0].Object != object {
			t.Fatalf("FactsAbout(%s) = %+v, want %s", at, hits, object)
		}
	}
	assertObjectAt(base.Add(500*time.Millisecond), subject+"_Torino")
	assertObjectAt(base.Add(1500*time.Millisecond), subject+"_Caraglio")
	assertObjectAt(base.Add(3*time.Second), subject+"_Torino")

	rows, err := client.Query(context.Background(),
		"SELECT fact_key, valid_to FROM FACT WHERE outV().name = :subject ORDER BY valid_from",
		map[string]any{"subject": subject})
	if err != nil {
		t.Fatalf("read recurrence history: %v", err)
	}
	activeKeys := 0
	for _, row := range rows {
		if rowString(row, "fact_key") != "" {
			activeKeys++
		}
	}
	if len(rows) != 3 || activeKeys != 1 {
		t.Fatalf("history rows=%d active keys=%d rows=%+v", len(rows), activeKeys, rows)
	}
}

func write(t *testing.T, client *Client, fact Fact, at time.Time) FactWrite {
	t.Helper()
	written, err := client.UpsertFact(context.Background(), fact, at)
	if err != nil {
		t.Fatalf("UpsertFact(%q): %v", fact.Statement, err)
	}
	return written
}

func TestStudioGraphSerializerReturnsFactEndpoints(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	object := subject + "_ArcadeDB"
	write(t, client, Fact{
		Subject: subject, Predicate: "uses", Object: object,
		Statement: subject + " uses ArcadeDB.", Source: FactSource{RunID: runID, WriterRole: WriterParent},
	}, time.Now().UTC())

	graph, err := client.QueryStudioGraph(
		context.Background(),
		"SELECT FROM FACT WHERE sources CONTAINS (run_id = :run) LIMIT 10",
		map[string]any{"run": runID},
		10,
	)
	if err != nil {
		t.Fatalf("QueryStudioGraph: %v", err)
	}
	if len(graph.Vertices) != 2 || len(graph.Edges) != 1 {
		t.Fatalf("studio graph = %d vertices / %d edges: %+v", len(graph.Vertices), len(graph.Edges), graph)
	}
	edge := graph.Edges[0]
	if edge.RID == "" || edge.Out == "" || edge.In == "" || edge.Out == edge.In {
		t.Fatalf("edge endpoints were not decoded: %+v", edge)
	}
	if edge.Properties["predicate"] != "uses" {
		t.Fatalf("edge properties = %+v", edge.Properties)
	}
}

// The endpoints must survive retrieval. `out.name` returned NULL here and no
// test noticed, because the SQL "looked right".
func TestRetrievedFactCarriesItsEndpointsAndProvenance(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	object := subject + "_Caraglio"

	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: object,
		Statement: subject + " lives in Caraglio.",
		Source:    FactSource{RunID: runID, MemoryIDs: []string{"msg-7"}, WriterRole: WriterParent},
	}, time.Now())

	hits, err := client.SearchFacts(context.Background(), "Caraglio", 5, time.Time{})
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	hit, ok := findBySubject(hits, subject)
	if !ok {
		t.Fatalf("fact not retrieved; hits = %+v", hits)
	}
	if hit.Subject != subject {
		t.Fatalf("subject = %q, want %q", hit.Subject, subject)
	}
	if hit.Object != object {
		t.Fatalf("object = %q, want %q", hit.Object, object)
	}
	if len(hit.Sources) != 1 || hit.Sources[0].RunID != runID ||
		len(hit.Sources[0].MemoryIDs) != 1 || hit.Sources[0].MemoryIDs[0] != "msg-7" {
		t.Fatalf("sources = %+v", hit.Sources)
	}
}

// A search with no instant must answer "what is true", not "what was ever
// true". Without the default filter the expired fact ranked first.
func TestExpiredFactIsExcludedFromAPresentTenseSearch(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	now := time.Now()

	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Torino",
		Statement: subject + " lives in Torino.",
		Source:    FactSource{RunID: runID, WriterRole: WriterParent},
		ValidFrom: now.AddDate(-6, 0, 0), ValidTo: now.AddDate(-3, 0, 0),
	}, now)

	hits, err := client.SearchFacts(context.Background(), "Torino", 5, time.Time{})
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if _, found := findBySubject(hits, subject); found {
		t.Fatalf("an expired fact was returned by a present-tense search: %+v", hits)
	}
}

// The whole point of the bitemporal model: the past stays answerable.
func TestAsOfReturnsWhatWasTrueThen(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	now := time.Now()
	past := now.AddDate(-5, 0, 0)

	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Torino",
		Statement: subject + " lives in Torino.",
		Source:    FactSource{RunID: runID, WriterRole: WriterParent},
		ValidFrom: now.AddDate(-6, 0, 0), ValidTo: now.AddDate(-3, 0, 0),
	}, now)

	hits, err := client.SearchFacts(context.Background(), "Torino", 5, past)
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	hit, ok := findBySubject(hits, subject)
	if !ok {
		t.Fatalf("as_of inside the window returned nothing; hits = %+v", hits)
	}
	if !strings.Contains(hit.Statement, "Torino") {
		t.Fatalf("statement = %q", hit.Statement)
	}
}

// Supersession must close the previous window rather than delete it, and must
// match on subject+predicate: filtering on the object meant it never fired.
func TestSupersessionClosesTheWindowAndKeepsThePastQueryable(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	now := time.Now()
	moved := now.AddDate(-1, 0, 0)

	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Torino",
		Statement: subject + " lives in Torino.",
		Source:    FactSource{RunID: runID, WriterRole: WriterParent},
		ValidFrom: now.AddDate(-6, 0, 0),
	}, now)

	written := write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Caraglio",
		Statement: subject + " lives in Caraglio.",
		Source:    FactSource{RunID: runID, WriterRole: WriterParent},
		ValidFrom: moved, Supersedes: true,
	}, now)
	if written.Superseded != 1 {
		t.Fatalf("superseded = %d, want 1", written.Superseded)
	}

	present, err := client.SearchFacts(context.Background(), "Caraglio", 5, time.Time{})
	if err != nil {
		t.Fatalf("SearchFacts(now): %v", err)
	}
	if hit, ok := findBySubject(present, subject); !ok || !strings.Contains(hit.Statement, "Caraglio") {
		t.Fatalf("present-tense answer = %+v, want Caraglio", present)
	}

	before, err := client.SearchFacts(context.Background(), "Torino", 5, now.AddDate(-3, 0, 0))
	if err != nil {
		t.Fatalf("SearchFacts(past): %v", err)
	}
	hit, ok := findBySubject(before, subject)
	if !ok || !strings.Contains(hit.Statement, "Torino") {
		t.Fatalf("the superseded fact stopped being queryable: %+v", before)
	}
	// Closed, not deleted.
	if hit.ValidTo == "" {
		t.Fatal("superseded fact has no valid_to: the window was not closed")
	}
}

// The schema must be replayable: it runs on every boot.
func TestEnsureMemorySchemaIsIdempotentAgainstALiveDatabase(t *testing.T) {
	client := integrationClient(t)
	for range 2 {
		if err := client.EnsureMemorySchema(context.Background()); err != nil {
			t.Fatalf("EnsureMemorySchema: %v", err)
		}
	}
}

func findBySubject(hits []FactHit, subject string) (FactHit, bool) {
	for _, hit := range hits {
		if hit.Subject == subject {
			return hit, true
		}
	}
	return FactHit{}, false
}

// The traversal path is the one that must be exact: when the question names the
// entity there is nothing to rank, and the answer is not allowed to be a guess.
func TestFactsAboutAnswersExactlyAndRespectsTime(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	now := time.Now()

	write(t, client, Fact{
		Subject: subject, Predicate: "works_for", Object: subject + "_PmSync",
		Statement: subject + " works for PmSync.",
		Source:    FactSource{RunID: runID, WriterRole: WriterParent}, ValidFrom: now.AddDate(-4, 0, 0),
	}, now)
	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Torino",
		Statement: subject + " lives in Torino.",
		Source:    FactSource{RunID: runID, WriterRole: WriterParent},
		ValidFrom: now.AddDate(-6, 0, 0), ValidTo: now.AddDate(-3, 0, 0),
	}, now)

	all, err := client.FactsAbout(context.Background(), subject, "", 10, time.Time{}, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if len(all) != 1 || all[0].Predicate != "works_for" {
		t.Fatalf("present-tense traversal = %+v, want only the open fact", all)
	}

	narrowed, err := client.FactsAbout(context.Background(), subject, "works_for", 10, time.Time{}, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout(predicate): %v", err)
	}
	if len(narrowed) != 1 || narrowed[0].Object != subject+"_PmSync" {
		t.Fatalf("narrowed = %+v", narrowed)
	}

	past, err := client.FactsAbout(context.Background(), subject, "lives_in", 10, now.AddDate(-5, 0, 0), FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout(as_of): %v", err)
	}
	if len(past) != 1 || !strings.Contains(past[0].Statement, "Torino") {
		t.Fatalf("as_of traversal = %+v", past)
	}
}
